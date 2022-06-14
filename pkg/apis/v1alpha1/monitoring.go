package v1alpha1

import (
	"fmt"
	"reflect"
)

// monitoring config yo
type MonitoringConfig struct {
	// Port is the port for the Prometheus scraper
	Port int `json:"port",omitempty`
	// PrometheusUrl is the URL for Prometheus scraper
	Url string `json:"url",omitempty`
	// PrometheusGroup is the Prometheus group label
	Group string `json:"group",omitempty`
}

// Validate checks a configuration and injects defaults
func (req *MonitoringConfig) Validate() error {
	// validate port
	if req.Port == 0 {
		req.Port = DefaultMonitoringPort
	}
	if req.Port <= 0 || req.Port > 65535 {
		return fmt.Errorf("invalid port: %d", req.Port)
	}

	// validate URL
	if req.Url == "" {
		req.Url = DefaultMonitoringUrl
	}

	//validate group
	if req.Group == "" {
		req.Group = DefaultMonitoringGroup
	}

	return nil
}

// Name returns the name of the object to be configured
func (req *MonitoringConfig) ConfigName() string {
	// singleton!
	return DefaultMonitoringName
}

// DeepEqual compares two configurations
func (req *MonitoringConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// String stringifies the configuration
func (req *MonitoringConfig) String() string {
	return fmt.Sprintf("%#v", req)
}
