package object

import (
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Object is the high-level interface for all STUNner objects like listeners, clusters, etc.
type Object interface {
	// ObjectName returns the name of the object.
	ObjectName() string
	// ObjectType returns the type of the object.
	ObjectType() string
	// Inspect examines whether a configuration change requires a reconciliation or restart.
	Inspect(old, new, full v1alpha1.Config) (bool, error)
	// Reconcile updates the object for a new configuration.
	Reconcile(conf v1alpha1.Config) error
	// GetConfig returns the configuration of the running authenticator.
	GetConfig() v1alpha1.Config
	// Close closes the object, may return ErrRestartRequired.
	Close() error
}

// Factory can create new objects
type Factory interface {
	// New will spawn a new object from the factory
	New(conf v1alpha1.Config) (Object, error)
}

// ReadinessHandler is a callback that allows an object to check the readiness of STUNner.
type ReadinessHandler = func() error

// RealmHandler is a callback that allows an object to find out the authentication realm.
type RealmHandler = func() string
