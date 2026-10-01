package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

type fakeReconcilable struct {
	name   string
	typ    runtime.ObjectType
	config stnrv2.Config
	status stnrv2.Status
}

func (o *fakeReconcilable) Name() string             { return o.name }
func (o *fakeReconcilable) Type() runtime.ObjectType { return o.typ }
func (o *fakeReconcilable) Start() error             { return nil }
func (o *fakeReconcilable) Close(_ bool) error       { return nil }
func (o *fakeReconcilable) GetConfig() stnrv2.Config { return o.config }
func (o *fakeReconcilable) Status() stnrv2.Status    { return o.status }
func (o *fakeReconcilable) Inspect(_, _ stnrv2.Config, _ *stnrv2.StunnerConfig) (runtime.Action, error) {
	return runtime.ActionNone, nil
}
func (o *fakeReconcilable) Reconcile(conf stnrv2.Config) error {
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
		name: stnrv2.DefaultAuthName,
		typ:  runtime.TypeAuth,
		config: &stnrv2.AuthConfig{
			Type:  stnrv2.AuthTypeStatic.String(),
			Realm: "example.org",
			Credentials: map[string]string{
				"username": "u",
				"password": "p",
			},
		},
		status: &stnrv2.AuthStatus{},
	}
	require.NoError(t, rt.Registry.Add(auth, nil))

	// A lifecycle-only node carries neither config nor status.
	lifecycleOnly := runtime.ObjectType("lifecycle-only")
	node := &fakeRunnable{name: "node-a", typ: lifecycleOnly}
	require.NoError(t, rt.Registry.Add(node, nil))

	gotAuth, ok := rt.GetConfig(runtime.TypeAuth, "").(*stnrv2.AuthConfig)
	require.True(t, ok)
	require.Equal(t, "example.org", gotAuth.Realm)

	require.Nil(t, rt.GetConfig(lifecycleOnly, "node-a"))
	require.Empty(t, rt.GetConfigs(lifecycleOnly))
	require.Nil(t, rt.GetStatus(lifecycleOnly, "node-a"))
	require.Empty(t, rt.GetStatuses(lifecycleOnly))
}
