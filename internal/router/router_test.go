package router_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/router"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

// fakeReconcilable is a minimal Reconcilable node used to seed the registry with cluster configs.
type fakeReconcilable struct {
	name   string
	typ    runtime.ObjectType
	config stnrv1.Config
}

func (o *fakeReconcilable) Name() string             { return o.name }
func (o *fakeReconcilable) Type() runtime.ObjectType { return o.typ }
func (o *fakeReconcilable) Start() error             { return nil }
func (o *fakeReconcilable) Close(_ bool) error       { return nil }
func (o *fakeReconcilable) GetConfig() stnrv1.Config { return o.config }
func (o *fakeReconcilable) Status() stnrv1.Status    { return nil }
func (o *fakeReconcilable) Inspect(_, _ stnrv1.Config, _ *stnrv1.StunnerConfig) (runtime.Action, error) {
	return runtime.ActionNone, nil
}
func (o *fakeReconcilable) Reconcile(_ stnrv1.Config) error { return nil }

func newRuntime(t *testing.T) *runtime.Runtime {
	t.Helper()
	log := logger.NewLoggerFactory("all:ERROR")
	rt := runtime.New(runtime.Config{Logger: log, DryRun: true})
	rt.Router = router.NewRouter(rt)
	return rt
}

// addCluster registers a static fake cluster so the Router can resolve and match it via GetConfig.
func addCluster(t *testing.T, rt *runtime.Runtime, name string, proto stnrv1.ClusterProtocol, endpoints ...string) {
	t.Helper()
	c := &fakeReconcilable{
		name: name,
		typ:  runtime.TypeCluster,
		config: &stnrv1.ClusterConfig{
			Name:      name,
			Type:      stnrv1.ClusterTypeStatic.String(),
			Protocol:  proto.String(),
			Endpoints: endpoints,
		},
	}
	require.NoError(t, rt.Registry.Add(c, nil))
}

func TestRouterMatch(t *testing.T) {
	rt := newRuntime(t)
	addCluster(t, rt, "cluster-a", stnrv1.ClusterProtocolUDP, "10.0.0.0/8")

	require.True(t, rt.Router.Match("cluster-a", net.ParseIP("10.0.0.1"), 0))
	require.True(t, rt.Router.Match("cluster-a", net.ParseIP("10.1.2.3"), 1234))
	require.False(t, rt.Router.Match("cluster-a", net.ParseIP("192.168.0.1"), 0))
	require.False(t, rt.Router.Match("nonexistent", net.ParseIP("10.0.0.1"), 0))
}

func TestRouteWithProtocol(t *testing.T) {
	rt := newRuntime(t)
	peer := net.ParseIP("10.0.0.1")

	addCluster(t, rt, "cluster-udp", stnrv1.ClusterProtocolUDP, "10.0.0.0/8")
	addCluster(t, rt, "cluster-tcp", stnrv1.ClusterProtocolTCP, "10.0.0.0/8")

	routes := []string{"cluster-udp", "cluster-tcp"}

	// UDP route resolves only the UDP cluster.
	got, ok := rt.Router.Route("listener", routes, stnrv1.ClusterProtocolUDP, peer, 1234)
	require.True(t, ok)
	require.Equal(t, "cluster-udp", got)

	// TCP route resolves only the TCP cluster.
	got, ok = rt.Router.Route("listener", routes, stnrv1.ClusterProtocolTCP, peer, 1234)
	require.True(t, ok)
	require.Equal(t, "cluster-tcp", got)

	// Peer cache is keyed per-protocol: a cached UDP lookup does not collide with TCP.
	gotUDP, okUDP := rt.Router.Route("listener", routes, stnrv1.ClusterProtocolUDP, peer, 80)
	gotTCP, okTCP := rt.Router.Route("listener", routes, stnrv1.ClusterProtocolTCP, peer, 80)
	require.True(t, okUDP)
	require.True(t, okTCP)
	require.Equal(t, "cluster-udp", gotUDP)
	require.Equal(t, "cluster-tcp", gotTCP)

	// First-match wins: two same-protocol clusters, first in route order is selected.
	addCluster(t, rt, "cluster-udp2", stnrv1.ClusterProtocolUDP, "10.0.0.0/8")
	routes2 := []string{"cluster-udp", "cluster-udp2"}
	rt.Router.InvalidateCache()
	got, ok = rt.Router.Route("listener2", routes2, stnrv1.ClusterProtocolUDP, peer, 5000)
	require.True(t, ok)
	require.Equal(t, "cluster-udp", got)
}
