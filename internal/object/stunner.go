package object

import (
	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Stunner is the root Object of the dataplane tree. It holds no own runtime state; its job is
// to surface the full StunnerConfig, report live state from descendants, and provide a single
// node the reconciler can root its walk at.
type Stunner struct {
	rt  *runtime.Runtime
	log logging.LeveledLogger
}

// NewStunner creates the singleton root object.
func NewStunner(_ stnrv1.Config, rt *runtime.Runtime) (runtime.Object, error) {
	return &Stunner{
		rt:  rt,
		log: rt.Logger.NewLogger("stunner-root"),
	}, nil
}

func (s *Stunner) Name() string             { return stnrv1.DefaultStunnerName }
func (s *Stunner) Type() runtime.ObjectType { return runtime.TypeStunner }

// GetConfig pulls each top-level child's config from the registry, surfacing the running
// dataplane state. Children missing before the first reconcile are reported as zero values.
func (s *Stunner) GetConfig() stnrv1.Config {
	out := &stnrv1.StunnerConfig{ApiVersion: stnrv1.ApiVersion}
	if s.rt == nil {
		return out
	}

	if a, ok := s.rt.GetConfig(runtime.TypeAdmin, "").(*stnrv1.AdminConfig); ok && a != nil {
		out.Admin = *a
	}
	if a, ok := s.rt.GetConfig(runtime.TypeAuth, "").(*stnrv1.AuthConfig); ok && a != nil {
		out.Auth = *a
	}
	for _, l := range s.rt.GetConfigs(runtime.TypeListener) {
		out.Listeners = append(out.Listeners, *(l.(*stnrv1.ListenerConfig)))
	}
	for _, c := range s.rt.GetConfigs(runtime.TypeCluster) {
		out.Clusters = append(out.Clusters, *(c.(*stnrv1.ClusterConfig)))
	}

	return out
}

// Status aggregates the children's statuses into a StunnerStatus.
func (s *Stunner) Status() stnrv1.Status {
	status := &stnrv1.StunnerStatus{ApiVersion: stnrv1.ApiVersion}
	if s.rt == nil {
		return status
	}
	if a, ok := s.rt.GetStatus(runtime.TypeAdmin, "").(*stnrv1.AdminStatus); ok {
		status.Admin = a
	}
	if a, ok := s.rt.GetStatus(runtime.TypeAuth, "").(*stnrv1.AuthConfig); ok {
		status.Auth = a
	}
	listenerStatuses := s.rt.GetStatuses(runtime.TypeListener)
	status.Listeners = make([]*stnrv1.ListenerStatus, 0, len(listenerStatuses))
	for _, ls := range listenerStatuses {
		status.Listeners = append(status.Listeners, ls.(*stnrv1.ListenerStatus))
	}
	clusterStatuses := s.rt.GetStatuses(runtime.TypeCluster)
	status.Clusters = make([]*stnrv1.ClusterStatus, 0, len(clusterStatuses))
	for _, cs := range clusterStatuses {
		status.Clusters = append(status.Clusters, cs.(*stnrv1.ClusterStatus))
	}
	return status
}

// Inspect/Reconcile/Start/Close are no-ops at the root: it has no own state and the tree-walk
// handles descendants.
func (s *Stunner) Inspect(_, _ stnrv1.Config, _ *stnrv1.StunnerConfig) (runtime.Action, error) {
	return runtime.ActionNone, nil
}
func (s *Stunner) Reconcile(_ stnrv1.Config) error { return nil }
func (s *Stunner) Start() error                    { return nil }
func (s *Stunner) Close(_ bool) error              { return nil }
