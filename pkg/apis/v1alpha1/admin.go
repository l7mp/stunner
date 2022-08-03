package v1alpha1

import (
	"fmt"
	"net/url"
	"reflect"
)

// AdminConfig holds the administrative configuration
type AdminConfig struct {
	// Name is the name of the server, optional
	Name string `json:"name,omitempty"`
	// LogLevel is the desired log verbosity, e.g.: "stunner:TRACE,all:INFO"
	LogLevel string `json:"loglevel,omitempty"`
	// MetricsEndpoint is the url to the metric server (Prometheus)
	MetricsEndpoint string `json:"metrics_endpoint,omitempty"`
}

// Validate checks a configuration and injects defaults
func (req *AdminConfig) Validate() error {
	//FIXME: no validation for loglevel (we'd need to create a new logger and it's not worth)
	if req.LogLevel == "" {
		req.LogLevel = DefaultLogLevel
	}
	if req.Name == "" {
		req.Name = DefaultStunnerName
	}

	//validate metrics endpoint
	_, err := url.Parse(req.MetricsEndpoint)
	if err != nil {
		return fmt.Errorf("%s: not a valid metric endpoint URL", req.MetricsEndpoint)
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
