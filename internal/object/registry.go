package object

// Type names for the various Object kinds. Used as keys in the Registry.
const (
	TypeStunner      = "stunner"
	TypeAdmin        = "admin"
	TypeAuth         = "auth"
	TypeHealth       = "health"
	TypeMetrics      = "metrics"
	TypeOffload      = "offload"
	TypeListenerList = "listeners"
	TypeListener     = "listener"
	TypeClusterList  = "clusters"
	TypeCluster      = "cluster"
)

// Singleton names. For singleton Object kinds, the ObjectName is fixed.
const (
	DefaultListenerListName = "default-listener-list"
	DefaultClusterListName  = "default-cluster-list"
	DefaultHealthName       = "default-health"
	DefaultMetricsName      = "default-metrics"
	DefaultOffloadName      = "default-offload"
)

// Registry stores every live Object in the dataplane, keyed by (ObjectType, ObjectName). The
// Registry replaces the old object.Router: it serves the same runtime cross-reference role (e.g.,
// the TURN auth handler looks up the Auth object at request time) but with a flat, generic shape.
type Registry interface {
	// Lookup returns the named object of the given type, if present.
	Lookup(objType, name string) (Object, bool)
	// LookupOne returns the single object of the given type. For singleton kinds (Admin, Auth,
	// Health, ...) this is the natural query; ok is false if there is no such object.
	LookupOne(objType string) (Object, bool)
	// LookupAll returns every object of the given type in stable (by name) order.
	LookupAll(objType string) []Object
	// Add registers an object. Returns an error if an object with the same (type, name) is
	// already registered.
	Add(o Object) error
	// Remove unregisters an object.
	Remove(o Object) error
}
