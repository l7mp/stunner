package v1

import (
	"fmt"
	// "sort"
	"strings"
)

// StunnerConfig specifies the configuration for the STUnner daemon.
type StunnerConfig struct {
	// ApiVersion is the version of the STUNner API implemented. Must be set to "v1".
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
		return fmt.Errorf("unsupported API version: %q", req.ApiVersion)
	}

	if err := req.Admin.Validate(); err != nil {
		return err
	}

	if err := req.Auth.Validate(); err != nil {
		return err
	}

	if req.Listeners == nil {
		req.Listeners = []ListenerConfig{}
	} else {
		for i, l := range req.Listeners {
			if err := l.Validate(); err != nil {
				return err
			}
			req.Listeners[i] = l
		}
	}

	if req.Clusters == nil {
		req.Clusters = []ClusterConfig{}
	} else {
		for i, c := range req.Clusters {
			if err := c.Validate(); err != nil {
				return err
			}
			req.Clusters[i] = c
		}
	}

	return nil
}

// Name returns the name of the object to be configured.
func (req *StunnerConfig) ConfigName() string {
	return req.Admin.Name
}

// DeepEqual compares two configurations.
func (a *StunnerConfig) DeepEqual(conf Config) bool {
	b, ok := conf.(*StunnerConfig)
	if !ok {
		return false
	}

	if a.ApiVersion != b.ApiVersion {
		return false
	}

	if !a.Admin.DeepEqual(&b.Admin) {
		return false
	}

	if !a.Auth.DeepEqual(&b.Auth) {
		return false
	}

	if len(a.Listeners) != len(b.Listeners) {
		return false
	}
	for i := range a.Listeners {
		if !a.Listeners[i].DeepEqual(&b.Listeners[i]) {
			return false
		}
	}

	if len(a.Clusters) != len(b.Clusters) {
		return false
	}
	for i := range a.Clusters {
		if !a.Clusters[i].DeepEqual(&b.Clusters[i]) {
			return false
		}
	}

	return true
}

// DeepCopyInto copies a configuration.
func (req *StunnerConfig) DeepCopyInto(dst Config) {
	ret := dst.(*StunnerConfig)
	ret.ApiVersion = req.ApiVersion
	req.Admin.DeepCopyInto(&ret.Admin)
	req.Auth.DeepCopyInto(&ret.Auth)

	ret.Listeners = make([]ListenerConfig, len(req.Listeners))
	for i := range req.Listeners {
		req.Listeners[i].DeepCopyInto(&ret.Listeners[i])
	}

	ret.Clusters = make([]ClusterConfig, len(req.Clusters))
	for i := range req.Clusters {
		req.Clusters[i].DeepCopyInto(&ret.Clusters[i])
	}
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

// Summary returns a stringified configuration.
func (req *StunnerConfig) Summary() string {
	// isEnabled = func(b bool) string { if b {return "enabled"} else {return "disabled"}}
	strOrNone := func(s string) string {
		if s != "" {
			return s
		} else {
			return "<none>"
		}
	}
	intOrNone := func(s int) string {
		if s != 0 {
			return fmt.Sprintf("%d", s)
		} else {
			return "<none>"
		}
	}
	status := fmt.Sprintf("Gateway: %s (loglevel: %q)\n", req.Admin.Name, req.Admin.LogLevel)
	if t, err := NewAuthType(req.Auth.Type); err == nil {
		if t == AuthTypeStatic {
			status += fmt.Sprintf("Authentication type: static, username/password: %s/%s\n",
				req.Auth.Credentials["username"], req.Auth.Credentials["password"])
		} else {
			status += fmt.Sprintf("Authentication type: ephemeral, shared-secret: %s\n",
				req.Auth.Credentials["secret"])
		}
	}

	status += "Listeners:\n"
	for _, l := range req.Listeners {
		status += fmt.Sprintf("  - Name: %s\n", l.Name)
		status += fmt.Sprintf("    Protocol: %s\n", l.Protocol)
		status += fmt.Sprintf("    Public address:port: %s:%s\n", strOrNone(l.PublicAddr), intOrNone(l.PublicPort))
		status += fmt.Sprintf("    Routes: [%s]\n", strings.Join(l.Routes, ", "))
		ep := []string{}
		for _, r := range l.Routes {
			if c, err := req.GetClusterConfig(r); err == nil {
				ep = append(ep, c.Endpoints...)
			}
		}
		status += fmt.Sprintf("    Endpoints: [%s]\n", strings.Join(ep, ", "))
	}

	return status
}

// StunnerStatus represents the status of the STUnner daemon.
type StunnerStatus struct {
	ApiVersion      string            `json:"version"`
	Admin           *AdminStatus      `json:"admin"`
	Auth            *AuthStatus       `json:"auth"`
	Listeners       []*ListenerStatus `json:"listeners"`
	Clusters        []*ClusterStatus  `json:"clusters"`
	AllocationCount int               `json:"allocationCount"`
	Status          string            `json:"status"`
}

// String stringifies the status.
func (s *StunnerStatus) String() string {
	ls := []string{}
	for _, l := range s.Listeners {
		ls = append(ls, l.String())
	}
	cs := []string{}
	for _, c := range s.Clusters {
		cs = append(cs, c.String())
	}

	return fmt.Sprintf("%s/%s/%s/%s/allocs:%d/status=%s", s.Admin.String(), s.Auth.String(),
		ls, cs, s.AllocationCount, s.Status)
}

// String summarizes the status.
func (s *StunnerStatus) Summary() string {
	return fmt.Sprintf("%s\n\t%s\n\tlisteners:%d/clusters:%d\n\tallocs:%d/status=%s",
		s.Admin.String(), s.Auth.String(), len(s.Listeners), len(s.Clusters),
		s.AllocationCount, s.Status)
}
