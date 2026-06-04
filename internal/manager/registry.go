package manager

import (
	"fmt"
	"sort"
	"sync"

	"github.com/l7mp/stunner/internal/object"
)

// Registry is the manager-side registry interface: it exposes object storage plus topology wiring
// helpers used by the reconciliation walk.
type Registry interface {
	object.Registry
	AttachSubManager(o object.Object, sm *Manager) error
	NodeOf(o object.Object) *Node
}

// Node ties an Object to its sub-managers. Nodes are stored by the Registry and looked up by the
// reconciliation walk. There is exactly one Node per registered Object.
type Node struct {
	// Object is the Object this Node represents.
	Object object.Object
	// SubManagers are the Managers whose items live under this Object. For the root Stunner
	// Object these are adminMgr/authMgr/listenerListMgr/clusterListMgr; for Admin they are
	// healthMgr/metricsMgr/offloadMgr; for ListenerList just the per-Listener manager; etc.
	SubManagers []*Manager
}

// registry implements object.Registry. It also tracks Node metadata (per-Object sub-managers) used
// by the reconciliation walk.
type registry struct {
	mu    sync.RWMutex
	items map[regKey]*Node
}

type regKey struct {
	typ, name string
}

// NewRegistry returns a new empty Registry.
func NewRegistry() Registry {
	return &registry{items: make(map[regKey]*Node)}
}

func (r *registry) key(o object.Object) regKey { return regKey{o.ObjectType(), o.ObjectName()} }

// Lookup returns the named object of the given type, if present.
func (r *registry) Lookup(typ, name string) (object.Object, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.items[regKey{typ, name}]
	if !ok {
		return nil, false
	}
	return n.Object, true
}

// LookupOne returns the single object of the given type. Useful for singleton kinds.
func (r *registry) LookupOne(typ string) (object.Object, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var found object.Object
	for k, n := range r.items {
		if k.typ != typ {
			continue
		}
		if found != nil {
			// More than one — caller should have used LookupAll. Return the first by name
			// for determinism rather than failing.
			break
		}
		found = n.Object
	}
	if found != nil {
		return found, true
	}
	return nil, false
}

// LookupAll returns every object of the given type in stable (by name) order.
func (r *registry) LookupAll(typ string) []object.Object {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []object.Object
	for k, n := range r.items {
		if k.typ == typ {
			out = append(out, n.Object)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ObjectName() < out[j].ObjectName() })
	return out
}

// Add registers an object. Returns an error if an object with the same (type, name) already
// exists.
func (r *registry) Add(o object.Object) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(o)
	if _, ok := r.items[k]; ok {
		return fmt.Errorf("registry: duplicate (type:%s/name:%s)", k.typ, k.name)
	}
	r.items[k] = &Node{Object: o}
	return nil
}

// Remove unregisters an object.
func (r *registry) Remove(o object.Object) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, r.key(o))
	return nil
}

// NodeOf returns the Node attached to o, if any. Used to walk sub-managers.
func (r *registry) NodeOf(o object.Object) *Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.items[r.key(o)]
}

// AttachSubManager wires a sub-manager onto the Node owning o. Idempotent on (o, sm.Name) so the
// caller can re-attach defensively.
func (r *registry) AttachSubManager(o object.Object, sm *Manager) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.key(o)
	n, ok := r.items[k]
	if !ok {
		return fmt.Errorf("registry: attach: no such object (%s, %s)", k.typ, k.name)
	}
	for i, existing := range n.SubManagers {
		if existing.Name() == sm.Name() {
			n.SubManagers[i] = sm
			return nil
		}
	}
	n.SubManagers = append(n.SubManagers, sm)
	return nil
}
