package manager

import (
	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/object"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// ListExtractor pulls the slice of typed Configs handled by this Manager out of the full
// StunnerConfig. Singleton Managers return a single-element slice; multi-instance Managers (e.g.,
// the per-Listener Manager under ListenerList) return one entry per item.
type ListExtractor = func(*stnrv1.StunnerConfig) ([]stnrv1.Config, error)

// Constructor creates a new object for a manager from config and registry.
type Constructor = func(conf stnrv1.Config, reg object.Registry) (object.Object, error)

// Manager owns the items of one ObjectType. Items live in the Registry, not in the Manager — the
// Manager just knows how to find them by type and how to drive the per-item reconcile lifecycle.
type Manager struct {
	name       string
	objectType string
	ctor       Constructor
	extractor  ListExtractor
	reg        Registry
	log        logging.LeveledLogger
	singleton  bool
	itemName   string
}

// Option customizes a manager at construction time.
type Option func(*Manager)

// WithSingleton marks a manager as singleton and fixes its item name.
func WithSingleton(itemName string) Option {
	return func(m *Manager) {
		m.singleton = true
		m.itemName = itemName
	}
}

// NewManager creates a new Manager for the given object type. The Registry is the shared store;
// the Constructor builds new items; the ListExtractor selects this manager's slice out of the full
// StunnerConfig.
func NewManager(name, objectType string, ctor Constructor, extractor ListExtractor, reg Registry, logger logging.LoggerFactory, opts ...Option) *Manager {
	m := &Manager{
		name:       name,
		objectType: objectType,
		ctor:       ctor,
		extractor:  extractor,
		reg:        reg,
		log:        logger.NewLogger(name),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *Manager) Name() string             { return m.name }
func (m *Manager) ObjectType() string       { return m.objectType }
func (m *Manager) Constructor() Constructor { return m.ctor }
func (m *Manager) Extractor() ListExtractor { return m.extractor }

func (m *Manager) Items() []object.Object { return m.reg.LookupAll(m.objectType) }

func (m *Manager) Get(name string) (object.Object, bool) {
	return m.reg.Lookup(m.objectType, name)
}

func (m *Manager) Keys() []string {
	items := m.Items()
	out := make([]string, len(items))
	for i, o := range items {
		out[i] = o.ObjectName()
	}
	return out
}
