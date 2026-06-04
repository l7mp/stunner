package manager

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/object"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

const (
	testRootType  = "test-root"
	testGroupType = "test-group"
	testItemType  = "test-item"
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
	Decision         object.Action
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
	objType string
	rec     *eventRecorder
}

func (f *fakeFactory) New(conf stnrv1.Config, _ object.Registry) (object.Object, error) {
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
	objType string
	cfg     *fakeConfig
	rec     *eventRecorder
}

func (o *fakeObject) ObjectName() string { return o.cfg.Name }

func (o *fakeObject) ObjectType() string { return o.objType }

func (o *fakeObject) Extract(_ *stnrv1.StunnerConfig) (stnrv1.Config, error) {
	return o.cfg.clone(), nil
}

func (o *fakeObject) GetConfig() stnrv1.Config {
	return o.cfg.clone()
}

func (o *fakeObject) Status() stnrv1.Status {
	return fakeStatus{}
}

func (o *fakeObject) Inspect(_, conf stnrv1.Config, _ *stnrv1.StunnerConfig) (object.Action, error) {
	cfg := conf.(*fakeConfig)
	o.rec.add(fmt.Sprintf("inspect:%s/%s:%d", o.objType, o.cfg.Name, cfg.Decision))

	if cfg.InspectErr {
		return object.ActionNone, errInspect
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
	t       *testing.T
	reg     Registry
	rec     *eventRecorder
	desired map[string][]stnrv1.Config

	rootManager  *Manager
	groupManager *Manager
	itemManager  *Manager
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	rec := &eventRecorder{}
	reg := NewRegistry()
	logFactory := logger.NewLoggerFactory("all:ERROR")

	env := &testEnv{
		t:       t,
		reg:     reg,
		rec:     rec,
		desired: map[string][]stnrv1.Config{},
	}

	env.rootManager = NewManager("test-root-manager", testRootType,
		(&fakeFactory{objType: testRootType, rec: rec}).New, env.extractor(testRootType), reg, logFactory,
		WithSingleton("root"))
	env.groupManager = NewManager("test-group-manager", testGroupType,
		(&fakeFactory{objType: testGroupType, rec: rec}).New, env.extractor(testGroupType), reg, logFactory)
	env.itemManager = NewManager("test-item-manager", testItemType,
		(&fakeFactory{objType: testItemType, rec: rec}).New, env.extractor(testItemType), reg, logFactory)

	return env
}

func (e *testEnv) extractor(objType string) ListExtractor {
	return func(_ *stnrv1.StunnerConfig) ([]stnrv1.Config, error) {
		confs := e.desired[objType]
		out := make([]stnrv1.Config, len(confs))
		for i := range confs {
			out[i] = confs[i].(*fakeConfig).clone()
		}
		return out, nil
	}
}

func (e *testEnv) addExisting(objType string, cfg *fakeConfig) *fakeObject {
	e.t.Helper()

	o := &fakeObject{objType: objType, cfg: cfg.clone(), rec: e.rec}
	require.NoError(e.t, e.reg.Add(o))

	return o
}

func (e *testEnv) setDesired(objType string, cfgs ...*fakeConfig) {
	e.t.Helper()

	out := make([]stnrv1.Config, len(cfgs))
	for i := range cfgs {
		out[i] = cfgs[i].clone()
	}
	e.desired[objType] = out
}

func (e *testEnv) seedBaseTree() {
	e.t.Helper()

	root := e.addExisting(testRootType, &fakeConfig{Name: "root"})
	group := e.addExisting(testGroupType, &fakeConfig{Name: "group"})

	require.NoError(e.t, e.reg.AttachSubManager(root, e.groupManager))
	require.NoError(e.t, e.reg.AttachSubManager(group, e.itemManager))

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

// Test full lifecycle with mixed reconcile, restart, create, and delete actions.
func TestReconcileFlow(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "reconcile"})
	env.addExisting(testItemType, &fakeConfig{Name: "restart"})
	env.addExisting(testItemType, &fakeConfig{Name: "delete"})

	env.setDesired(testItemType,
		&fakeConfig{Name: "reconcile", Decision: object.ActionReconcile},
		&fakeConfig{Name: "restart", Decision: object.ActionRestart},
		&fakeConfig{Name: "new"},
	)

	err := env.rootManager.reconcile(&stnrv1.StunnerConfig{}, false)
	var restarted stnrv1.ErrRestarted
	require.ErrorAs(t, err, &restarted)
	require.Equal(t, []string{fmt.Sprintf("%s: %s", testItemType, "restart")}, restarted.Objects)

	_, foundDeleted := env.reg.Lookup(testItemType, "delete")
	require.False(t, foundDeleted)
	_, foundNew := env.reg.Lookup(testItemType, "new")
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

// Test that dry-run applies config changes but skips close/start side effects.
func TestDryRun(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "old"})
	env.setDesired(testItemType,
		&fakeConfig{Name: "old", Decision: object.ActionRestart},
		&fakeConfig{Name: "new"},
	)

	err := env.rootManager.reconcile(&stnrv1.StunnerConfig{}, true)
	var restarted stnrv1.ErrRestarted
	require.ErrorAs(t, err, &restarted)
	require.Equal(t, []string{fmt.Sprintf("%s: %s", testItemType, "old")}, restarted.Objects)

	oldObj, oldFound := env.reg.Lookup(testItemType, "old")
	require.True(t, oldFound)
	oldCfg, ok := oldObj.GetConfig().(*fakeConfig)
	require.True(t, ok)
	require.Equal(t, object.ActionRestart, oldCfg.Decision)

	newObj, newFound := env.reg.Lookup(testItemType, "new")
	require.True(t, newFound)
	newCfg, ok := newObj.GetConfig().(*fakeConfig)
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

// Test recursive subtree deletion when a parent disappears from desired config.
func TestDeleteSubtree(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "a"})
	env.addExisting(testItemType, &fakeConfig{Name: "b"})

	env.setDesired(testGroupType)
	env.setDesired(testItemType)

	err := env.rootManager.reconcile(&stnrv1.StunnerConfig{}, false)
	require.NoError(t, err)

	_, rootFound := env.reg.Lookup(testRootType, "root")
	require.True(t, rootFound)
	_, groupFound := env.reg.Lookup(testGroupType, "group")
	require.False(t, groupFound)
	_, aFound := env.reg.Lookup(testItemType, "a")
	require.False(t, aFound)
	_, bFound := env.reg.Lookup(testItemType, "b")
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

// Test that inspect errors abort reconciliation before any lifecycle operation.
func TestInspectErr(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "bad"})
	env.setDesired(testItemType, &fakeConfig{Name: "bad", InspectErr: true})

	err := env.rootManager.reconcile(&stnrv1.StunnerConfig{}, false)
	require.ErrorIs(t, err, errInspect)

	events := env.rec.snapshot()
	assertNoPrefix(t, events, "close:")
	assertNoPrefix(t, events, "reconcile:")
	assertNoPrefix(t, events, "factory:")
	assertNoPrefix(t, events, "start:")

	_, found := env.reg.Lookup(testItemType, "bad")
	require.True(t, found)
}

// Test that reconcile errors abort processing before create and start phases.
func TestReconcileErr(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "existing"})
	env.setDesired(testItemType,
		&fakeConfig{Name: "existing", Decision: object.ActionReconcile, ReconcileErrMode: fakeErrFatal},
		&fakeConfig{Name: "new"},
	)

	err := env.rootManager.reconcile(&stnrv1.StunnerConfig{}, false)
	require.ErrorIs(t, err, errReconcile)

	_, found := env.reg.Lookup(testItemType, "new")
	require.False(t, found)

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{"reconcile:test-item/existing"})
	assertNoPrefix(t, events, "factory:")
	assertNoPrefix(t, events, "start:")
}

// Test that start errors are returned after successful object creation.
func TestStartErr(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()
	env.setDesired(testItemType, &fakeConfig{Name: "new", StartErr: true})

	err := env.rootManager.reconcile(&stnrv1.StunnerConfig{}, false)
	require.ErrorIs(t, err, errStart)

	_, found := env.reg.Lookup(testItemType, "new")
	require.True(t, found)

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{
		"factory:test-item/new",
		"start:test-item/new",
	})
}

// Test that ErrRestartRequired from Reconcile is propagated as an error.
func TestReconcileRestartRequired(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "existing"})
	env.setDesired(testItemType,
		&fakeConfig{Name: "existing", Decision: object.ActionReconcile, ReconcileErrMode: fakeErrRestartRequired},
	)

	err := env.rootManager.reconcile(&stnrv1.StunnerConfig{}, false)
	require.ErrorIs(t, err, object.ErrRestartRequired)

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{"reconcile:test-item/existing"})
	assertNoPrefix(t, events, "close:")
	assertNoPrefix(t, events, "start:")
}

// Test that ErrRestartRequired from Factory.New is propagated as an error.
func TestCreateRestartRequired(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.setDesired(testItemType,
		&fakeConfig{Name: "new", CreateErrMode: fakeErrRestartRequired},
	)

	err := env.rootManager.reconcile(&stnrv1.StunnerConfig{}, false)
	require.ErrorIs(t, err, object.ErrRestartRequired)

	_, found := env.reg.Lookup(testItemType, "new")
	require.False(t, found)

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{"factory:test-item/new"})
	assertNoPrefix(t, events, "start:")
}

// Test that singleton managers reject desired configs with the wrong item name.
func TestSingletonWrongName(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.setDesired(testRootType, &fakeConfig{Name: "renamed-root"})

	err := env.rootManager.reconcile(&stnrv1.StunnerConfig{}, false)
	require.ErrorContains(t, err, "singleton item must be named")

	events := env.rec.snapshot()
	assertNoPrefix(t, events, "reconcile:")
	assertNoPrefix(t, events, "factory:")
	assertNoPrefix(t, events, "close:")
	assertNoPrefix(t, events, "start:")
}

// Test that singleton managers reject desired configs with invalid cardinality.
func TestSingletonCardinality(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.setDesired(testRootType, &fakeConfig{Name: "root"}, &fakeConfig{Name: "root"})

	err := env.rootManager.reconcile(&stnrv1.StunnerConfig{}, false)
	require.ErrorContains(t, err, "singleton extractor returned")

	events := env.rec.snapshot()
	assertNoPrefix(t, events, "reconcile:")
	assertNoPrefix(t, events, "factory:")
	assertNoPrefix(t, events, "close:")
	assertNoPrefix(t, events, "start:")
}

// Test that shutdown recursively closes and removes the entire tree.
func TestShutdownTree(t *testing.T) {
	env := newTestEnv(t)
	env.seedBaseTree()

	env.addExisting(testItemType, &fakeConfig{Name: "a", ShutdownCloseErr: true})
	env.addExisting(testItemType, &fakeConfig{Name: "b"})

	err := env.rootManager.Shutdown()
	require.NoError(t, err)

	require.Empty(t, env.reg.LookupAll(testRootType))
	require.Empty(t, env.reg.LookupAll(testGroupType))
	require.Empty(t, env.reg.LookupAll(testItemType))

	events := env.rec.snapshot()
	assertOrderedSubsequence(t, events, []string{
		"close:test-item/a:true",
		"close:test-item/b:true",
		"close:test-group/group:true",
		"close:test-root/root:true",
	})
}
