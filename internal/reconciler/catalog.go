package reconciler

import (
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// KindSpec is the complete declarative description of one object kind: its constructor, its
// desired-config resolver, and its child kinds.
type KindSpec struct {
	// Type is the object kind described by this spec.
	Type runtime.ObjectType

	// Children lists the child kinds living under instances of this kind.
	Children []runtime.ObjectType

	// New constructs an instance. The parent is the owning node (nil for the root).
	New func(parent runtime.Runnable, conf stnrv1.Config, rt *runtime.Runtime) (runtime.Runnable, error)

	// ExtractConfigs answers: "given this parent and this full desired config, which instances of
	// this kind should exist under the parent, and with what configs?" It is given the parent's
	// name rather than the parent object, so it can only read the desired config and never stale
	// parent object state (parentName is "" for the root's kind).
	ExtractConfigs func(parentName string, full *stnrv1.StunnerConfig) ([]stnrv1.Config, error)

	// Singleton marks kinds with exactly one instance per parent; SingletonName resolves the
	// instance name from the parent's name ("" for the root's kind).
	Singleton     bool
	SingletonName func(parentName string) string
}

// Catalog stores the object kind specifications walked by the reconciler.
type Catalog struct {
	kinds map[runtime.ObjectType]KindSpec
}

// NewCatalogFromKinds builds a catalog from explicit kind specs.
func NewCatalogFromKinds(specs ...KindSpec) *Catalog {
	kinds := map[runtime.ObjectType]KindSpec{}
	for _, spec := range specs {
		kinds[spec.Type] = spec
	}
	return &Catalog{kinds: kinds}
}

// Kind returns the type specification for an object type.
func (c *Catalog) Kind(objType runtime.ObjectType) (KindSpec, bool) {
	spec, ok := c.kinds[objType]
	return spec, ok
}
