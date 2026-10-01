package v2

import (
	"fmt"
	"strings"
)

// StunnerConfig is the configuration of the STUNner daemon.
type StunnerConfig struct {
	// ApiVersion is the version of the STUNner API implemented. Must be set to "v2".
	ApiVersion string `json:"version"`
	// Admin holds the administrative configuration.
	Admin AdminConfig `json:"admin,omitempty"`
	// Auth defines the client authentication mechanism.
	Auth AuthConfig `json:"auth"`
	// Listeners are the client-facing sockets.
	Listeners []ListenerConfig `json:"listeners,omitempty"`
	// Servers are the dataplane behaviors the listeners feed.
	Servers []ServerConfig `json:"servers,omitempty"`
	// Clusters are the peer endpoint sets the servers reach and the transports that reach them.
	Clusters []ClusterConfig `json:"clusters,omitempty"`
}

// Validate checks that a configuration is well-formed and injects defaults. It checks syntax only:
// what connects to what is never rejected, references to missing objects are config skew skipped
// at runtime, and a combination that makes no sense runs and fails at runtime.
func (req *StunnerConfig) Validate() error {
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
	}
	if req.Servers == nil {
		req.Servers = []ServerConfig{}
	}
	if req.Clusters == nil {
		req.Clusters = []ClusterConfig{}
	}

	names := map[string]bool{}
	checkName := func(kind, name string) error {
		if names[kind+"/"+name] {
			return fmt.Errorf("duplicate %s name %q", kind, name)
		}
		names[kind+"/"+name] = true
		return nil
	}
	for i := range req.Listeners {
		if err := req.Listeners[i].Validate(); err != nil {
			return err
		}
		if err := checkName("listener", req.Listeners[i].Name); err != nil {
			return err
		}
	}
	for i := range req.Servers {
		if err := req.Servers[i].Validate(); err != nil {
			return err
		}
		if err := checkName("server", req.Servers[i].Name); err != nil {
			return err
		}
	}
	for i := range req.Clusters {
		if err := req.Clusters[i].Validate(); err != nil {
			return err
		}
		if err := checkName("cluster", req.Clusters[i].Name); err != nil {
			return err
		}
	}

	return nil
}

// ConfigName returns the name of the object to be configured.
func (req *StunnerConfig) ConfigName() string {
	return DefaultStunnerName
}

// DeepEqual compares two configurations.
func (req *StunnerConfig) DeepEqual(conf Config) bool {
	b, ok := conf.(*StunnerConfig)
	if !ok {
		return false
	}
	if req.ApiVersion != b.ApiVersion || !req.Admin.DeepEqual(&b.Admin) ||
		!req.Auth.DeepEqual(&b.Auth) || len(req.Listeners) != len(b.Listeners) ||
		len(req.Servers) != len(b.Servers) || len(req.Clusters) != len(b.Clusters) {
		return false
	}
	for i := range req.Listeners {
		if !req.Listeners[i].DeepEqual(&b.Listeners[i]) {
			return false
		}
	}
	for i := range req.Servers {
		if !req.Servers[i].DeepEqual(&b.Servers[i]) {
			return false
		}
	}
	for i := range req.Clusters {
		if !req.Clusters[i].DeepEqual(&b.Clusters[i]) {
			return false
		}
	}
	return true
}

// DeepCopyInto copies a configuration into another config.
func (req *StunnerConfig) DeepCopyInto(dst Config) {
	ret := dst.(*StunnerConfig)
	ret.ApiVersion = req.ApiVersion
	req.Admin.DeepCopyInto(&ret.Admin)
	req.Auth.DeepCopyInto(&ret.Auth)
	ret.Listeners = make([]ListenerConfig, len(req.Listeners))
	for i := range req.Listeners {
		req.Listeners[i].DeepCopyInto(&ret.Listeners[i])
	}
	ret.Servers = make([]ServerConfig, len(req.Servers))
	for i := range req.Servers {
		req.Servers[i].DeepCopyInto(&ret.Servers[i])
	}
	ret.Clusters = make([]ClusterConfig, len(req.Clusters))
	for i := range req.Clusters {
		req.Clusters[i].DeepCopyInto(&ret.Clusters[i])
	}
}

// DeepCopy copies a configuration.
func (req *StunnerConfig) DeepCopy() *StunnerConfig {
	c := &StunnerConfig{}
	req.DeepCopyInto(c)
	return c
}

// GetListenerConfig finds a listener by name.
func (req *StunnerConfig) GetListenerConfig(name string) (ListenerConfig, error) {
	for _, l := range req.Listeners {
		if l.Name == name {
			return l, nil
		}
	}
	return ListenerConfig{}, ErrNoSuchListener
}

// GetServerConfig finds a server by name.
func (req *StunnerConfig) GetServerConfig(name string) (ServerConfig, error) {
	for _, s := range req.Servers {
		if s.Name == name {
			return s, nil
		}
	}
	return ServerConfig{}, ErrNoSuchServer
}

// GetClusterConfig finds a cluster by name.
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
	status := []string{fmt.Sprintf("version=%q", req.ApiVersion), req.Admin.String(),
		req.Auth.String()}
	ls := []string{}
	for _, l := range req.Listeners {
		ls = append(ls, l.String())
	}
	status = append(status, fmt.Sprintf("listeners=[%s]", strings.Join(ls, ",")))
	ss := []string{}
	for _, s := range req.Servers {
		ss = append(ss, s.String())
	}
	status = append(status, fmt.Sprintf("servers=[%s]", strings.Join(ss, ",")))
	cs := []string{}
	for _, c := range req.Clusters {
		cs = append(cs, c.String())
	}
	status = append(status, fmt.Sprintf("clusters=[%s]", strings.Join(cs, ",")))
	return fmt.Sprintf("{%s}", strings.Join(status, ","))
}

// Summary returns a human-readable summary of the configuration: per listener, its server and the
// endpoints of the server's clusters.
func (req *StunnerConfig) Summary() string {
	strOrNone := func(s string) string {
		if s != "" {
			return s
		}
		return "<none>"
	}
	intOrNone := func(i int) string {
		if i != 0 {
			return fmt.Sprintf("%d", i)
		}
		return "<none>"
	}
	status := fmt.Sprintf("Gateway: %s (loglevel: %q)\n", req.Admin.Name, req.Admin.LogLevel)
	if t, err := NewAuthType(req.Auth.Type); err == nil {
		switch t {
		case AuthTypeStatic:
			status += fmt.Sprintf("Authentication type: static, username/password: %s/%s\n",
				req.Auth.Credentials["username"], req.Auth.Credentials["password"])
		case AuthTypeEphemeral:
			status += fmt.Sprintf("Authentication type: ephemeral, shared-secret: %s\n",
				req.Auth.Credentials["secret"])
		default:
			status += "Authentication type: none\n"
		}
	}

	status += "Listeners:\n"
	for _, l := range req.Listeners {
		addr := l.Addr
		switch l.Addr {
		case "$STUNNER_ADDR":
			addr = "<private-pod-ip-addr>"
		case DefaultNodeAddressPlaceholder:
			addr = "<node-ip-addr>"
		}
		server, _ := req.GetServerConfig(l.FirstServer())
		status += fmt.Sprintf("  - Name: %s\n", l.Name)
		status += fmt.Sprintf("    Protocol: %s\n", l.Protocol)
		status += fmt.Sprintf("    Server: %s (%s)\n", strOrNone(l.FirstServer()), strOrNone(server.Type))
		status += fmt.Sprintf("    Address:port: %s:%s\n", strOrNone(addr), intOrNone(l.Port))
		status += fmt.Sprintf("    Public address:port: %s:%s\n", strOrNone(l.PublicAddr),
			intOrNone(l.PublicPort))
		status += fmt.Sprintf("    Clusters: [%s]\n", strings.Join(server.Clusters, ", "))
		ep := []string{}
		for _, c := range server.Clusters {
			if cc, err := req.GetClusterConfig(c); err == nil {
				ep = append(ep, cc.Endpoints...)
			}
		}
		status += fmt.Sprintf("    Endpoints: [%s]\n", strings.Join(ep, ", "))
	}
	return status
}

// StunnerStatus is the status of the STUNner daemon.
type StunnerStatus struct {
	ApiVersion      string            `json:"version"`
	Admin           *AdminStatus      `json:"admin"`
	Auth            *AuthStatus       `json:"auth"`
	Listeners       []*ListenerStatus `json:"listeners"`
	Servers         []*ServerStatus   `json:"servers"`
	Clusters        []*ClusterStatus  `json:"clusters"`
	AllocationCount int               `json:"allocationCount"`
	Status          string            `json:"status"`
}

// String stringifies the status.
func (s *StunnerStatus) String() string {
	ls, ss, cs := []string{}, []string{}, []string{}
	for _, l := range s.Listeners {
		ls = append(ls, l.String())
	}
	for _, v := range s.Servers {
		ss = append(ss, v.String())
	}
	for _, c := range s.Clusters {
		cs = append(cs, c.String())
	}
	return fmt.Sprintf("%s/%s/listeners:[%s]/servers:[%s]/clusters:[%s]/allocs:%d/status=%s",
		s.Admin.String(), s.Auth.String(), strings.Join(ls, ","), strings.Join(ss, ","),
		strings.Join(cs, ","), s.AllocationCount, s.Status)
}

// OffloadStatus holds the offload engine's runtime status and traffic counters: the client side
// per listener, the peer side per cluster.
type OffloadStatus struct {
	Engine     string                    `json:"engine,omitempty"`
	Interfaces []string                  `json:"interfaces,omitempty"`
	Listeners  map[string]OffloadDirStat `json:"listeners,omitempty"`
	Clusters   map[string]OffloadDirStat `json:"clusters,omitempty"`
}

// String stringifies the offload status.
func (s *OffloadStatus) String() string {
	if s == nil {
		return "offload:{}"
	}
	return fmt.Sprintf("offload:{engine=%q,interfaces=[%s],listeners=%d,clusters=%d}",
		s.Engine, strings.Join(s.Interfaces, ","), len(s.Listeners), len(s.Clusters))
}

// Summary returns a multi-line summary of the status.
func (s *StunnerStatus) Summary() string {
	ls, ss, cs := []string{}, []string{}, []string{}
	for _, l := range s.Listeners {
		ls = append(ls, l.String())
	}
	for _, v := range s.Servers {
		ss = append(ss, v.String())
	}
	for _, c := range s.Clusters {
		cs = append(cs, c.String())
	}
	return fmt.Sprintf("%s\n\t%s\n\tlisteners:%s\n\tservers:%s\n\tclusters:%s\n\t"+
		"allocs:%d/status=%s", s.Admin.String(), s.Auth.String(), strings.Join(ls, ","),
		strings.Join(ss, ","), strings.Join(cs, ","), s.AllocationCount, s.Status)
}
