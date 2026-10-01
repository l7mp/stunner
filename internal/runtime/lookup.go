package runtime

import (
	"github.com/l7mp/stunner/v2/internal/api"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	licensecfg "github.com/l7mp/stunner/v2/pkg/config/license"
)

var defaultSingletonNames = map[ObjectType]string{
	TypeStunner: stnrv2.DefaultStunnerName,
	TypeAdmin:   stnrv2.DefaultAdminName,
	TypeAuth:    stnrv2.DefaultAuthName,
	TypeHealth:  stnrv2.DefaultHealthName,
	TypeMetrics: stnrv2.DefaultMetricsName,
	TypeOffload: stnrv2.DefaultOffloadName,
}

// defaultSingletonName returns the canonical singleton object name for an object type.
func defaultSingletonName(objType ObjectType) (string, bool) {
	name, ok := defaultSingletonNames[objType]
	return name, ok
}

// mustDefaultSingletonName returns the canonical singleton object name for an object type and
// panics if the type has none.
func mustDefaultSingletonName(objType ObjectType) string {
	name, ok := defaultSingletonName(objType)
	if !ok {
		panic("no default singleton name for type: " + string(objType))
	}
	return name
}

// Server returns the server of a name, false if there is none.
func (rt *Runtime) Server(name string) (api.Server, bool) {
	o, ok := rt.Registry.Get(TypeServer, name)
	if !ok {
		return nil, false
	}
	s, ok := o.(api.Server)
	return s, ok
}

// Dialer returns the dialer of the cluster of a name, false if there is none.
func (rt *Runtime) Dialer(name string) (api.Dialer, bool) {
	o, ok := rt.Registry.Get(TypeCluster, name)
	if !ok {
		return nil, false
	}
	return o.(api.Dialer), true
}

// Router returns the router of the cluster of a name, false if there is none.
func (rt *Runtime) Router(name string) (api.Router, bool) {
	o, ok := rt.Registry.Get(TypeCluster, name)
	if !ok {
		return nil, false
	}
	return o.(api.Router), true
}

// ClusterConfig returns the config of the cluster of a name, nil if there is none.
func (rt *Runtime) ClusterConfig(name string) *stnrv2.ClusterConfig {
	if c := rt.GetConfig(TypeCluster, name); c != nil {
		return c.(*stnrv2.ClusterConfig)
	}
	return nil
}

// ServerConfig returns the config of the server of a name, nil if there is none.
func (rt *Runtime) ServerConfig(name string) *stnrv2.ServerConfig {
	if c := rt.GetConfig(TypeServer, name); c != nil {
		return c.(*stnrv2.ServerConfig)
	}
	return nil
}

// ListenerConfig returns the config of the listener of a name, nil if there is none.
func (rt *Runtime) ListenerConfig(name string) *stnrv2.ListenerConfig {
	if c := rt.GetConfig(TypeListener, name); c != nil {
		return c.(*stnrv2.ListenerConfig)
	}
	return nil
}

// LicenseManager returns the process license config manager.
func (rt *Runtime) LicenseManager() licensecfg.ConfigManager { return rt.License }

// GetConfig returns the live config of a node. An empty name resolves to the canonical
// singleton name of the type. Returns nil if the node is missing or not Reconcilable.
func (rt *Runtime) GetConfig(objType ObjectType, name string) stnrv2.Config {
	r := rt.reconcilable(objType, name)
	if r == nil {
		return nil
	}
	return r.GetConfig()
}

// GetConfigs returns the live configs of every Reconcilable node of a type, in stable order.
// Lifecycle-only nodes are skipped.
func (rt *Runtime) GetConfigs(objType ObjectType) []stnrv2.Config {
	objects := rt.Registry.List(objType)
	configs := make([]stnrv2.Config, 0, len(objects))
	for _, o := range objects {
		r, ok := o.(Reconcilable)
		if !ok {
			continue
		}
		configs = append(configs, r.GetConfig())
	}
	return configs
}

// GetStatus returns the live status of a node. An empty name resolves to the canonical
// singleton name of the type. Returns nil if the node is missing or not Reconcilable.
func (rt *Runtime) GetStatus(objType ObjectType, name string) stnrv2.Status {
	r := rt.reconcilable(objType, name)
	if r == nil {
		return nil
	}
	return r.Status()
}

// GetStatuses returns the live statuses of every Reconcilable node of a type, in stable
// order. Lifecycle-only nodes are skipped.
func (rt *Runtime) GetStatuses(objType ObjectType) []stnrv2.Status {
	objects := rt.Registry.List(objType)
	statuses := make([]stnrv2.Status, 0, len(objects))
	for _, o := range objects {
		r, ok := o.(Reconcilable)
		if !ok {
			continue
		}
		statuses = append(statuses, r.Status())
	}
	return statuses
}

func (rt *Runtime) reconcilable(objType ObjectType, name string) Reconcilable {
	if name == "" {
		name = mustDefaultSingletonName(objType)
	}
	o, ok := rt.Registry.Get(objType, name)
	if !ok {
		return nil
	}
	r, ok := o.(Reconcilable)
	if !ok {
		return nil
	}
	return r
}
