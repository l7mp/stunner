package object

import (
	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Stunner is the root Object of the dataplane tree. It holds no own runtime state; its job is
// to surface the full StunnerConfig, report live state from descendants, and provide a single
// node the reconciler can root its walk at.
type Stunner struct {
	rt  *runtime.Runtime
	log logging.LeveledLogger
}

// NewStunner creates the singleton root object.
func NewStunner(_ stnrv2.Config, rt *runtime.Runtime) (runtime.Object, error) {
	return &Stunner{
		rt:  rt,
		log: rt.Logger.NewLogger("stunner-root"),
	}, nil
}

func (s *Stunner) Name() string             { return stnrv2.DefaultStunnerName }
func (s *Stunner) Type() runtime.ObjectType { return runtime.TypeStunner }

// GetConfig pulls each top-level child's config from the registry, surfacing the running
// dataplane state. Children missing before the first reconcile are reported as zero values.
func (s *Stunner) GetConfig() stnrv2.Config {
	out := &stnrv2.StunnerConfig{ApiVersion: stnrv2.ApiVersion}
	if s.rt == nil {
		return out
	}

	if a, ok := s.rt.GetConfig(runtime.TypeAdmin, "").(*stnrv2.AdminConfig); ok && a != nil {
		out.Admin = *a
	}
	if a, ok := s.rt.GetConfig(runtime.TypeAuth, "").(*stnrv2.AuthConfig); ok && a != nil {
		out.Auth = *a
	}
	for _, l := range s.rt.GetConfigs(runtime.TypeListener) {
		out.Listeners = append(out.Listeners, *(l.(*stnrv2.ListenerConfig)))
	}
	for _, c := range s.rt.GetConfigs(runtime.TypeServer) {
		out.Servers = append(out.Servers, *(c.(*stnrv2.ServerConfig)))
	}
	for _, c := range s.rt.GetConfigs(runtime.TypeCluster) {
		out.Clusters = append(out.Clusters, *(c.(*stnrv2.ClusterConfig)))
	}

	return out
}

// Status aggregates the children's statuses into a StunnerStatus.
func (s *Stunner) Status() stnrv2.Status {
	status := &stnrv2.StunnerStatus{ApiVersion: stnrv2.ApiVersion}
	if s.rt == nil {
		return status
	}
	if a, ok := s.rt.GetStatus(runtime.TypeAdmin, "").(*stnrv2.AdminStatus); ok {
		status.Admin = a
	}
	if a, ok := s.rt.GetStatus(runtime.TypeAuth, "").(*stnrv2.AuthConfig); ok {
		status.Auth = a
	}
	listenerStatuses := s.rt.GetStatuses(runtime.TypeListener)
	status.Listeners = make([]*stnrv2.ListenerStatus, 0, len(listenerStatuses))
	for _, ls := range listenerStatuses {
		status.Listeners = append(status.Listeners, ls.(*stnrv2.ListenerStatus))
	}
	status.Servers = []*stnrv2.ServerStatus{}
	for _, st := range s.rt.GetStatuses(runtime.TypeServer) {
		status.Servers = append(status.Servers, st.(*stnrv2.ServerStatus))
	}
	status.Clusters = []*stnrv2.ClusterStatus{}
	for _, st := range s.rt.GetStatuses(runtime.TypeCluster) {
		status.Clusters = append(status.Clusters, st.(*stnrv2.ClusterStatus))
	}
	return status
}

// Inspect/Reconcile/Start/Close are no-ops at the root: it has no own state and the tree-walk
// handles descendants.
func (s *Stunner) Inspect(_, _ stnrv2.Config, _ *stnrv2.StunnerConfig) (runtime.Action, error) {
	return runtime.ActionNone, nil
}
func (s *Stunner) Reconcile(_ stnrv2.Config) error { return nil }
func (s *Stunner) Start() error                    { return nil }
func (s *Stunner) Close(_ bool) error              { return nil }
