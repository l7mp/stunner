package manager

import (
	// "fmt"
	"sort"
	"sync"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/object"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Manager stores STUNner objects
type Manager interface {
	// Upsert upserts the object into the store
	Upsert(o object.Object) error
	// Get returns a named object
	Get(name string) (object.Object, bool)
	// Delete deletes the object from the store, may return ErrReturnRequired
	Delete(o object.Object) error
	// PrepareReconciliation prepares the reconciliation of the manager
	PrepareReconciliation(confs []stnrv1.Config, stunenerConf stnrv1.Config) (*ReconciliationState, error)
	// FinishReconciliation finishes the reconciliation from the specified state
	FinishReconciliation(state *ReconciliationState) error
	// Keys returns the names iof all objects in the store in alphabetical order, suitable for iteration
	Keys() []string
}

// locks avoid races during reconcile: auth/perm handlers may call into the manager from separate threads
type managerImpl struct {
	name    string
	lock    sync.RWMutex
	objects map[string]object.Object
	factory object.Factory
	log     logging.LeveledLogger
}

// NewManager creates a new Manager.
func NewManager(name string, f object.Factory, logger logging.LoggerFactory) Manager {
	return &managerImpl{
		name:    name,
		objects: make(map[string]object.Object),
		factory: f,
		log:     logger.NewLogger(name),
	}
}

// config must be validated before callling u!
func (m *managerImpl) Upsert(o object.Object) error {
	m.log.Tracef("upsert object %q", o.ObjectName())

	m.lock.Lock()
	defer m.lock.Unlock()

	m.objects[o.ObjectName()] = o

	return nil
}

func (m *managerImpl) Get(name string) (object.Object, bool) {
	// m.log.Tracef("get object %s", name)

	m.lock.RLock()
	o, found := m.objects[name]
	m.lock.RUnlock()

	if found {
		return o, found
	}

	return nil, false
}

// Delete removes an object, may return ErrRestartRequired
func (m *managerImpl) Delete(o object.Object) error {
	m.log.Tracef("delete object %q", o.ObjectName())

	m.lock.Lock()
	defer m.lock.Unlock()

	delete(m.objects, o.ObjectName())

	return o.Close()
}

// safe for addition/deletion
func (m *managerImpl) Keys() []string {
	// m.log.Tracef("object keys")

	names := make([]string, len(m.objects))
	i := 0

	m.lock.RLock()
	for k := range m.objects {
		names[i] = k
		i += 1
	}
	m.lock.RUnlock()

	sort.Strings(names)
	return names
}
