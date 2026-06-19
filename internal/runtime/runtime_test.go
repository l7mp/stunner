package runtime_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

type fakeReconcilable struct {
	name   string
	typ    runtime.ObjectType
	config stnrv1.Config
	status stnrv1.Status
}

func (o *fakeReconcilable) Name() string             { return o.name }
func (o *fakeReconcilable) Type() runtime.ObjectType { return o.typ }
func (o *fakeReconcilable) Start() error             { return nil }
func (o *fakeReconcilable) Close(_ bool) error       { return nil }
func (o *fakeReconcilable) GetConfig() stnrv1.Config { return o.config }
func (o *fakeReconcilable) Status() stnrv1.Status    { return o.status }
func (o *fakeReconcilable) Inspect(_, _ stnrv1.Config, _ *stnrv1.StunnerConfig) (runtime.Action, error) {
	return runtime.ActionNone, nil
}
func (o *fakeReconcilable) Reconcile(conf stnrv1.Config) error {
	o.config = conf
	return nil
}

type fakeRunnable struct {
	name string
	typ  runtime.ObjectType
}

func (o *fakeRunnable) Name() string             { return o.name }
func (o *fakeRunnable) Type() runtime.ObjectType { return o.typ }
func (o *fakeRunnable) Start() error             { return nil }
func (o *fakeRunnable) Close(_ bool) error       { return nil }

func newRuntime(t *testing.T) *runtime.Runtime {
	t.Helper()
	log := logger.NewLoggerFactory("all:ERROR")
	return runtime.New(runtime.Config{Logger: log, DryRun: true})
}

func TestRegistryChildrenAndOrdering(t *testing.T) {
	rt := newRuntime(t)

	root := &fakeRunnable{name: "root", typ: runtime.TypeStunner}
	require.NoError(t, rt.Registry.Add(root, nil))

	b := &fakeRunnable{name: "b", typ: runtime.TypeListener}
	a := &fakeRunnable{name: "a", typ: runtime.TypeListener}
	require.NoError(t, rt.Registry.Add(b, root))
	require.NoError(t, rt.Registry.Add(a, root))

	rootList := rt.Registry.ChildrenOf(nil, runtime.TypeStunner)
	require.Len(t, rootList, 1)
	require.Equal(t, "root", rootList[0].Name())

	children := rt.Registry.ChildrenOf(root, runtime.TypeListener)
	require.Len(t, children, 2)
	require.Equal(t, "a", children[0].Name())
	require.Equal(t, "b", children[1].Name())

	require.NoError(t, rt.Registry.Remove(a))
	children = rt.Registry.ChildrenOf(root, runtime.TypeListener)
	require.Len(t, children, 1)
	require.Equal(t, "b", children[0].Name())
}

func TestLookupSkipsLifecycleOnly(t *testing.T) {
	rt := newRuntime(t)

	auth := &fakeReconcilable{
		name: stnrv1.DefaultAuthName,
		typ:  runtime.TypeAuth,
		config: &stnrv1.AuthConfig{
			Type:  stnrv1.AuthTypeStatic.String(),
			Realm: "example.org",
			Credentials: map[string]string{
				"username": "u",
				"password": "p",
			},
		},
		status: &stnrv1.AuthStatus{},
	}
	require.NoError(t, rt.Registry.Add(auth, nil))

	listenerServer := &fakeRunnable{name: "listener-a", typ: runtime.TypeListenerServer}
	require.NoError(t, rt.Registry.Add(listenerServer, nil))

	gotAuth, ok := rt.GetConfig(runtime.TypeAuth, "").(*stnrv1.AuthConfig)
	require.True(t, ok)
	require.Equal(t, "example.org", gotAuth.Realm)

	require.Nil(t, rt.GetConfig(runtime.TypeListenerServer, "listener-a"))
	require.Empty(t, rt.GetConfigs(runtime.TypeListenerServer))
	require.Nil(t, rt.GetStatus(runtime.TypeListenerServer, "listener-a"))
	require.Empty(t, rt.GetStatuses(runtime.TypeListenerServer))
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
