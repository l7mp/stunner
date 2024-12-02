package manager

import (
	"fmt"

	"github.com/l7mp/stunner/internal/object"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

type ReconcileJob struct {
	Object               object.Object
	NewConfig, OldConfig stnrv1.Config
}

type ReconciliationState struct {
	NewJobQueue, ChangedJobQueue, DeletedJobQueue []ReconcileJob
	ToBeStarted, ToBeRestarted                    []object.Object
}

// PrepareReconciliation prepares the reconciliation of the objects handled by the manager and returns a
// set of reconciliation jobs to be performed, ErrRestartRequired if the server needs to be
// restarted, and an error if the config was not accepted. Configuration must be validated.
func (m *managerImpl) PrepareReconciliation(confs []stnrv1.Config, stunnerConf stnrv1.Config) (*ReconciliationState, error) {
	m.log.Tracef("preparing reconciliation")

	state := ReconciliationState{
		NewJobQueue:     []ReconcileJob{},
		ChangedJobQueue: []ReconcileJob{},
		DeletedJobQueue: []ReconcileJob{},
		ToBeStarted:     []object.Object{},
		ToBeRestarted:   []object.Object{},
	}

	// find what has to be added or changed
	for _, c := range confs {
		m.log.Debugf("reconciling for conf %q: %s", c.ConfigName(), c.String())

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

			m.log.Tracef("inspecting object %q for configuration change: %s -> %s",
				o.ObjectName(), runningConf.String(), c.String())

			changed, err := o.Inspect(runningConf, c, stunnerConf)
			if err != nil && err != object.ErrRestartRequired {
				return nil, err
			}

			if changed {
				m.log.Tracef("object %q changes, adding to job queue", o.ObjectName())
				state.ChangedJobQueue = append(state.ChangedJobQueue,
					ReconcileJob{Object: o, NewConfig: c, OldConfig: runningConf})

				if err == object.ErrRestartRequired {
					m.log.Tracef("object %q asks for a restart", o.ObjectName())
					state.ToBeRestarted = append(state.ToBeRestarted, o)
				}

			} else {
				m.log.Tracef("object %q unchanged", o.ObjectName())
			}

		} else {
			m.log.Tracef("new object %q: adding to job queue", c.ConfigName())
			state.NewJobQueue = append(state.NewJobQueue,
				ReconcileJob{Object: nil, NewConfig: c, OldConfig: nil})
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

	return &state, nil
}

// FinishReconciliation finishes the reconciliation from the specified state.
func (m *managerImpl) FinishReconciliation(state *ReconciliationState) error {
	m.log.Tracef("finishing reconciliation")

	m.log.Trace("running the new-object job queue")
	for _, j := range state.NewJobQueue {
		o, err := m.factory.New(j.NewConfig)
		if err != nil {
			if err != object.ErrRestartRequired {
				m.log.Errorf("could not create new object: %s", err.Error())
				return err
			}
			state.ToBeStarted = append(state.ToBeStarted, o)
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
		// reconciled objects are already inspected for a restart: ignore restart requests
		if err != nil && err != object.ErrRestartRequired {
			return err
		}
	}

	m.log.Debugf("reconciliation ready: to-be-created: %d, changed: %d, deleted: %d",
		len(state.NewJobQueue), len(state.ChangedJobQueue), len(state.DeletedJobQueue))

	return nil
}

func findConfByName(confs []stnrv1.Config, name string) bool {
	for _, c := range confs {
		if c.ConfigName() == name {
			return true
		}
	}

	return false
}
