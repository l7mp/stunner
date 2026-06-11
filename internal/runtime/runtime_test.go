package runtime_test

import (
	"net"
	"testing"

	"github.com/pion/turn/v5"
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

type fakeRelay struct {
	fakeRunnable
}

func (r *fakeRelay) Validate() error { return nil }
func (r *fakeRelay) AllocatePacketConn(turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	return nil, nil, nil
}
func (r *fakeRelay) AllocateListener(turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	return nil, nil, nil
}
func (r *fakeRelay) AllocateConn(turn.AllocateConnConfig) (net.Conn, error) {
	return nil, nil
}
func (r *fakeRelay) ClusterName() string              { return "cluster-a" }
func (r *fakeRelay) Protocol() stnrv1.ClusterProtocol { return stnrv1.ClusterProtocolUDP }
func (r *fakeRelay) Match(_ net.IP, _ int) bool       { return true }

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

func TestRelayLookup(t *testing.T) {
	rt := newRuntime(t)

	relayName := runtime.RelayName("cluster-a", stnrv1.ClusterProtocolUDP)
	relay := &fakeRelay{fakeRunnable{name: relayName, typ: runtime.TypeRelay}}
	require.NoError(t, rt.Registry.Add(relay, nil))

	got, ok := rt.GetRelay("cluster-a", stnrv1.ClusterProtocolUDP)
	require.True(t, ok)
	require.Equal(t, "cluster-a", got.ClusterName())

	_, ok = rt.GetRelay("cluster-a", stnrv1.ClusterProtocolTCP)
	require.False(t, ok)
}
