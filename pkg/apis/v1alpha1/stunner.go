package v1alpha1

import (
	"fmt"
	"sort"

	"github.com/pion/transport/vnet"
)

// StunnerConfig configures the STUnner daemon
type StunnerConfig struct {
	// ApiVersion is the version of the STUNner API implemented
	ApiVersion string `json:"version"`
	// AdminConfig holds the administrative configuration
	Admin AdminConfig `json:"admin,omitempty"`
	// Auth defines the specification of the STUN/TURN authentication mechanism used by STUNner
	Auth AuthConfig `json:"auth"`
	// Monitoring holds the Prometheus configuration
	Monitoring MonitoringConfig `json:"monitoring"`
	// Listeners defines the listeners for STUNner
	Listeners []ListenerConfig `json:"listeners,omitempty"`
	// Clusters defines the upstream endpoints to which transport peer connections can be made
	// through STUNner
	Clusters []ClusterConfig `json:"clusters,omitempty"`
	Net      *vnet.Net       `json:"-"`
}

// Validate checks if a listener configuration is correct
func (req *StunnerConfig) Validate() error {
	// ApiVersion
	if req.ApiVersion != ApiVersion {
		return fmt.Errorf("unsupported API version: %s", req.ApiVersion)
	}

	// validate admin
	if err := req.Admin.Validate(); err != nil {
		return err
	}

	// validate auth
	if err := req.Auth.Validate(); err != nil {
		return err
	}

	// validate monitoring
	if err := req.Monitoring.Validate(); err != nil {
		return err
	}

	// validate listeners
	for i, c := range req.Listeners {
		if err := c.Validate(); err != nil {
			return err
		}
		req.Listeners[i] = c
	}
	// listeners are sorted by name
	sort.Slice(req.Listeners, func(i, j int) bool {
		return req.Listeners[i].Name < req.Listeners[j].Name
	})

	// validate clusters
	for i, c := range req.Clusters {
		if err := c.Validate(); err != nil {
			return err
		}
		req.Clusters[i] = c
	}

	// clusters are sorted by name
	sort.Slice(req.Clusters, func(i, j int) bool {
		return req.Clusters[i].Name < req.Clusters[j].Name
	})

	return nil
}

// Name returns the name of the object to be configured
func (req *StunnerConfig) ConfigName() string {
	return req.Admin.Name
}

// DeepEqual compares two configurations
func (req *StunnerConfig) DeepEqual(conf Config) bool {
	other, ok := conf.(*StunnerConfig)
	if !ok {
		return false
	}

	if req.ApiVersion != other.ApiVersion {
		return false
	}
	if !req.Admin.DeepEqual(&other.Admin) {
		return false
	}
	if !req.Auth.DeepEqual(&other.Auth) {
		return false
	}

	if !req.Monitoring.DeepEqual(&other.Monitoring) {
		return false
	}

	for i := range req.Listeners {
		if !req.Listeners[i].DeepEqual(&other.Listeners[i]) {
			return false
		}
	}

	for i := range req.Clusters {
		if !req.Clusters[i].DeepEqual(&other.Clusters[i]) {
			return false
		}
	}

	return true
}

// String stringifies the configuration
func (req *StunnerConfig) String() string {
	return fmt.Sprintf("%#v", req)
}
