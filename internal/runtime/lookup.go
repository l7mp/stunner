package runtime

import (
	"strings"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

var defaultSingletonNames = map[ObjectType]string{
	TypeStunner: stnrv1.DefaultStunnerName,
	TypeAdmin:   stnrv1.DefaultAdminName,
	TypeAuth:    stnrv1.DefaultAuthName,
	TypeHealth:  stnrv1.DefaultHealthName,
	TypeMetrics: stnrv1.DefaultMetricsName,
	TypeOffload: stnrv1.DefaultOffloadName,
}

// DefaultSingletonName returns the canonical singleton object name for an object type.
func DefaultSingletonName(objType ObjectType) (string, bool) {
	name, ok := defaultSingletonNames[objType]
	return name, ok
}

// MustDefaultSingletonName returns the canonical singleton object name for an object type.
func MustDefaultSingletonName(objType ObjectType) string {
	name, ok := DefaultSingletonName(objType)
	if !ok {
		panic("no default singleton name for type: " + string(objType))
	}
	return name
}

// GetConfig returns the live config of a node. An empty name resolves to the canonical
// singleton name of the type. Returns nil if the node is missing or not Reconcilable.
func (rt *Runtime) GetConfig(objType ObjectType, name string) stnrv1.Config {
	r := rt.reconcilable(objType, name)
	if r == nil {
		return nil
	}
	return r.GetConfig()
}

// GetConfigs returns the live configs of every Reconcilable node of a type, in stable order.
// Lifecycle-only nodes are skipped.
func (rt *Runtime) GetConfigs(objType ObjectType) []stnrv1.Config {
	objects := rt.Registry.List(objType)
	configs := make([]stnrv1.Config, 0, len(objects))
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
func (rt *Runtime) GetStatus(objType ObjectType, name string) stnrv1.Status {
	r := rt.reconcilable(objType, name)
	if r == nil {
		return nil
	}
	return r.Status()
}

// GetStatuses returns the live statuses of every Reconcilable node of a type, in stable
// order. Lifecycle-only nodes are skipped.
func (rt *Runtime) GetStatuses(objType ObjectType) []stnrv1.Status {
	objects := rt.Registry.List(objType)
	statuses := make([]stnrv1.Status, 0, len(objects))
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
		name = MustDefaultSingletonName(objType)
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

// RelayName builds the registry name of a cluster relay node. It is the single authority on
// relay naming: relays are keyed by cluster plus protocol today; when multiple relays per
// protocol become possible (e.g., port-qualified relays), extend the key here.
func RelayName(cluster string, proto stnrv1.ClusterProtocol) string {
	return cluster + "/" + strings.ToLower(proto.String())
}

// GetRelay resolves the relay of a cluster for a given protocol from the registry.
func (rt *Runtime) GetRelay(cluster string, proto stnrv1.ClusterProtocol) (Relay, bool) {
	o, ok := rt.Registry.Get(TypeRelay, RelayName(cluster, proto))
	if !ok {
		return nil, false
	}
	r, ok := o.(Relay)
	if !ok {
		return nil, false
	}
	return r, true
}
