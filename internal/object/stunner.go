package object

import (
	"github.com/pion/logging"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Stunner is the root Object of the dataplane tree. It holds no own runtime state; its job is to
// surface the full StunnerConfig (Extract identity), report live state from descendants, and
// provide a single Object handle the Reconciler can root its walk at.
type Stunner struct {
	reg Registry
	log logging.LeveledLogger
}

// NewStunner creates the singleton root object.
func NewStunner(_ stnrv1.Config, reg Registry, rt *Runtime) (Object, error) {
	return &Stunner{
		reg: reg,
		log: rt.Logger.NewLogger("stunner-root"),
	}, nil
}

func (s *Stunner) ObjectName() string { return stnrv1.DefaultStunnerName }
func (s *Stunner) ObjectType() string { return TypeStunner }

// Extract is identity: the root sees the whole config.
func (s *Stunner) Extract(c *stnrv1.StunnerConfig) (stnrv1.Config, error) { return c, nil }

// GetConfig pulls each top-level child's config from the Registry. Surfaces the
// running dataplane state — semantically equivalent to the old Stunner.GetConfig in ./config.go.
func (s *Stunner) GetConfig() stnrv1.Config {
	out := &stnrv1.StunnerConfig{ApiVersion: stnrv1.ApiVersion}
	if s.reg == nil {
		return out
	}

	if a, ok := s.reg.LookupOne(TypeAdmin); ok {
		out.Admin = *a.GetConfig().(*stnrv1.AdminConfig)
	}
	if a, ok := s.reg.LookupOne(TypeAuth); ok {
		out.Auth = *a.GetConfig().(*stnrv1.AuthConfig)
	}
	if l, ok := s.reg.LookupOne(TypeListenerList); ok {
		out.Listeners = append([]stnrv1.ListenerConfig(nil), l.GetConfig().(*ListenerListConfig).Listeners...)
	}
	if c, ok := s.reg.LookupOne(TypeClusterList); ok {
		out.Clusters = append([]stnrv1.ClusterConfig(nil), c.GetConfig().(*ClusterListConfig).Clusters...)
	}

	return out
}

// Status aggregates the children's statuses into a StunnerStatus.
func (s *Stunner) Status() stnrv1.Status {
	status := &stnrv1.StunnerStatus{ApiVersion: stnrv1.ApiVersion}
	if s.reg == nil {
		return status
	}
	if a, ok := s.reg.LookupOne(TypeAdmin); ok {
		if as, ok := a.Status().(*stnrv1.AdminStatus); ok {
			status.Admin = as
		}
	}
	if a, ok := s.reg.LookupOne(TypeAuth); ok {
		if as, ok := a.Status().(*stnrv1.AuthConfig); ok {
			status.Auth = as
		}
	}
	listeners := s.reg.LookupAll(TypeListener)
	status.Listeners = make([]*stnrv1.ListenerStatus, 0, len(listeners))
	for _, l := range listeners {
		if ls, ok := l.Status().(*stnrv1.ListenerStatus); ok {
			status.Listeners = append(status.Listeners, ls)
		}
	}
	clusters := s.reg.LookupAll(TypeCluster)
	status.Clusters = make([]*stnrv1.ClusterStatus, 0, len(clusters))
	for _, c := range clusters {
		if cs, ok := c.Status().(*stnrv1.ClusterStatus); ok {
			status.Clusters = append(status.Clusters, cs)
		}
	}
	return status
}

// Inspect/Reconcile/Start/Close are no-ops at the root: it has no own state and the tree-walk
// handles descendants.
func (s *Stunner) Inspect(_, _ stnrv1.Config, _ *stnrv1.StunnerConfig) (Action, error) {
	return ActionNone, nil
}
func (s *Stunner) Reconcile(_ stnrv1.Config) error { return nil }
func (s *Stunner) Start() error                    { return nil }
func (s *Stunner) Close(_ bool) error              { return nil }
