package manager

import (
	// "fmt"
	// "sort"

	// "github.com/pion/logging"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

type job struct {
	object            object.Object
	config, oldConfig v1alpha1.Config
}

// Reconcile updates all objects handled by the manager and returns the config for the new objects to be created. Input config must be validated! Returns ErrRestartRequired if the server needs to be restarted
func (m *managerImpl) Reconcile(confs []v1alpha1.Config) ([]v1alpha1.Config, error) {
	m.log.Tracef("reconciling manager")

	restart := false
	newJobQueue := []v1alpha1.Config{}
	changedJobQueue := []job{}
	deleted := 0

	// find what has to be added or changed
	for _, c := range confs {
		m.log.Tracef("reconciling for conf %q: %#v", c.ConfigName(), c)

		o, found := m.Get(c.ConfigName())
		if found {
			// configs have it, object exists: object may need to be updated
			runningConf := o.GetConfig()

			// make sure lists are sorted and names are OK
			if err := runningConf.Validate(); err != nil {
				m.log.Errorf("cannot validate running configuration for object %s: %#v",
					o.ObjectName(), runningConf)
			}
			if runningConf.DeepEqual(c) {
				m.log.Tracef("object %q unchanged", o.ObjectName())
			} else {
				m.log.Tracef("object %q changes, adding to job queue", o.ObjectName())
				changedJobQueue = append(changedJobQueue,
					job{object: o, config: c, oldConfig: runningConf})
			}
		} else {
			m.log.Tracef("new object %q: adding to job queue", c.ConfigName())
			newJobQueue = append(newJobQueue, c)
		}
	}

	// find what has to be deleted and delete it
	for _, o := range m.objects {
		if !findConfByName(confs, o.ObjectName()) {
			m.log.Tracef("deleting object %q", o.ObjectName())
			err := m.Delete(o)
			if err == v1alpha1.ErrRestartRequired {
				restart = true
			}
			deleted += 1
		}
	}

	// do the reconciliation jobs
	m.log.Trace("running the reconciliation job queue")
	for _, j := range changedJobQueue {
		m.log.Tracef("reconciling object %q: %#v -> %#v", j.object.ObjectName(), j.oldConfig, j.config)
		err := j.object.Reconcile(j.config)
		if err != nil {
			if err == v1alpha1.ErrRestartRequired {
				restart = true
			} else {
				return []v1alpha1.Config{}, err
			}
		}
	}

	m.log.Debugf("reconciliation ready: new objects: %d, changed objects: %d, deleted objects: %d",
		len(newJobQueue), len(changedJobQueue), deleted)

	if restart {
		return newJobQueue, v1alpha1.ErrRestartRequired
	}

	return newJobQueue, nil
}

func findConfByName(confs []v1alpha1.Config, name string) bool {
	for _, c := range confs {
		if c.ConfigName() == name {
			return true
		}
	}

	return false
}
