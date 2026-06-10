package reconciler

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

const (
	testRootType  runtime.ObjectType = runtime.TypeStunner
	testGroupType runtime.ObjectType = "test-group"
	testItemType  runtime.ObjectType = "test-item"
)

var (
	errInspect   = errors.New("inspect failed")
	errReconcile = errors.New("reconcile failed")
	errCreate    = errors.New("create failed")
	errStart     = errors.New("start failed")
	errClose     = errors.New("close failed")
)

type fakeErrMode int

const (
	fakeErrNone fakeErrMode = iota
	fakeErrRestartRequired
	fakeErrFatal
)

type fakeConfig struct {
	Name             string
	Decision         runtime.Action
	InspectErr       bool
	ReconcileErrMode fakeErrMode
	CreateErrMode    fakeErrMode
	StartErr         bool
	CloseErr         bool
	ShutdownCloseErr bool
}

func (c *fakeConfig) clone() *fakeConfig {
	d := *c
	return &d
}

func (c *fakeConfig) Validate() error { return nil }

func (c *fakeConfig) ConfigName() string { return c.Name }

func (c *fakeConfig) DeepEqual(other stnrv1.Config) bool {
	o, ok := other.(*fakeConfig)
	if !ok {
		return false
	}
	return *c == *o
}

func (c *fakeConfig) DeepCopyInto(dst stnrv1.Config) {
	out := dst.(*fakeConfig)
	*out = *c
}

func (c *fakeConfig) String() string {
	return fmt.Sprintf("fake-config:%s", c.Name)
}

type fakeStatus struct{}

func (fakeStatus) String() string { return "fake-status" }

type eventRecorder struct {
	events []string
}

func (r *eventRecorder) add(event string) {
	r.events = append(r.events, event)
}

func (r *eventRecorder) snapshot() []string {
	out := make([]string, len(r.events))
	copy(out, r.events)
	return out
}

type fakeFactory struct {
	objType runtime.ObjectType
	rec     *eventRecorder
}

func (f *fakeFactory) New(conf stnrv1.Config) (runtime.Runnable, error) {
	cfg := conf.(*fakeConfig).clone()
	f.rec.add(fmt.Sprintf("factory:%s/%s", f.objType, cfg.Name))

	obj := &fakeObject{
		objType: f.objType,
		cfg:     cfg,
		rec:     f.rec,
	}

	switch cfg.CreateErrMode {
	case fakeErrFatal:
		return nil, errCreate
	case fakeErrRestartRequired:
		return obj, object.ErrRestartRequired
	default:
		return obj, nil
	}
}

type fakeObject struct {
	objType runtime.ObjectType
	cfg     *fakeConfig
	rec     *eventRecorder
}

func (o *fakeObject) Name() string { return o.cfg.Name }

func (o *fakeObject) Type() runtime.ObjectType { return o.objType }

func (o *fakeObject) GetConfig() stnrv1.Config {
	return o.cfg.clone()
}

func (o *fakeObject) Status() stnrv1.Status {
	return fakeStatus{}
}

func (o *fakeObject) Inspect(_, conf stnrv1.Config, _ *stnrv1.StunnerConfig) (runtime.Action, error) {
	cfg := conf.(*fakeConfig)
	o.rec.add(fmt.Sprintf("inspect:%s/%s:%d", o.objType, o.cfg.Name, cfg.Decision))

	if cfg.InspectErr {
		return runtime.ActionNone, errInspect
	}

	return cfg.Decision, nil
}

func (o *fakeObject) Reconcile(conf stnrv1.Config) error {
	cfg := conf.(*fakeConfig).clone()
	o.rec.add(fmt.Sprintf("reconcile:%s/%s", o.objType, o.cfg.Name))
	o.cfg = cfg

	switch cfg.ReconcileErrMode {
	case fakeErrFatal:
		return errReconcile
	case fakeErrRestartRequired:
		return object.ErrRestartRequired
	default:
		return nil
	}
}

func (o *fakeObject) Start() error {
	o.rec.add(fmt.Sprintf("start:%s/%s", o.objType, o.cfg.Name))
	if o.cfg.StartErr {
		return errStart
	}
	return nil
}

func (o *fakeObject) Close(shutdown bool) error {
	o.rec.add(fmt.Sprintf("close:%s/%s:%t", o.objType, o.cfg.Name, shutdown))
	if shutdown && o.cfg.ShutdownCloseErr {
		return errClose
	}
	if !shutdown && o.cfg.CloseErr {
		return errClose
	}
	return nil
}

type testEnv struct {
	t          *testing.T
	rt         *runtime.Runtime
	rec        *eventRecorder
	desired    map[runtime.ObjectType][]stnrv1.Config
	reconciler *Reconciler
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	rec := &eventRecorder{}
	logFactory := logger.NewLoggerFactory("all:ERROR")
	rt := runtime.New(runtime.Deps{Logger: logFactory, DryRun: true})

	env := &testEnv{
		t:       t,
		rt:      rt,
		rec:     rec,
		desired: map[runtime.ObjectType][]stnrv1.Config{},
	}

	catalog := object.NewCatalogFromKinds(
		object.KindSpec{
			Type:     testRootType,
			Children: []runtime.ObjectType{testGroupType},
			New: func(_ runtime.Runnable, conf stnrv1.Config, _ *runtime.Runtime) (runtime.Runnable, error) {
				return (&fakeFactory{objType: testRootType, rec: rec}).New(conf)
			},
			DesiredConfigs: func(_ runtime.Runnable, _ *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
				return env.extractor(testRootType)(), nil
			},
			Singleton:     true,
			SingletonName: func(_ runtime.Runnable) string { return "root" },
		},
		object.KindSpec{
			Type:     testGroupType,
			Children: []runtime.ObjectType{testItemType},
			New: func(_ runtime.Runnable, conf stnrv1.Config, _ *runtime.Runtime) (runtime.Runnable, error) {
				return (&fakeFactory{objType: testGroupType, rec: rec}).New(conf)
			},
			DesiredConfigs: func(_ runtime.Runnable, _ *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
				return env.extractor(testGroupType)(), nil
			},
		},
		object.KindSpec{
			Type: testItemType,
			New: func(_ runtime.Runnable, conf stnrv1.Config, _ *runtime.Runtime) (runtime.Runnable, error) {
				return (&fakeFactory{objType: testItemType, rec: rec}).New(conf)
			},
			DesiredConfigs: func(_ runtime.Runnable, _ *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
				return env.extractor(testItemType)(), nil
			},
		},
	)

	env.reconciler = New(catalog, rt, logFactory)

	return env
}

func (e *testEnv) extractor(objType runtime.ObjectType) func() []stnrv1.Config {
	return func() []stnrv1.Config {
		confs := e.desired[objType]
		out := make([]stnrv1.Config, len(confs))
		for i := range confs {
			out[i] = confs[i].(*fakeConfig).clone()
		}
		return out
	}
}

func (e *testEnv) addExisting(objType runtime.ObjectType, cfg *fakeConfig) *fakeObject {
	e.t.Helper()

	o := &fakeObject{objType: objType, cfg: cfg.clone(), rec: e.rec}

	var parent runtime.Runnable
	switch objType {
	case testRootType:
		parent = nil
	case testGroupType:
		p, ok := e.rt.Registry.Get(testRootType, "root")
		require.True(e.t, ok)
		parent = p
	case testItemType:
		p, ok := e.rt.Registry.Get(testGroupType, "group")
		require.True(e.t, ok)
		parent = p
	}

	require.NoError(e.t, e.rt.Registry.Add(o, parent))

	return o
}

func (e *testEnv) setDesired(objType runtime.ObjectType, cfgs ...*fakeConfig) {
	e.t.Helper()

	out := make([]stnrv1.Config, len(cfgs))
	for i := range cfgs {
		out[i] = cfgs[i].clone()
	}
	e.desired[objType] = out
}

func (e *testEnv) seedBaseTree() {
	e.t.Helper()

	e.addExisting(testRootType, &fakeConfig{Name: "root"})
	e.addExisting(testGroupType, &fakeConfig{Name: "group"})

	e.setDesired(testRootType, &fakeConfig{Name: "root"})
	e.setDesired(testGroupType, &fakeConfig{Name: "group"})
}

func assertOrderedSubsequence(t *testing.T, got []string, want []string) {
	t.Helper()

	idx := 0
	for _, w := range want {
		found := false
		for idx < len(got) {
			if got[idx] == w {
				idx++
				found = true
				break
			}
			idx++
		}
		require.Truef(t, found, "event %q is missing from event stream: %v", w, got)
	}
}

func assertNoPrefix(t *testing.T, got []string, prefix string) {
	t.Helper()

	for _, e := range got {
		require.NotContainsf(t, e, prefix, "unexpected %q event in %v", prefix, got)
	}
}

func TestReconcileFlow(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "reconcile"})
	env.addExisting(testItemType, &fakeConfig{Name: "restart"})
	env.addExisting(testItemType, &fakeConfig{Name: "delete"})

	env.setDesired(testItemType,
		&fakeConfig{Name: "reconcile", Decision: runtime.ActionReconcile},
		&fakeConfig{Name: "restart", Decision: runtime.ActionRestart},
		&fakeConfig{Name: "new"},
	)

	err := env.reconciler.run(&stnrv1.StunnerConfig{}, false)
	var restarted stnrv1.ErrRestarted
	require.ErrorAs(t, err, &restarted)
	require.Equal(t, []string{fmt.Sprintf("%s: %s", testItemType, "restart")}, restarted.Objects)

	_, foundDeleted := env.rt.Registry.Get(testItemType, "delete")
	require.False(t, foundDeleted)
	_, foundNew := env.rt.Registry.Get(testItemType, "new")
	require.True(t, foundNew)

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{
		"close:test-item/restart:false",
		"close:test-item/delete:false",
		"reconcile:test-item/reconcile",
		"reconcile:test-item/restart",
		"factory:test-item/new",
		"start:test-item/restart",
		"start:test-item/new",
	})
}

func TestDryRun(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "old"})
	env.setDesired(testItemType,
		&fakeConfig{Name: "old", Decision: runtime.ActionRestart},
		&fakeConfig{Name: "new"},
	)

	err := env.reconciler.run(&stnrv1.StunnerConfig{}, true)
	var restarted stnrv1.ErrRestarted
	require.ErrorAs(t, err, &restarted)
	require.Equal(t, []string{fmt.Sprintf("%s: %s", testItemType, "old")}, restarted.Objects)

	oldObj, oldFound := env.rt.Registry.Get(testItemType, "old")
	require.True(t, oldFound)
	oldReconcilable, ok := oldObj.(runtime.Object)
	require.True(t, ok)
	oldCfg, ok := oldReconcilable.GetConfig().(*fakeConfig)
	require.True(t, ok)
	require.Equal(t, runtime.ActionRestart, oldCfg.Decision)

	newObj, newFound := env.rt.Registry.Get(testItemType, "new")
	require.True(t, newFound)
	newReconcilable, ok := newObj.(runtime.Object)
	require.True(t, ok)
	newCfg, ok := newReconcilable.GetConfig().(*fakeConfig)
	require.True(t, ok)
	require.Equal(t, "new", newCfg.Name)

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{
		"reconcile:test-item/old",
		"factory:test-item/new",
	})
	assertNoPrefix(t, events, "close:")
	assertNoPrefix(t, events, "start:")
}

func TestDeleteSubtree(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "a"})
	env.addExisting(testItemType, &fakeConfig{Name: "b"})

	env.setDesired(testGroupType)
	env.setDesired(testItemType)

	err := env.reconciler.run(&stnrv1.StunnerConfig{}, false)
	require.NoError(t, err)

	_, rootFound := env.rt.Registry.Get(testRootType, "root")
	require.True(t, rootFound)
	_, groupFound := env.rt.Registry.Get(testGroupType, "group")
	require.False(t, groupFound)
	_, aFound := env.rt.Registry.Get(testItemType, "a")
	require.False(t, aFound)
	_, bFound := env.rt.Registry.Get(testItemType, "b")
	require.False(t, bFound)

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{
		"close:test-item/a:false",
		"close:test-item/b:false",
		"close:test-group/group:false",
	})
	assertNoPrefix(t, events, "reconcile:")
	assertNoPrefix(t, events, "factory:")
	assertNoPrefix(t, events, "start:")
}

func TestInspectErr(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "bad"})
	env.setDesired(testItemType, &fakeConfig{Name: "bad", InspectErr: true})

	err := env.reconciler.run(&stnrv1.StunnerConfig{}, false)
	require.ErrorIs(t, err, errInspect)

	events := env.rec.snapshot()
	assertNoPrefix(t, events, "close:")
	assertNoPrefix(t, events, "reconcile:")
	assertNoPrefix(t, events, "factory:")
	assertNoPrefix(t, events, "start:")

	_, found := env.rt.Registry.Get(testItemType, "bad")
	require.True(t, found)
}

func TestReconcileErr(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "existing"})
	env.setDesired(testItemType,
		&fakeConfig{Name: "existing", Decision: runtime.ActionReconcile, ReconcileErrMode: fakeErrFatal},
		&fakeConfig{Name: "new"},
	)

	err := env.reconciler.run(&stnrv1.StunnerConfig{}, false)
	require.ErrorIs(t, err, errReconcile)

	_, found := env.rt.Registry.Get(testItemType, "new")
	require.False(t, found)

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{"reconcile:test-item/existing"})
	assertNoPrefix(t, events, "factory:")
	assertNoPrefix(t, events, "start:")
}

func TestStartErr(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()
	env.setDesired(testItemType, &fakeConfig{Name: "new", StartErr: true})

	err := env.reconciler.run(&stnrv1.StunnerConfig{}, false)
	require.ErrorIs(t, err, errStart)

	_, found := env.rt.Registry.Get(testItemType, "new")
	require.True(t, found)

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{
		"factory:test-item/new",
		"start:test-item/new",
	})
}

func TestReconcileRestartRequired(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "existing"})
	env.setDesired(testItemType,
		&fakeConfig{Name: "existing", Decision: runtime.ActionReconcile, ReconcileErrMode: fakeErrRestartRequired},
	)

	err := env.reconciler.run(&stnrv1.StunnerConfig{}, false)
	require.ErrorIs(t, err, object.ErrRestartRequired)

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{"reconcile:test-item/existing"})
	assertNoPrefix(t, events, "close:")
	assertNoPrefix(t, events, "start:")
}

func TestCreateRestartRequired(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.setDesired(testItemType,
		&fakeConfig{Name: "new", CreateErrMode: fakeErrRestartRequired},
	)

	err := env.reconciler.run(&stnrv1.StunnerConfig{}, false)
	require.ErrorIs(t, err, object.ErrRestartRequired)

	_, found := env.rt.Registry.Get(testItemType, "new")
	require.False(t, found)

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{"factory:test-item/new"})
	assertNoPrefix(t, events, "start:")
}

func TestSingletonWrongName(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.setDesired(testRootType, &fakeConfig{Name: "renamed-root"})

	err := env.reconciler.run(&stnrv1.StunnerConfig{}, false)
	require.ErrorContains(t, err, "singleton item must be named")

	events := env.rec.snapshot()
	assertNoPrefix(t, events, "reconcile:")
	assertNoPrefix(t, events, "factory:")
	assertNoPrefix(t, events, "close:")
	assertNoPrefix(t, events, "start:")
}

func TestSingletonCardinality(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.setDesired(testRootType, &fakeConfig{Name: "root"}, &fakeConfig{Name: "root"})

	err := env.reconciler.run(&stnrv1.StunnerConfig{}, false)
	require.ErrorContains(t, err, "singleton resolver returned")

	events := env.rec.snapshot()
	assertNoPrefix(t, events, "reconcile:")
	assertNoPrefix(t, events, "factory:")
	assertNoPrefix(t, events, "close:")
	assertNoPrefix(t, events, "start:")
}

func TestShutdownTree(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "a", ShutdownCloseErr: true})
	env.addExisting(testItemType, &fakeConfig{Name: "b"})

	err := env.reconciler.Shutdown()
	require.NoError(t, err)

	require.Empty(t, env.rt.Registry.List(testRootType))
	require.Empty(t, env.rt.Registry.List(testGroupType))
	require.Empty(t, env.rt.Registry.List(testItemType))

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{
		"close:test-item/a:true",
		"close:test-item/b:true",
		"close:test-group/group:true",
		fmt.Sprintf("close:%s/root:true", testRootType),
	})
}
