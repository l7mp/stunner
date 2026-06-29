// Package reconciler implements the engine that converges the STUNner object tree to a desired
// StunnerConfig. The tree shape is fully described by the object catalog (internal/object); the
// engine walks it recursively, asks each object to Inspect its desired config (constructing
// missing objects on the fly), and applies the resulting operations in globally ordered phases:
// prepare -> stop -> reconcile -> register -> delete -> start
package reconciler

import (
	"errors"
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
	catalog *Catalog
	rt      *runtime.Runtime
	log     logging.LeveledLogger
}

// New creates a Reconciler over a catalog and a runtime.
func New(catalog *Catalog, rt *runtime.Runtime, logger logging.LoggerFactory) *Reconciler {
	return &Reconciler{
		catalog: catalog,
		rt:      rt,
		log:     logger.NewLogger("reconciler"),
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
	// ErrRestarted signals a successful reconcile that bounced some objects: never roll back.
	var restarted stnrv1.ErrRestarted
	if err != nil && !errors.As(err, &restarted) && !p.SuppressRollback {
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
	o, ok := r.rt.Registry.Get(spec.Type, spec.SingletonName(""))
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
	// Phase 1: plan. Walk the catalog and classify every node into operations. Nothing is
	// constructed or mutated here; the walk derives the whole desired tree from configs and
	// names alone.
	ops := newOps()
	rootSpec, _ := r.catalog.Kind(runtime.TypeStunner)
	if err := r.prepareKind(nil, "", "", rootSpec, req, ops); err != nil {
		return err
	}

	// Phase 2: construct newly desired objects (parents-first), but do not touch the registry
	// yet: registration is deferred until after reconcile so a reconcile failure leaves the
	// registry untouched. Construction is side-effect-light (servers start in the start phase),
	// so it runs even on dry-run; only the lifecycle phases (stop/start) are suppressed.
	created, err := r.construct(ops)
	if err != nil {
		return err
	}

	// Phase 3: close objects that are in the stop class (children-first within subtrees).
	if !dryRun {
		for _, o := range ops.stop {
			r.log.Debugf("reconciliation: close %s/%s", o.Type(), o.Name())
			if err := o.Close(false); err != nil {
				r.log.Warnf("close %s/%s: %s", o.Type(), o.Name(), err.Error())
			}
		}
	}

	// Phase 4: reconcile existing objects whose state has changed.
	for _, ref := range ops.reconcile {
		r.log.Tracef("reconciliation: reconcile %s/%s", ref.Object.Type(), ref.Object.Name())
		if err := ref.Object.Reconcile(ref.Config); err != nil {
			return fmt.Errorf("reconcile %s/%s: %w", ref.Object.Type(), ref.Object.Name(), err)
		}
	}

	// Phase 5: register the constructed objects with the registry (parents-first).
	for _, cr := range created {
		r.log.Debugf("reconciliation: add %s/%s", cr.obj.Type(), cr.obj.Name())
		if err := r.rt.Registry.Add(cr.obj, cr.parent); err != nil {
			return fmt.Errorf("add %s/%s to registry: %w", cr.obj.Type(), cr.obj.Name(), err)
		}
	}

	// Phase 6: delete stale objects.
	for _, o := range ops.delete {
		r.log.Debugf("reconciliation: delete %s/%s", o.Type(), o.Name())
		if err := r.rt.Registry.Remove(o); err != nil {
			return fmt.Errorf("remove %s/%s from registry: %w", o.Type(), o.Name(), err)
		}
	}

	// Phase 7: start objects that are in the start class (parents-first within subtrees).
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

// construct builds the planned new objects, parents-first, and returns them for later
// registration. The plan walk appends each createPlan before its children's, so iterating in
// order guarantees a parent is built before its children; a new parent is resolved from the
// objects built earlier in this phase, an existing parent from the registry. Nothing is added to
// the registry here (that happens after reconcile), and construction is side-effect-light:
// servers start in the start phase.
func (r *Reconciler) construct(ops *ops) ([]constructedRef, error) {
	built := make(map[string]runtime.Runnable, len(ops.create))
	out := make([]constructedRef, 0, len(ops.create))

	for _, cp := range ops.create {
		spec, ok := r.catalog.Kind(cp.typ)
		if !ok {
			return nil, fmt.Errorf("catalog missing type %q", cp.typ)
		}

		name := cp.config.ConfigName()

		var parent runtime.Runnable
		if cp.parentType != "" {
			parentKey := string(cp.parentType) + "/" + cp.parentName
			if p, ok := built[parentKey]; ok {
				parent = p
			} else if p, ok := r.rt.Registry.Get(cp.parentType, cp.parentName); ok {
				parent = p
			} else {
				return nil, fmt.Errorf("create %s/%s: parent %s/%s not found",
					cp.typ, name, cp.parentType, cp.parentName)
			}
		}

		r.log.Debugf("reconciliation: create %s/%s", cp.typ, name)
		obj, err := spec.New(parent, cp.config, r.rt)
		if err != nil {
			return nil, fmt.Errorf("create %s/%s: %w", cp.typ, name, err)
		}
		if obj == nil {
			return nil, fmt.Errorf("create %s/%s: constructor returned nil object", cp.typ, name)
		}

		built[string(cp.typ)+"/"+name] = obj
		out = append(out, constructedRef{obj: obj, parent: parent})
		ops.addStart(obj)
	}

	return out, nil
}

// prepareKind diffs the desired instances of one kind against the registry, classifies them into
// operations, and recurses into their child kinds. It is pure planning: it constructs nothing and
// mutates no object. parentType/parentName identify the owning node (both "" at the root); parent
// is the owning object (nil at the root and under not-yet-constructed parents) and is used only to
// scope the stale-instance scan.
func (r *Reconciler) prepareKind(parent runtime.Runnable, parentType runtime.ObjectType, parentName string, spec KindSpec, full *stnrv1.StunnerConfig, ops *ops) error {
	desired, err := spec.ExtractConfigs(parentName, full)
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
		if name := desired[0].ConfigName(); name != spec.SingletonName(parentName) {
			return fmt.Errorf("kind %q singleton item must be named %q, got %q",
				spec.Type, spec.SingletonName(parentName), name)
		}
	}

	seen := make(map[string]bool, len(desired))
	for _, conf := range desired {
		name := conf.ConfigName()
		seen[name] = true

		obj, found := r.rt.Registry.Get(spec.Type, name)
		if !found {
			// Plan the construction; the object (and its children) are built in the
			// construct phase. obj stays nil, so the recursion plans a fresh subtree.
			ops.create = append(ops.create, createPlan{
				typ:        spec.Type,
				config:     conf,
				parentType: parentType,
				parentName: parentName,
			})
		} else if reconcilable, ok := obj.(runtime.Object); ok {
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
				// Make sure all children restart.
				r.collectRestartStops(obj, ops)
				r.collectRestartStarts(obj, ops)
				ops.restartedNames = append(ops.restartedNames,
					fmt.Sprintf("%s: %s", obj.Type(), obj.Name()))
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
			if err := r.prepareKind(obj, spec.Type, name, childSpec, full, ops); err != nil {
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
