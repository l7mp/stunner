// Package reconciler implements the engine that converges the STUNner object tree to a desired
// StunnerConfig. The tree shape is fully described by the object catalog (internal/object); the
// engine walks it recursively, asks each object to Inspect its desired config, and applies the
// resulting operations in globally ordered phases: prepare -> stop -> reconcile -> create ->
// delete -> start
package reconciler

import (
	"fmt"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Policy holds root-only reconciliation behavior knobs.
type Policy struct {
	SuppressRollback bool
	DryRun           bool
}

// Reconciler converges the object tree to a desired config, driven by the catalog.
type Reconciler struct {
	catalog *runtime.Catalog
	rt      *runtime.Runtime
	log     logging.LeveledLogger
}

// New creates a Reconciler over a catalog and a runtime.
func New(catalog *runtime.Catalog, rt *runtime.Runtime, logger logging.LoggerFactory) *Reconciler {
	return &Reconciler{
		catalog: catalog,
		rt:      rt,
		log:     logger.NewLogger("reconciler"),
	}
}

type createRef struct {
	Parent runtime.Runnable
	Spec   runtime.KindSpec
	Config stnrv1.Config
}

type reconcileRef struct {
	Object runtime.Object
	Config stnrv1.Config
}

type ops struct {
	reconcile      []reconcileRef
	create         []createRef
	delete         []runtime.Runnable
	start          []runtime.Runnable
	stop           []runtime.Runnable
	restartedNames []string
	startSet       map[string]bool
	stopSet        map[string]bool
	deleteSet      map[string]bool
}

func newOps() *ops {
	return &ops{
		startSet:  map[string]bool{},
		stopSet:   map[string]bool{},
		deleteSet: map[string]bool{},
	}
}

// Reconcile runs one reconcile against the tree and applies root-only behavior (validation,
// snapshotting, rollback, dry-run).
func (r *Reconciler) Reconcile(req *stnrv1.StunnerConfig, p Policy) error {
	if err := req.Validate(); err != nil {
		return err
	}

	r.log.Infof("reconciliation: commencing (dry-run=%t,rollback=%t,listeners=%d,clusters=%d) for config=%s",
		p.DryRun, !p.SuppressRollback, len(req.Listeners), len(req.Clusters), req.String())

	root, err := r.root()
	if err != nil {
		return err
	}
	snapshot := root.GetConfig().(*stnrv1.StunnerConfig)

	err = r.run(req, p.DryRun)
	if err != nil && !p.SuppressRollback {
		r.log.Infof("reconciliation: rollback initiated")
		_ = r.run(snapshot, p.DryRun)
	}

	return err
}

// root returns the root object of the tree.
func (r *Reconciler) root() (runtime.Object, error) {
	spec, ok := r.catalog.Kind(runtime.TypeStunner)
	if !ok {
		return nil, fmt.Errorf("catalog missing root type %q", runtime.TypeStunner)
	}
	if !spec.Singleton || spec.SingletonName == nil {
		return nil, fmt.Errorf("root kind %q must be a named singleton", spec.Type)
	}
	o, ok := r.rt.Registry.Get(spec.Type, spec.SingletonName(nil))
	if !ok {
		return nil, fmt.Errorf("root object not registered")
	}
	rootObj, ok := o.(runtime.Object)
	if !ok {
		return nil, fmt.Errorf("root object %s/%s is not reconcilable", o.Type(), o.Name())
	}
	return rootObj, nil
}

// run performs one reconcile over the whole tree.
func (r *Reconciler) run(req *stnrv1.StunnerConfig, dryRun bool) error {
	// Phase 1: identify the objects that require reconciliation/restarting.
	ops := newOps()
	rootSpec, _ := r.catalog.Kind(runtime.TypeStunner)
	if err := r.prepareKind(nil, rootSpec, req, ops); err != nil {
		return err
	}

	// Phase 2: close objects that are in the stop class (children-first within subtrees).
	if !dryRun {
		for _, o := range ops.stop {
			r.log.Debugf("reconciliation: close %s/%s", o.Type(), o.Name())
			if err := o.Close(false); err != nil {
				r.log.Warnf("close %s/%s: %s", o.Type(), o.Name(), err.Error())
			}
		}
	}

	// Phase 3: reconcile existing objects whose state has changed.
	for _, ref := range ops.reconcile {
		r.log.Tracef("reconciliation: reconcile %s/%s", ref.Object.Type(), ref.Object.Name())
		if err := ref.Object.Reconcile(ref.Config); err != nil {
			return fmt.Errorf("reconcile %s/%s: %w", ref.Object.Type(), ref.Object.Name(), err)
		}
	}

	// Phase 4: create new objects. The list grows while we iterate: freshly created nodes
	// prepare their own child kinds, appending further creates.
	for i := 0; i < len(ops.create); i++ {
		cr := &ops.create[i]
		r.log.Debugf("reconciliation: create %s/%s", cr.Spec.Type, cr.Config.ConfigName())
		obj, err := cr.Spec.New(cr.Parent, cr.Config, r.rt)
		if err != nil {
			return fmt.Errorf("create %s/%s: %w", cr.Spec.Type, cr.Config.ConfigName(), err)
		}
		if obj == nil {
			return fmt.Errorf("create %s/%s: constructor returned nil object",
				cr.Spec.Type, cr.Config.ConfigName())
		}
		ops.addStart(obj)
		if err := r.rt.Registry.Add(obj, cr.Parent); err != nil {
			return fmt.Errorf("add %s/%s to registry: %w", obj.Type(), obj.Name(), err)
		}
		for _, childType := range cr.Spec.Children {
			childSpec, ok := r.catalog.Kind(childType)
			if !ok {
				return fmt.Errorf("catalog missing type %q", childType)
			}
			if err := r.prepareKind(obj, childSpec, req, ops); err != nil {
				return err
			}
		}
		r.log.Debugf("reconciliation: create %s/%s: done", obj.Type(), obj.Name())
	}

	// Phase 5: delete stale objects.
	for _, o := range ops.delete {
		r.log.Debugf("reconciliation: delete %s/%s", o.Type(), o.Name())
		if err := r.rt.Registry.Remove(o); err != nil {
			return fmt.Errorf("remove %s/%s from registry: %w", o.Type(), o.Name(), err)
		}
	}

	// Phase 6: start objects that are in the start class (parents-first within subtrees).
	if !dryRun {
		for _, o := range ops.start {
			r.log.Debugf("reconciliation: start %s/%s", o.Type(), o.Name())
			if err := o.Start(); err != nil {
				return fmt.Errorf("start %s/%s: %w", o.Type(), o.Name(), err)
			}
		}
	}

	r.log.Debugf("reconciliation: done (dry-run=%t) stats: close=%d reconcile=%d create=%d delete=%d start=%d restarted=%d",
		dryRun, len(ops.stop), len(ops.reconcile), len(ops.create), len(ops.delete), len(ops.start),
		len(ops.restartedNames))

	return restartedErrorFromOps(ops)
}

// prepareKind diffs the desired instances of one kind under `parent` against the registry,
// classifies them into operations, and recurses into surviving instances' child kinds.
func (r *Reconciler) prepareKind(parent runtime.Runnable, spec runtime.KindSpec,
	full *stnrv1.StunnerConfig, ops *ops) error {

	if spec.DesiredConfigs == nil {
		return fmt.Errorf("kind %q has no desired-config resolver", spec.Type)
	}
	desired, err := spec.DesiredConfigs(parent, full)
	if err != nil {
		return fmt.Errorf("kind %q desired-config resolver: %w", spec.Type, err)
	}

	if spec.Singleton {
		if len(desired) != 1 {
			return fmt.Errorf("kind %q singleton resolver returned %d items", spec.Type, len(desired))
		}
		if spec.SingletonName == nil {
			return fmt.Errorf("kind %q is singleton but has no singleton-name resolver", spec.Type)
		}
		if name := desired[0].ConfigName(); name != spec.SingletonName(parent) {
			return fmt.Errorf("kind %q singleton item must be named %q, got %q",
				spec.Type, spec.SingletonName(parent), name)
		}
	}

	seen := make(map[string]bool, len(desired))
	for _, conf := range desired {
		name := conf.ConfigName()
		seen[name] = true

		obj, found := r.rt.Registry.Get(spec.Type, name)
		if !found {
			ops.create = append(ops.create, createRef{Parent: parent, Spec: spec, Config: conf})
			continue
		}

		if reconcilable, ok := obj.(runtime.Object); ok {
			// Inspect is the critical per-object decision: untouched vs in-place
			// reconcile vs restart. The object implementations carry years of
			// accumulated reconciliation fixes; the engine only schedules.
			decision, err := reconcilable.Inspect(reconcilable.GetConfig(), conf, full)
			if err != nil {
				return fmt.Errorf("inspect %s/%s: %w", obj.Type(), obj.Name(), err)
			}

			switch decision {
			case runtime.ActionNone:
			case runtime.ActionReconcile:
				ops.reconcile = append(ops.reconcile, reconcileRef{Object: reconcilable, Config: conf})
			case runtime.ActionRestart:
				ops.reconcile = append(ops.reconcile, reconcileRef{Object: reconcilable, Config: conf})
				r.collectRestartStops(obj, ops)
				r.collectRestartStarts(obj, ops)
				if shouldReportRestart(obj.Type()) {
					ops.restartedNames = append(ops.restartedNames,
						fmt.Sprintf("%s: %s", obj.Type(), obj.Name()))
				}
			default:
				return fmt.Errorf("object %s/%s returned invalid inspect action: %d",
					obj.Type(), obj.Name(), decision)
			}
		}

		for _, childType := range spec.Children {
			childSpec, ok := r.catalog.Kind(childType)
			if !ok {
				return fmt.Errorf("catalog missing type %q", childType)
			}
			if err := r.prepareKind(obj, childSpec, full, ops); err != nil {
				return err
			}
		}
	}

	// Stale instances: children of this kind under the parent that are no longer desired.
	for _, obj := range r.rt.Registry.ChildrenOf(parent, spec.Type) {
		if seen[obj.Name()] {
			continue
		}
		r.collectDeleteSubtree(obj, ops)
	}

	return nil
}

// Shutdown closes every object in the tree with shutdown=true (children-first) and removes it
// from the registry.
func (r *Reconciler) Shutdown() error {
	root, err := r.root()
	if err != nil {
		return err
	}
	r.shutdownNode(root)
	return nil
}

func (r *Reconciler) shutdownNode(obj runtime.Runnable) {
	for _, childType := range r.childTypes(obj) {
		for _, child := range r.rt.Registry.ChildrenOf(obj, childType) {
			r.shutdownNode(child)
		}
	}
	if err := obj.Close(true); err != nil {
		r.log.Warnf("close error on %s/%s during shutdown: %s", obj.Type(), obj.Name(), err.Error())
	}
	_ = r.rt.Registry.Remove(obj)
}

// childTypes returns the child kinds of a node per the catalog.
func (r *Reconciler) childTypes(obj runtime.Runnable) []runtime.ObjectType {
	spec, ok := r.catalog.Kind(obj.Type())
	if !ok {
		return nil
	}
	return spec.Children
}

func shouldReportRestart(objType runtime.ObjectType) bool {
	switch objType {
	case runtime.TypeHealth, runtime.TypeMetrics:
		return false
	default:
		return true
	}
}

// collectRestartStops records the whole subtree of a restarting object in the stop class,
// children-first.
func (r *Reconciler) collectRestartStops(obj runtime.Runnable, ops *ops) {
	for _, childType := range r.childTypes(obj) {
		for _, child := range r.rt.Registry.ChildrenOf(obj, childType) {
			r.collectRestartStops(child, ops)
		}
	}
	ops.addStop(obj)
}

// collectRestartStarts records the whole subtree of a restarting object in the start class,
// parents-first.
func (r *Reconciler) collectRestartStarts(obj runtime.Runnable, ops *ops) {
	ops.addStart(obj)
	for _, childType := range r.childTypes(obj) {
		for _, child := range r.rt.Registry.ChildrenOf(obj, childType) {
			r.collectRestartStarts(child, ops)
		}
	}
}

// collectDeleteSubtree records an object and all its descendants for stop+delete,
// children-first.
func (r *Reconciler) collectDeleteSubtree(obj runtime.Runnable, ops *ops) {
	for _, childType := range r.childTypes(obj) {
		for _, child := range r.rt.Registry.ChildrenOf(obj, childType) {
			r.collectDeleteSubtree(child, ops)
		}
	}
	ops.addDelete(obj)
	ops.addStop(obj)
}

func restartedErrorFromOps(ops *ops) error {
	if len(ops.restartedNames) == 0 {
		return nil
	}
	return stnrv1.ErrRestarted{Objects: append([]string(nil), ops.restartedNames...)}
}

func opKey(o runtime.Runnable) string { return string(o.Type()) + "/" + o.Name() }

func (ops *ops) addStop(o runtime.Runnable) {
	k := opKey(o)
	if ops.stopSet[k] {
		return
	}
	ops.stopSet[k] = true
	ops.stop = append(ops.stop, o)
}

func (ops *ops) addStart(o runtime.Runnable) {
	k := opKey(o)
	if ops.startSet[k] {
		return
	}
	ops.startSet[k] = true
	ops.start = append(ops.start, o)
}

func (ops *ops) addDelete(o runtime.Runnable) {
	k := opKey(o)
	if ops.deleteSet[k] {
		return
	}
	ops.deleteSet[k] = true
	ops.delete = append(ops.delete, o)
}
