package v1alpha1

import (
	"fmt"
	"sort"
	"strings"
)

// StunnerConfig specifies the configuration of the the STUnner daemon.
type StunnerConfig struct {
	// ApiVersion is the version of the STUNner API implemented.
	ApiVersion string `json:"version"`
	// AdminConfig holds administrative configuration.
	Admin AdminConfig `json:"admin,omitempty"`
	// Auth defines the STUN/TURN authentication mechanism.
	Auth AuthConfig `json:"auth"`
	// Listeners defines the server sockets exposed to clients.
	Listeners []ListenerConfig `json:"listeners,omitempty"`
	// Clusters defines the upstream endpoints to which relay transport connections can be made
	// by clients.
	Clusters []ClusterConfig `json:"clusters,omitempty"`
}

// Validate checks if a listener configuration is correct.
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

	// validate listeners
	for i, l := range req.Listeners {
		if err := l.Validate(); err != nil {
			return err
		}
		req.Listeners[i] = l
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

// Name returns the name of the object to be configured.
func (req *StunnerConfig) ConfigName() string {
	return req.Admin.Name
}

// DeepEqual compares two configurations.
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

// String stringifies the configuration.
func (req *StunnerConfig) String() string {
	status := []string{}
	status = append(status, fmt.Sprintf("version=%q", req.ApiVersion))
	status = append(status, req.Admin.String())
	status = append(status, req.Auth.String())

	ls := []string{}
	for _, l := range req.Listeners {
		ls = append(ls, l.String())
	}
	status = append(status, fmt.Sprintf("listeners=[%s]", strings.Join(ls, ",")))

	cs := []string{}
	for _, c := range req.Clusters {
		cs = append(cs, c.String())
	}
	status = append(status, fmt.Sprintf("clusters=[%s]", strings.Join(cs, ",")))

	return fmt.Sprintf("{%s}", strings.Join(status, ","))
}

// GetListenerConfig finds a Listener by name in a StunnerConfig or returns an error.
func (req *StunnerConfig) GetListenerConfig(name string) (ListenerConfig, error) {
	for _, l := range req.Listeners {
		if l.Name == name {
			return l, nil
		}
	}

	return ListenerConfig{}, ErrNoSuchListener
}

// GetClusterConfig finds a Cluster by name in a StunnerConfig or returns an error.
func (req *StunnerConfig) GetClusterConfig(name string) (ClusterConfig, error) {
	for _, c := range req.Clusters {
		if c.Name == name {
			return c, nil
		}
	}

	return ClusterConfig{}, ErrNoSuchCluster
}
