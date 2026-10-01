// Package v2 is the v2 version of the STUNner API. A config is a set of listeners, servers and
// clusters, referenced by name: a listener names the servers it feeds, a server names the clusters
// it reaches peers through, in order. The package is self-contained; v1 configs convert into it
// with v1.ConvertToV2.
package v2

// Config is the main interface for STUNner configuration objects.
type Config interface {
	// Validate checks a configuration and injects defaults.
	Validate() error
	// ConfigName returns the name of the object to be configured.
	ConfigName() string
	// DeepEqual compares two configurations.
	DeepEqual(other Config) bool
	// DeepCopyInto copies a configuration.
	DeepCopyInto(dst Config)
	// String stringifies the configuration.
	String() string
}

// Status holds the status of a component.
type Status interface {
	// String stringifies the status.
	String() string
}
