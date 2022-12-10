package manager

import (
	"fmt"
	// "sort"

	// "github.com/pion/logging"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

type ReconcileJob struct {
	Object               object.Object
	NewConfig, OldConfig v1alpha1.Config
}

type ReconciliationState struct {
	NewJobQueue, ChangedJobQueue, DeletedJobQueue []ReconcileJob
	Restarted                                     []object.Object
}

// PrepareReconciliation prepares the reconciliation of the objects handled by the manager and returns a
// set of reconciliation jobs to be performed, ErrRestartRequired if the server needs to be
// restarted, and an error if the config was not accepted
func (m *managerImpl) PrepareReconciliation(confs []v1alpha1.Config) (*ReconciliationState, error) {
	m.log.Tracef("preparing reconciliation")

	state := ReconciliationState{
		NewJobQueue:     []ReconcileJob{},
		ChangedJobQueue: []ReconcileJob{},
		DeletedJobQueue: []ReconcileJob{},
		Restarted:       []object.Object{},
	}

	var restart error = nil

	// find what has to be added or changed
	for _, c := range confs {
		m.log.Debugf("reconciling for conf %q: %s", c.ConfigName(), c.String())

		// make sure lists are sorted and names are OK
		if err := c.Validate(); err != nil {
			return nil, err
		}

		o, found := m.Get(c.ConfigName())
		if found {
			// got new config for the object: object may need to be updated
			runningConf := o.GetConfig()

			// make sure lists are sorted and names are OK
			if err := runningConf.Validate(); err != nil {
				return nil, fmt.Errorf("internal error: cannot validate running "+
					"config (%s) for object %s: %w", runningConf.String(),
					o.ObjectName(), err)
			}

			if runningConf.DeepEqual(c) {
				m.log.Tracef("object %q unchanged", o.ObjectName())
			} else {
				m.log.Tracef("object %q changes, adding to job queue", o.ObjectName())
				state.ChangedJobQueue = append(state.ChangedJobQueue,
					ReconcileJob{Object: o, NewConfig: c, OldConfig: runningConf})
			}

		} else {
			m.log.Tracef("new object %q: adding to job queue", c.ConfigName())
			// create a mock object so that we can call Inspect later on
			o, _ = m.factory.New(nil)
			state.NewJobQueue = append(state.NewJobQueue,
				ReconcileJob{Object: o, NewConfig: c, OldConfig: nil})
		}
	}

	// find what has to be deleted
	for _, o := range m.objects {
		if !findConfByName(confs, o.ObjectName()) {
			m.log.Tracef("deleted object %q: adding to deleted queue", o.ObjectName())
			state.DeletedJobQueue = append(state.DeletedJobQueue,
				ReconcileJob{Object: o, NewConfig: nil, OldConfig: o.GetConfig()})
		}
	}

	m.log.Trace("inspecting the reconciliation job queue")
	for _, j := range state.ChangedJobQueue {
		m.log.Tracef("inspecting object %q for configuration change: %s -> %s",
			j.Object.ObjectName(), j.OldConfig.String(), j.NewConfig.String())

		if j.Object.Inspect(j.OldConfig, j.NewConfig) {
			state.Restarted = append(state.Restarted, j.Object)
			restart = object.ErrRestartRequired
		}
	}

	return &state, restart
}

// FinishReconciliation finishes the reconciliation from the specified state
func (m *managerImpl) FinishReconciliation(state *ReconciliationState) error {
	m.log.Tracef("finishing reconciliation")

	m.log.Trace("running the new-object job queue")
	for _, j := range state.NewJobQueue {
		o, err := m.factory.New(j.NewConfig)
		if err != nil && err != object.ErrRestartRequired {
			m.log.Errorf("could not create new object: %s", err.Error())
			return err
		}
		// ignore errors
		_ = m.Upsert(o)
	}

	m.log.Trace("running the deletion job queue")
	for _, j := range state.DeletedJobQueue {
		o := j.Object
		m.log.Tracef("deleting object %q: running conf: %s", o.ObjectName(),
			j.OldConfig.String())
		// ignore error
		_ = m.Delete(o)
	}

	m.log.Trace("running the reconciliation job queue")
	for _, j := range state.ChangedJobQueue {
		o := j.Object
		m.log.Tracef("reconciling object %q: %s -> %s", o.ObjectName(),
			j.OldConfig.String(), j.NewConfig.String())

		err := o.Reconcile(j.NewConfig)
		if err != nil && err != object.ErrRestartRequired {
			return err
		}
	}

	m.log.Debugf("reconciliation ready: to-be-created: %d, changed: %d, deleted: %d",
		len(state.NewJobQueue), len(state.ChangedJobQueue), len(state.DeletedJobQueue))

	return nil
}

func findConfByName(confs []v1alpha1.Config, name string) bool {
	for _, c := range confs {
		if c.ConfigName() == name {
			return true
		}
	}

	return false
}
