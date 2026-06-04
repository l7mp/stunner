package object

import (
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Action is the reconciliation action associated with an object.
type Action int

const (
	// ActionNone means no object-local change is needed.
	ActionNone Action = iota
	// ActionReconcile means object-local state changes but no restart is required.
	ActionReconcile
	// ActionRestart means object-local state changes and restart is required.
	ActionRestart
	// ActionNew means the object does not exist yet and must be created.
	ActionNew
	// ActionDelete means the object must be closed and removed.
	ActionDelete
)

// Object is the high-level interface for all STUNner objects (listeners, clusters, etc.). Objects
// form a tree rooted at the Stunner object; the Reconciler walks the tree and drives each Object
// through the four phases (prepare → close → reconcile/create → start).
type Object interface {
	// ObjectName returns the unique name of the object within its type.
	ObjectName() string
	// ObjectType returns the type tag of the object. Used by the Registry for keying and by
	// the Reconciler for routing.
	ObjectType() string

	// Extract pulls this Object's typed subconfig out of the full StunnerConfig.
	Extract(c *stnrv1.StunnerConfig) (stnrv1.Config, error)

	// GetConfig returns the live configuration of the Object. Composite objects build this by
	// querying their children from the Registry.
	GetConfig() stnrv1.Config
	// Status returns the live status of the Object.
	Status() stnrv1.Status

	// Inspect compares the live state (old), desired object config (new), and the full desired
	// StunnerConfig (full) and reports the object-local decision. Recursion into children is
	// handled by the reconciler walk, not Inspect.
	Inspect(old, new stnrv1.Config, full *stnrv1.StunnerConfig) (Action, error)
	// Reconcile applies a new config to the Object. The Object must already be Closed if the
	// previous Inspect call returned restart=true.
	Reconcile(conf stnrv1.Config) error
	// Start brings the Object online. Called for newly created Objects and for Objects that
	// were Closed because Inspect asked for a restart.
	Start() error
	// Close shuts the Object down. The shutdown flag distinguishes terminate (true) from
	// reconcile-driven restart (false). Offload.Close(false) is a no-op — the eBPF engine
	// survives reconciliation; Offload.Close(true) tears it down.
	Close(shutdown bool) error
}

// ReadinessHandler is a callback that allows an object to check the readiness of STUNner.
type ReadinessHandler = func() error

// RealmHandler is a callback that allows an object to find out the authentication realm.
type RealmHandler = func() string

// StatusHandler is a callback that allows an object to obtain the status of STUNNer.
type StatusHandler = func() stnrv1.Status
