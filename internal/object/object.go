package object

import (
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Object is the high-level interface for all STUNner objects like listeners, clusters, etc.
type Object interface {
	// ObjectName returns the name of the object
	ObjectName() string
	// Inspect examines whether a configuration change on the object would require a restart
	Inspect(old, new v1alpha1.Config) bool
	// Reconcile updates the object for a new configuration, may return ErrRestartRequired
	Reconcile(conf v1alpha1.Config) error
	// GetConfig returns the configuration of the running authenticator
	GetConfig() v1alpha1.Config
	// Close closes the object, may return ErrRestartRequired
	Close() error
}

// Factory can create new objects
type Factory interface {
	// New will spawn a new object from the factory
	New(conf v1alpha1.Config) (Object, error)
}
