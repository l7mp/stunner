package v2

import (
	"fmt"
	"strings"
	"time"
)

// StunnerConfig is the configuration of the STUNner daemon.
type StunnerConfig struct {
	// ApiVersion is the version of the STUNner API implemented. Must be set to "v2".
	ApiVersion string `json:"version"`
	// Admin holds the administrative configuration.
	Admin AdminConfig `json:"admin,omitempty"`
	// Auths are the authentication policies servers refer to by name.
	Auths []AuthConfig `json:"auths,omitempty"`
	// Listeners are the client-facing sockets.
	Listeners []ListenerConfig `json:"listeners,omitempty"`
	// Servers are the dataplane behaviors the listeners feed.
	Servers []ServerConfig `json:"servers,omitempty"`
	// Clusters are the peer endpoint sets the servers reach and the transports that reach them.
	Clusters []ClusterConfig `json:"clusters,omitempty"`
	// Filters are the rules listeners and clusters admit traffic by.
	Filters []FilterConfig `json:"filters,omitempty"`
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

	if req.Listeners == nil {
		req.Listeners = []ListenerConfig{}
	}
	if req.Servers == nil {
		req.Servers = []ServerConfig{}
	}
	if req.Clusters == nil {
		req.Clusters = []ClusterConfig{}
	}
	if req.Filters == nil {
		req.Filters = []FilterConfig{}
	}
	if req.Auths == nil {
		req.Auths = []AuthConfig{}
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
	for i := range req.Filters {
		if err := req.Filters[i].Validate(); err != nil {
			return err
		}
		if err := checkName("filter", req.Filters[i].Name); err != nil {
			return err
		}
	}
	for i := range req.Auths {
		if req.Auths[i].Name == "" {
			req.Auths[i].Name = DefaultAuthName
		}
		if err := req.Auths[i].Validate(); err != nil {
			return err
		}
		if err := checkName("auth", req.Auths[i].Name); err != nil {
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
		len(req.Auths) != len(b.Auths) || len(req.Listeners) != len(b.Listeners) ||
		len(req.Servers) != len(b.Servers) || len(req.Clusters) != len(b.Clusters) ||
		len(req.Filters) != len(b.Filters) {
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
	for i := range req.Filters {
		if !req.Filters[i].DeepEqual(&b.Filters[i]) {
			return false
		}
	}
	for i := range req.Auths {
		if !req.Auths[i].DeepEqual(&b.Auths[i]) {
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
	ret.Listeners, ret.Servers, ret.Clusters, ret.Filters, ret.Auths = nil, nil, nil, nil, nil
	if req.Auths != nil {
		ret.Auths = make([]AuthConfig, len(req.Auths))
		for i := range req.Auths {
			req.Auths[i].DeepCopyInto(&ret.Auths[i])
		}
	}
	if req.Listeners != nil {
		ret.Listeners = make([]ListenerConfig, len(req.Listeners))
		for i := range req.Listeners {
			req.Listeners[i].DeepCopyInto(&ret.Listeners[i])
		}
	}
	if req.Servers != nil {
		ret.Servers = make([]ServerConfig, len(req.Servers))
		for i := range req.Servers {
			req.Servers[i].DeepCopyInto(&ret.Servers[i])
		}
	}
	if req.Clusters != nil {
		ret.Clusters = make([]ClusterConfig, len(req.Clusters))
		for i := range req.Clusters {
			req.Clusters[i].DeepCopyInto(&ret.Clusters[i])
		}
	}
	if req.Filters != nil {
		ret.Filters = make([]FilterConfig, len(req.Filters))
		for i := range req.Filters {
			req.Filters[i].DeepCopyInto(&ret.Filters[i])
		}
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

// GetAuthConfig finds an authentication policy by name.
func (req *StunnerConfig) GetAuthConfig(name string) (AuthConfig, error) {
	for _, a := range req.Auths {
		if a.Name == name {
			return a, nil
		}
	}
	return AuthConfig{}, ErrNoSuchAuth
}

// GetFilterConfig finds a filter by name.
func (req *StunnerConfig) GetFilterConfig(name string) (FilterConfig, error) {
	for _, f := range req.Filters {
		if f.Name == name {
			return f, nil
		}
	}
	return FilterConfig{}, ErrNoSuchFilter
}

// GetTURNURIForListener returns the TURN URIs of a listener, one per public address: the
// listener's transport makes the TURN transport, at the public port or else the listen port, and
// the URIs carry the credentials of the auth policy of the first TURN server in the listener's
// chain, which an ephemeral policy mints for the user id, expiring at expiry, or after the
// policy's lifetime for a zero expiry.
func (req *StunnerConfig) GetTURNURIForListener(listener, userid string, expiry time.Time) ([]URI, error) {
	l, err := req.GetListenerConfig(listener)
	if err != nil {
		return nil, fmt.Errorf("no listener %q", listener)
	}
	proto, err := NewListenerProtocol(l.Protocol)
	if err != nil {
		return nil, fmt.Errorf("listener %q: %w", l.Name, err)
	}
	turnProto, ok := map[Protocol]Protocol{
		ProtocolUDP:  ProtocolTURNUDP,
		ProtocolTCP:  ProtocolTURNTCP,
		ProtocolTLS:  ProtocolTURNTLS,
		ProtocolDTLS: ProtocolTURNDTLS,
	}[proto]
	if !ok {
		return nil, fmt.Errorf("listener %q: no TURN URI for a %s listener", l.Name, proto.String())
	}
	var turn *TURNServerConfig
	for _, name := range l.Servers {
		if s, err := req.GetServerConfig(name); err == nil && s.TURN != nil {
			turn = s.TURN
			break
		}
	}
	if turn == nil {
		return nil, fmt.Errorf("listener %q feeds no TURN server", l.Name)
	}
	auth, err := req.GetAuthConfig(turn.Auth)
	if err != nil {
		return nil, fmt.Errorf("no authentication policy %q for listener %q", turn.Auth, l.Name)
	}
	username, password, err := auth.GenerateCredentials(userid, expiry)
	if err != nil {
		return nil, fmt.Errorf("listener %q: %w", l.Name, err)
	}

	addrs := l.PublicAddrs
	if len(addrs) == 0 && l.PublicAddr != "" {
		addrs = []string{l.PublicAddr}
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("listener %q has no public address", l.Name)
	}
	port := l.PublicPort
	if port == 0 {
		port = l.Port
	}
	uris := []URI{}
	for _, addr := range addrs {
		uris = append(uris, URI{Protocol: turnProto, Host: addr, Port: port, Username: username,
			Password: password})
	}
	return uris, nil
}

// String stringifies the configuration.
func (req *StunnerConfig) String() string {
	as := []string{}
	for _, a := range req.Auths {
		as = append(as, a.String())
	}
	status := []string{fmt.Sprintf("version=%q", req.ApiVersion), req.Admin.String(),
		fmt.Sprintf("auths=[%s]", strings.Join(as, ","))}
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
	if len(req.Filters) > 0 {
		fs := []string{}
		for _, f := range req.Filters {
			fs = append(fs, f.String())
		}
		status = append(status, fmt.Sprintf("filters=[%s]", strings.Join(fs, ",")))
	}
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
	for _, a := range req.Auths {
		t, err := NewAuthType(a.Type)
		if err != nil {
			continue
		}
		switch t {
		case AuthTypeStatic:
			status += fmt.Sprintf("Authentication %s: static, username/password: %s/%s\n", a.Name,
				a.Credentials["username"], a.Credentials["password"])
		case AuthTypeEphemeral:
			status += fmt.Sprintf("Authentication %s: ephemeral, shared-secret: %s\n", a.Name,
				a.Credentials["secret"])
		default:
			status += fmt.Sprintf("Authentication %s: none\n", a.Name)
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
	Auths           []*AuthStatus     `json:"auths"`
	Listeners       []*ListenerStatus `json:"listeners"`
	Servers         []*ServerStatus   `json:"servers"`
	Clusters        []*ClusterStatus  `json:"clusters"`
	Filters         []*FilterStatus   `json:"filters,omitempty"`
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
	as := []string{}
	for _, a := range s.Auths {
		as = append(as, a.String())
	}
	return fmt.Sprintf("%s/auths:[%s]/listeners:[%s]/servers:[%s]/clusters:[%s]/allocs:%d/status=%s",
		s.Admin.String(), strings.Join(as, ","), strings.Join(ls, ","), strings.Join(ss, ","),
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
	as := []string{}
	for _, a := range s.Auths {
		as = append(as, a.String())
	}
	return fmt.Sprintf("%s\n\tauths:%s\n\tlisteners:%s\n\tservers:%s\n\tclusters:%s\n\t"+
		"allocs:%d/status=%s", s.Admin.String(), strings.Join(as, ","), strings.Join(ls, ","),
		strings.Join(ss, ","), strings.Join(cs, ","), s.AllocationCount, s.Status)
}
