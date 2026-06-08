package object

import (
	"github.com/pion/transport/v4"

	"github.com/l7mp/stunner/internal/quota"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/telemetry"
	licensecfg "github.com/l7mp/stunner/pkg/config/license"
	"github.com/l7mp/stunner/pkg/logger"
)

// Type names for the various Object kinds. Used as keys in the Registry.
const (
	TypeStunner      = "stunner"
	TypeAdmin        = "admin"
	TypeAuth         = "auth"
	TypeHealth       = "health"
	TypeMetrics      = "metrics"
	TypeOffload      = "offload"
	TypeListenerList = "listeners"
	TypeListener     = "listener"
	TypeClusterList  = "clusters"
	TypeCluster      = "cluster"
)

// Singleton names. For singleton Object kinds, the ObjectName is fixed.
const (
	DefaultListenerListName = "default-listener-list"
	DefaultClusterListName  = "default-cluster-list"
	DefaultHealthName       = "default-health"
	DefaultMetricsName      = "default-metrics"
	DefaultOffloadName      = "default-offload"
)

// Registry stores every live Object in the dataplane, keyed by (ObjectType, ObjectName). The
// Registry replaces the old object.Router: it serves the same runtime cross-reference role (e.g.,
// the TURN auth handler looks up the Auth object at request time) but with a flat, generic shape.
type Registry interface {
	// Lookup returns the named object of the given type, if present.
	Lookup(objType, name string) (Object, bool)
	// LookupOne returns the single object of the given type. For singleton kinds (Admin, Auth,
	// Health, ...) this is the natural query; ok is false if there is no such object.
	LookupOne(objType string) (Object, bool)
	// LookupAll returns every object of the given type in stable (by name) order.
	LookupAll(objType string) []Object
	// Add registers an object. Returns an error if an object with the same (type, name) is
	// already registered.
	Add(o Object) error
	// Remove unregisters an object.
	Remove(o Object) error
}

// Router is an object that knows how to match clusters and listeners.
type Router interface {
	// GetAdmin returns the admin object. Panics if no admin object is available.
	GetAdmin() *Admin

	// GetAuth returns the authenitation object. Panics if no auth object is available.
	GetAuth() *Auth

	// GetListeners returns all STUNner listeners.
	GetListeners() []*Listener

	// GetListener returns a STUNner listener or nil of no listener with the given name was found.
	GetListener(name string) *Listener

	// GetClusters returns all STUNner clusters.
	GetClusters() []*Cluster

	// GetCluster returns a STUNner cluster or nil if no cluster with the given name was found.
	GetCluster(name string) *Cluster

	// GetLicenseConfigManager returns the license manager.
	GetLicenseConfigManager() licensecfg.ConfigManager
}

// GetClustersForListener returns the clusters for a listener.
func GetClustersForListener(l *Listener) []*Cluster {
	ret := []*Cluster{}
	for _, route := range l.Routes {
		for _, o := range l.reg.LookupAll(TypeCluster) {
			if o.ObjectName() == route {
				ret = append(ret, o.(*Cluster))
			}
		}
	}

	return ret
}

// Runtime carries per-STUNner process dependencies used by object constructors.
type Runtime struct {
	Logger           logger.LoggerFactory
	DryRun           bool
	Resolver         resolver.DnsResolver
	Telemetry        *telemetry.Telemetry
	QuotaStore       quota.Store
	UdpThreadNum     int
	Net              transport.Net
	ReadinessHandler ReadinessHandler
	StatusHandler    StatusHandler
	OffloadHandler   OffloadHandlerCtor
}
