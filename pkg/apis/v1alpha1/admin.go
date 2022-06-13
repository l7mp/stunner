package v1alpha1

import (
	"fmt"
	"reflect"
)

// AdminConfig holds the administrative configuration
type AdminConfig struct {
	// Name is the name of the server, optional
	Name string `json:"name,omitempty"`
	// LogLevel is the desired log verbosity, e.g.: "stunner:TRACE,all:INFO"
	LogLevel string `json:"logLevel,omitempty"`
}

// Validate checks a configuration and injects defaults
func (req *AdminConfig) Validate() error {
	if req.LogLevel == "" {
		req.LogLevel = DefaultLogLevel
	}
	if req.Name == "" {
		req.Name = DefaultStunnerName
	}

	return nil
}

// Name returns the name of the object to be configured
func (req *AdminConfig) ConfigName() string {
	// singleton!
	return DefaultAdminName
}

// DeepEqual compares two configurations
func (req *AdminConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// String stringifies the configuration
func (req *AdminConfig) String() string {
	return fmt.Sprintf("%#v", req)
}
