// Package runtime is the kernel of the STUNner object system: it defines the node contracts
// (Runnable, Reconcilable, Object), the Registry that stores every live node keyed by (type,
// name) with parent edges, and the Runtime, the single cross-object access point carrying
// process-wide dependencies, registry-backed config/status lookups, relay routing, and the
// readiness/shutdown flags.
package runtime

import (
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// ObjectType identifies a runtime object kind.
type ObjectType string

// Type names for the various object kinds.
const (
	TypeStunner        ObjectType = "stunner"
	TypeAdmin          ObjectType = "admin"
	TypeAuth           ObjectType = "auth"
	TypeHealth         ObjectType = "health"
	TypeMetrics        ObjectType = "metrics"
	TypeOffload        ObjectType = "offload"
	TypeListener       ObjectType = "listener"
	TypeListenerServer ObjectType = "listener-server"
	TypeCluster        ObjectType = "cluster"
	TypeRelay          ObjectType = "relay"
)

// Action is the reconciliation action an Object reports from Inspect.
type Action int

const (
	// ActionNone means no object-local change is needed.
	ActionNone Action = iota
	// ActionReconcile means object-local state changes but no restart is required.
	ActionReconcile
	// ActionRestart means object-local state changes and the object (plus its subtree) must
	// be closed, reconciled and started again.
	ActionRestart
)

// Runnable is the minimal node contract in the object tree: identity plus lifecycle.
// Lifecycle-only nodes (listener servers, cluster relays) implement just this.
type Runnable interface {
	Name() string
	Type() ObjectType
	// Start brings the node online.
	Start() error
	// Close shuts the node down. The shutdown flag distinguishes terminate (true) from
	// reconcile-driven restart (false).
	Close(shutdown bool) error
}

// Reconcilable is the desired-state convergence contract for config-driven objects.
type Reconcilable interface {
	// GetConfig returns the live running config. Implementations MUST be safe for
	// concurrent use: the dataplane reads configs from request handlers while the
	// reconciler may be writing (use an atomic snapshot, see the concrete objects).
	GetConfig() stnrv1.Config
	// Inspect compares the live state (old), desired object config (new), and the full
	// desired StunnerConfig (full) and reports the object-local decision. Recursion into
	// children is handled by the reconciler walk, not Inspect.
	Inspect(old, new stnrv1.Config, full *stnrv1.StunnerConfig) (Action, error)
	// Reconcile applies a new config to the Object. The Object must already be Closed if
	// the previous Inspect call returned ActionRestart.
	Reconcile(conf stnrv1.Config) error
	// Status returns the live status of the Object.
	Status() stnrv1.Status
}

// Object is the full contract for config-driven STUNner objects.
type Object interface {
	Runnable
	Reconcilable
}
