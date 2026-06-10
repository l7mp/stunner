// Package runtime is the kernel of the STUNner object system: it defines the node contracts
// (Runnable, Reconcilable, Object), the Registry that stores every live node keyed by (type,
// name) with parent edges, and the Runtime, the single cross-object access point carrying
// process-wide dependencies, registry-backed config/status lookups, relay routing, and the
// readiness/shutdown flags.
package runtime

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/pion/transport/v4"
	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/internal/quota"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

// ObjectType identifies a runtime object kind.
type ObjectType string

// Type names for the various object kinds.
const (
	TypeStunner        ObjectType = "stunner"
	TypeAdmin          ObjectType = "admin"
	TypeAuth           ObjectType = "auth"
	TypeHealth         ObjectType = "health"
	TypeMetrics        ObjectType = "metrics"
	TypeOffload        ObjectType = "offload"
	TypeListener       ObjectType = "listener"
	TypeListenerServer ObjectType = "listener-server"
	TypeCluster        ObjectType = "cluster"
	TypeRelay          ObjectType = "relay"
)

// Action is the reconciliation action an Object reports from Inspect.
type Action int

const (
	// ActionNone means no object-local change is needed.
	ActionNone Action = iota
	// ActionReconcile means object-local state changes but no restart is required.
	ActionReconcile
	// ActionRestart means object-local state changes and the object (plus its subtree) must
	// be closed, reconciled and started again.
	ActionRestart
)

// Runnable is the minimal node contract in the object tree: identity plus lifecycle.
// Lifecycle-only nodes (listener servers, cluster relays) implement just this.
type Runnable interface {
	Name() string
	Type() ObjectType
	// Start brings the node online.
	Start() error
	// Close shuts the node down. The shutdown flag distinguishes terminate (true) from
	// reconcile-driven restart (false).
	Close(shutdown bool) error
}

// Reconcilable is the desired-state convergence contract for config-driven objects.
type Reconcilable interface {
	// GetConfig returns the live running config. Implementations MUST be safe for
	// concurrent use: the dataplane reads configs from request handlers while the
	// reconciler may be writing (use an atomic snapshot, see the concrete objects).
	GetConfig() stnrv1.Config
	// Inspect compares the live state (old), desired object config (new), and the full
	// desired StunnerConfig (full) and reports the object-local decision. Recursion into
	// children is handled by the reconciler walk, not Inspect.
	Inspect(old, new stnrv1.Config, full *stnrv1.StunnerConfig) (Action, error)
	// Reconcile applies a new config to the Object. The Object must already be Closed if
	// the previous Inspect call returned ActionRestart.
	Reconcile(conf stnrv1.Config) error
	// Status returns the live status of the Object.
	Status() stnrv1.Status
}

// Object is the full contract for config-driven STUNner objects.
type Object interface {
	Runnable
	Reconcilable
}

// NodeConfig is a name-only config used by lifecycle-only (Runnable) kinds: the reconciler
// needs configs only for set-membership diffing, and lifecycle-only nodes derive all real
// state from their parent.
type NodeConfig struct {
	Name string `json:"name"`
}

func (c *NodeConfig) Validate() error    { return nil }
func (c *NodeConfig) ConfigName() string { return c.Name }
func (c *NodeConfig) DeepEqual(other stnrv1.Config) bool {
	o, ok := other.(*NodeConfig)
	return ok && c.Name == o.Name
}
func (c *NodeConfig) DeepCopyInto(dst stnrv1.Config) {
	if d, ok := dst.(*NodeConfig); ok {
		*d = *c
	}
}
func (c *NodeConfig) String() string { return fmt.Sprintf("NodeConfig{name=%q}", c.Name) }

// Relay is a protocol-specific upstream relay implementation.
type Relay interface {
	Validate() error
	AllocatePacketConn(turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error)
	AllocateListener(turn.AllocateListenerConfig) (net.Listener, net.Addr, error)
	AllocateConn(turn.AllocateConnConfig) (net.Conn, error)
	ClusterName() string
	Protocol() stnrv1.ClusterProtocol
	Match(peer net.IP, port int) bool
}

// ManagedRelay is a relay that has an explicit lifecycle.
type ManagedRelay interface {
	Relay
	Start() error
	Close() error
}

// Router resolves relays for packet-level workflows.
type Router interface {
	Route(listener string, routes []string, peer net.IP, port int) (Relay, bool)
	InvalidateCache()
}

// NewUnsupportedNetOpError returns a net.OpError wrapping errors.ErrUnsupported.
func NewUnsupportedNetOpError(op, network string) error {
	return &net.OpError{Op: op, Net: network, Err: errors.ErrUnsupported}
}

// Deps carries the process-wide dependencies shared by all objects. Set once at startup.
type Deps struct {
	Logger       logger.LoggerFactory
	DryRun       bool
	Resolver     resolver.DnsResolver
	Telemetry    *telemetry.Telemetry
	QuotaStore   quota.Store
	UdpThreadNum int
	Net          transport.Net
}

// Runtime is the single cross-object access point: process-wide dependencies, the object
// Registry, relay routing, and the process readiness/shutdown flags.
type Runtime struct {
	Deps

	// Registry stores every live node in the dataplane.
	Registry *Registry
	// Router resolves cluster relays for the TURN packet path with LRU caching.
	Router Router

	ready      atomic.Bool
	shutdown   atomic.Bool
	forceReady atomic.Bool
}

// New creates a Runtime with an empty Registry and a default Router.
func New(deps Deps) *Runtime {
	rt := &Runtime{Deps: deps, Registry: NewRegistry()}
	rt.Router = NewRouter(rt)
	return rt
}

// IsReady returns true if STUNner is ready to serve requests.
func (rt *Runtime) IsReady() bool {
	return rt.ready.Load()
}

// ReadyForProbes returns true if readiness probes should report ready.
func (rt *Runtime) ReadyForProbes() bool {
	return rt.forceReady.Load() || rt.ready.Load()
}

// IsShutdown returns true if STUNner is in shutdown mode.
func (rt *Runtime) IsShutdown() bool {
	return rt.shutdown.Load()
}

// SetReady updates the runtime readiness flag.
func (rt *Runtime) SetReady(v bool) {
	rt.ready.Store(v)
}

// SetShutdown updates shutdown mode and clears readiness on shutdown.
func (rt *Runtime) SetShutdown(v bool) {
	rt.shutdown.Store(v)
	if v {
		rt.ready.Store(false)
	}
}

// SetForceReady configures force-ready readiness probe behavior.
func (rt *Runtime) SetForceReady(v bool) {
	rt.forceReady.Store(v)
}
