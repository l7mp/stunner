package manager

import (
	"fmt"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/object"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// ReconcilePolicy holds root-only reconciliation behavior knobs.
type ReconcilePolicy struct {
	SuppressRollback bool
	DryRun           bool
	Log              logging.LeveledLogger
}

type actionRef struct {
	Manager *Manager
	Object  object.Object
	Config  stnrv1.Config
}

type reconcileOps struct {
	reconcile      []actionRef
	create         []actionRef
	delete         []actionRef
	start          []actionRef
	stop           []actionRef
	restartedNames []string
}

// Reconcile runs one reconcile against the root manager and applies root-only behavior
// (validation, snapshotting, rollback, dry-run).
func Reconcile(root *Manager, req *stnrv1.StunnerConfig, p ReconcilePolicy) error {
	if err := req.Validate(); err != nil {
		return err
	}

	root.log.Infof("reconciliation: commencing (dry-run=%t,rollback=%t,listeners=%d,clusters=%d) for config=%s",
		p.DryRun, !p.SuppressRollback, len(req.Listeners), len(req.Clusters), req.String())

	snapshot := root.Items()[0].GetConfig().(*stnrv1.StunnerConfig)

	err := root.reconcile(req, p.DryRun)
	if err != nil && !p.SuppressRollback {
		p.Log.Infof("reconciliation: rollback initiated")
		_ = root.reconcile(snapshot, p.DryRun)
	}

	return err
}

// reconcile performs one reconcile over this manager subtree.
func (m *Manager) reconcile(req *stnrv1.StunnerConfig, dryRun bool) error {
	reg := m.reg
	ops := &reconcileOps{}

	if err := m.prepare(reg, req, ops); err != nil {
		return err
	}

	// 1. Close objects that are in the stop class.
	if !dryRun {
		for _, ref := range ops.stop {
			m.log.Debugf("reconciliation: close %s/%s", ref.Object.ObjectType(), ref.Object.ObjectName())
			if err := ref.Object.Close(false); err != nil {
				m.log.Warnf("close %s/%s: %s", ref.Object.ObjectType(), ref.Object.ObjectName(), err.Error())
			}
		}
	}

	// 2. Reconcile existing objects whose state has changed.
	for _, ref := range ops.reconcile {
		m.log.Tracef("reconciliation: reconcile %s/%s", ref.Object.ObjectType(), ref.Object.ObjectName())
		if err := ref.Object.Reconcile(ref.Config); err != nil {
			return fmt.Errorf("reconcile %s/%s: %w", ref.Object.ObjectType(), ref.Object.ObjectName(), err)
		}
	}

	// 3. Create new objects as necessary.
	for i := range ops.create {
		cr := &ops.create[i]
		m.log.Debugf("reconciliation: create %s/%s", cr.Manager.objectType, cr.Config.ConfigName())
		obj, err := cr.Manager.ctor(cr.Config, cr.Manager.reg)
		if err != nil {
			return fmt.Errorf("manager %q: create %q: %w", cr.Manager.name, cr.Config.ConfigName(), err)
		}
		if obj == nil {
			return fmt.Errorf("manager %q: create %q: constructor returned nil object", cr.Manager.name,
				cr.Config.ConfigName())
		}
		cr.Object = obj
		ops.start = append(ops.start, actionRef{Object: obj})
		if err := cr.Manager.reg.Add(obj); err != nil {
			return fmt.Errorf("manager %q: add %q to registry: %w", cr.Manager.name, cr.Config.ConfigName(), err)
		}
		m.log.Debugf("reconciliation: create %s/%s: done", obj.ObjectType(), obj.ObjectName())
	}

	// 4. Delete stale objects.
	for _, ref := range ops.delete {
		m.log.Debugf("reconciliation: delete %s/%s", ref.Object.ObjectType(), ref.Object.ObjectName())
		if err := m.reg.Remove(ref.Object); err != nil {
			return fmt.Errorf("remove %s/%s from registry: %w", ref.Object.ObjectType(), ref.Object.ObjectName(), err)
		}
	}

	// 5. Start objects that are in the start class.
	if !dryRun {
		for _, ref := range ops.start {
			m.log.Debugf("reconciliation: start %s/%s", ref.Object.ObjectType(), ref.Object.ObjectName())
			if err := ref.Object.Start(); err != nil {
				return fmt.Errorf("start %s/%s: %w", ref.Object.ObjectType(), ref.Object.ObjectName(), err)
			}
		}
	}

	m.log.Debugf("reconciliation: done (dry-run=%t) stats: close=%d reconcile=%d create=%d delete=%d start=%d restarted=%d",
		dryRun, len(ops.stop), len(ops.reconcile), len(ops.create), len(ops.delete), len(ops.start),
		len(ops.restartedNames))

	return restartedErrorFromOps(ops)
}

// Shutdown closes every object in this manager subtree with shutdown=true and removes it from the
// registry.
func (m *Manager) Shutdown() error {
	reg := m.reg
	for _, item := range m.Items() {
		shutdownObject(reg, item, m.reg, m.log)
	}
	return nil
}

func (m *Manager) prepare(reg Registry, full *stnrv1.StunnerConfig, ops *reconcileOps) error {
	desired, err := m.extractor(full)
	if err != nil {
		return fmt.Errorf("manager %q list extractor: %w", m.name, err)
	}

	if m.singleton {
		if len(desired) != 1 {
			return fmt.Errorf("manager %q singleton extractor returned %d items", m.name, len(desired))
		}
	}

	seen := make(map[string]bool, len(desired))
	for _, conf := range desired {
		name := conf.ConfigName()
		if m.singleton {
			if name != m.itemName {
				return fmt.Errorf("manager %q singleton item must be named %q, got %q", m.name, m.itemName, name)
			}
			name = m.itemName
		}
		seen[name] = true

		obj, found := m.reg.Lookup(m.objectType, name)
		if !found {
			ops.create = append(ops.create, actionRef{Manager: m, Config: conf})
			continue
		}

		decision, err := obj.Inspect(obj.GetConfig(), conf, full)
		if err != nil {
			return fmt.Errorf("inspect %s/%s: %w", obj.ObjectType(), obj.ObjectName(), err)
		}

		switch decision {
		case object.ActionNone:
		case object.ActionReconcile:
			ops.reconcile = append(ops.reconcile, actionRef{Object: obj, Config: conf})
		case object.ActionRestart:
			ops.reconcile = append(ops.reconcile, actionRef{Object: obj, Config: conf})
			ops.stop = append(ops.stop, actionRef{Object: obj})
			ops.start = append(ops.start, actionRef{Object: obj})
			if shouldReportRestart(obj.ObjectType()) {
				ops.restartedNames = append(ops.restartedNames,
					fmt.Sprintf("%s: %s", obj.ObjectType(), obj.ObjectName()))
			}
		default:
			panic(fmt.Sprintf("object %s/%s returned invalid inspect action: %d",
				obj.ObjectType(), obj.ObjectName(), decision))
		}

		node := reg.NodeOf(obj)
		for _, sm := range node.SubManagers {
			if err := sm.prepare(reg, full, ops); err != nil {
				return err
			}
		}
	}

	for _, obj := range m.reg.LookupAll(m.objectType) {
		if seen[obj.ObjectName()] {
			continue
		}
		collectDeleteSubtree(reg, obj, ops)
	}

	return nil
}

func shouldReportRestart(objType string) bool {
	switch objType {
	case object.TypeHealth, object.TypeMetrics:
		return false
	default:
		return true
	}
}

func collectDeleteSubtree(reg Registry, obj object.Object, ops *reconcileOps) {
	node := reg.NodeOf(obj)
	for _, sm := range node.SubManagers {
		for _, child := range sm.Items() {
			collectDeleteSubtree(reg, child, ops)
		}
	}
	ops.delete = append(ops.delete, actionRef{Object: obj})
	ops.stop = append(ops.stop, actionRef{Object: obj})
}

func shutdownObject(reg Registry, obj object.Object, registryIface object.Registry, log logging.LeveledLogger) {
	node := reg.NodeOf(obj)
	for _, sm := range node.SubManagers {
		for _, item := range sm.Items() {
			shutdownObject(reg, item, registryIface, log)
		}
	}

	if err := obj.Close(true); err != nil {
		log.Warnf("close error on %s/%s during shutdown: %s", obj.ObjectType(), obj.ObjectName(), err.Error())
	}
	_ = registryIface.Remove(obj)
}

func restartedErrorFromOps(ops *reconcileOps) error {
	if len(ops.restartedNames) == 0 {
		return nil
	}
	return stnrv1.ErrRestarted{Objects: append([]string(nil), ops.restartedNames...)}
}
