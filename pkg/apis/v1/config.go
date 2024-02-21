// Package v1 is the v1 version of the STUNner API.
package v1

// Config is the main interface for STUNner configuration objects.
type Config interface {
	// Validate checks a configuration and injects defaults.
	Validate() error
	// Name returns the name of the object to be configured.
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
