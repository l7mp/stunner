package manager

import (
	// "fmt"
	"sort"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

type job struct {
        object object.Object
        config, oldConfig v1alpha1.Config
}

// Manager stores STUNner objects
type Manager interface {
	// Upsert upserts the object into the store
	Upsert(o object.Object) error
        // Get returns a named object
        Get(name string) (object.Object, bool)
	// Delete deletes the object from the store, may return ErrReturnRequired
	Delete(o object.Object) error
        // Reconcile reconcliles the the object store
        Reconcile(confs []v1alpha1.Config) ([]v1alpha1.Config, error)
        // Keys returns the names iof all objects in the store in alphabetical order, suitable for iteration
        Keys() []string
        
}

// NewManager creates a new Manager.
func NewManager(name string, logger logging.LoggerFactory) Manager {
        return &managerImpl{
                name: name,
		objects: make(map[string]object.Object),
                log: logger.NewLogger(name),
        }
}

type managerImpl struct {
        name string
	objects map[string]object.Object
        log logging.LeveledLogger
}

// config must be validated before callling u!
func (m *managerImpl) Upsert(o object.Object) error {
        m.log.Tracef("upsert object %s", o.ObjectName())

	m.objects[o.ObjectName()] = o
        return nil
}

func (m *managerImpl) Get(name string) (object.Object, bool) {
        m.log.Tracef("get object %s", name)
        o, found := m.objects[name]

        if found {
                return o, found
        } 
        
	return nil, false
}

// Delete removes an object, may return ErrRestartRequired
func (m *managerImpl) Delete(o object.Object) error {
        m.log.Tracef("delete object %s", o.ObjectName())

	delete(m.objects, o.ObjectName())
        return o.Close()
}

// safe for addition/deletion
func (m *managerImpl) Keys() []string {
        m.log.Tracef("object keys")

        names := make([]string, len(m.objects))
        i := 0
        for k, _ := range m.objects {
                names[i] = k
                i += 1
        }

        sort.Strings(names)
        return names
}

// Reconcile updates all objects handled by the manager and returns the config for the new objects to be created. Input config must be validated! Returns ErrRestartRequired if the server needs to be restarted
func (m *managerImpl) Reconcile(confs []v1alpha1.Config) ([]v1alpha1.Config, error) {
        m.log.Debugf("reconciling manager for configs: %#v", confs)

        restart         := false
        newJobQueue     := []v1alpha1.Config{}
        changedJobQueue := []job{}
        deleted         := 0
        
        // find what has to be added or changed
        for _, c := range confs {
                m.log.Tracef("reconciling for conf %s: %#v", c.ConfigName(), c)

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
                                m.log.Debugf("object %s unchanged", o.ObjectName())
                        } else {
                                m.log.Debugf("object %s changes, adding to job queue", o.ObjectName())
                                changedJobQueue = append(changedJobQueue,
                                        job{object: o, config: c, oldConfig: runningConf})
                        }
                } else {
                        m.log.Debugf("new object %s: adding to job queue", c.ConfigName())
                        newJobQueue = append(newJobQueue, c)
                }
        }

        // find what has to be deleted and delete it
        for _, o := range m.objects {
                if !findConfByName(confs, o.ObjectName()) {
                        m.log.Debugf("deleting object %s", o.ObjectName())
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
                m.log.Debugf("reconciling object %s: %#v -> %#v", j.object.ObjectName(), j.oldConfig, j.config)
                err := j.object.Reconcile(j.config)
                if err != nil {
                        if err == v1alpha1.ErrRestartRequired {
                                restart = true
                        } else {
                                return []v1alpha1.Config{}, err
                        }
                }
        }

        m.log.Debugf("reconciling ready: new objects: %d, changed objects: %d, deleted objects: %d, " +
                "restart required for changed objects: %t", len(newJobQueue), len(changedJobQueue),
                deleted, restart)
        
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
