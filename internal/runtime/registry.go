package runtime

import (
	"fmt"
	"sort"
	"sync"
)

// regKey identifies a node in the registry.
type regKey struct {
	typ  ObjectType
	name string
}

type regNode struct {
	obj    Runnable
	parent regKey
	isRoot bool
}

// Registry stores every live node in the dataplane keyed by (Type, Name), plus a parent edge
// per node so the reconciler can walk subtrees. Thread-safe.
type Registry struct {
	mu    sync.RWMutex
	items map[regKey]*regNode
}

// NewRegistry returns a new empty Registry.
func NewRegistry() *Registry {
	return &Registry{items: make(map[regKey]*regNode)}
}

func keyOf(o Runnable) regKey { return regKey{o.Type(), o.Name()} }

// Get returns the named node of the given type, if present.
func (r *Registry) Get(typ ObjectType, name string) (Runnable, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.items[regKey{typ, name}]
	if !ok {
		return nil, false
	}
	return n.obj, true
}

// List returns every node of the given type in stable (by name) order.
func (r *Registry) List(typ ObjectType) []Runnable {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Runnable
	for k, n := range r.items {
		if k.typ == typ {
			out = append(out, n.obj)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Add registers a node under the given parent (nil for the root). Returns an error if a node
// with the same (type, name) already exists.
func (r *Registry) Add(o Runnable, parent Runnable) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := keyOf(o)
	if _, ok := r.items[k]; ok {
		return fmt.Errorf("registry: duplicate (type:%s/name:%s)", k.typ, k.name)
	}
	n := &regNode{obj: o}
	if parent == nil {
		n.isRoot = true
	} else {
		n.parent = keyOf(parent)
	}
	r.items[k] = n
	return nil
}

// Remove unregisters a node.
func (r *Registry) Remove(o Runnable) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, keyOf(o))
	return nil
}

// ChildrenOf returns the nodes of the given type whose parent is the given node, in stable
// (by name) order.
func (r *Registry) ChildrenOf(parent Runnable, typ ObjectType) []Runnable {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var pk regKey
	isRoot := parent == nil
	if !isRoot {
		pk = keyOf(parent)
	}
	var out []Runnable
	for k, n := range r.items {
		if k.typ != typ {
			continue
		}
		if isRoot {
			if n.isRoot {
				out = append(out, n.obj)
			}
			continue
		}
		if n.parent == pk {
			out = append(out, n.obj)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
