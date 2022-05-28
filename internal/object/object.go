package object

import (
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Object is the high-level interface for all STUNner objects like listeners, clusters, etc.
type Object interface {
        // ObjectName returns the name of the object
        ObjectName() string
        // Reconcile updates the object for a new configuration. May restart the server, always check returned error for ErrRestartRequired
        Reconcile(conf v1alpha1.Config) error
        // GetConfig returns the configuration of the running authenticator
        GetConfig() v1alpha1.Config
        // Close closes the object
        Close()
}

