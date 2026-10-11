package v1

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strconv"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// portRangeMatcher matches a v1 endpoint with a port range: "<IP>[/<prefix>]:<min-max>".
var portRangeMatcher = regexp.MustCompile("^(.*):<([0-9]+)-([0-9]+)>$")

// ConvertToV2 converts a v1 config into a v2 config. Every TURN server gets a permission filter
// allowing its clusters' endpoints (every address for TURN-*); a permission no rule allows is
// denied.
func ConvertToV2(req *StunnerConfig) (*stnrv2.StunnerConfig, error) {
	c := req.DeepCopy()
	if err := c.Validate(); err != nil {
		return nil, err
	}

	ret := &stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin: stnrv2.AdminConfig{
			Name:                c.Admin.Name,
			LogLevel:            c.Admin.LogLevel,
			MetricsEndpoint:     c.Admin.MetricsEndpoint,
			HealthCheckEndpoint: c.Admin.HealthCheckEndpoint,
			UserQuota:           c.Admin.UserQuota,
			OffloadEngine:       c.Admin.OffloadEngine,
			OffloadInterfaces:   c.Admin.OffloadInterfaces,
		},
		// the v1 auth is the default policy, the one a TURN server refers to unless told otherwise
		Auths: []stnrv2.AuthConfig{convertAuth(c.Auth, stnrv2.DefaultAuthName)},
	}
	if c.Admin.LicenseConfig != nil {
		l := stnrv2.LicenseConfig(*c.Admin.LicenseConfig)
		ret.Admin.LicenseConfig = &l
	}

	for _, l := range c.Listeners {
		listener, server := ConvertListener(l)
		ret.Listeners = append(ret.Listeners, listener)
		ret.Servers = append(ret.Servers, server)
	}
	for _, cl := range c.Clusters {
		cluster, filter, err := ConvertCluster(cl, c.Listeners)
		if err != nil {
			return nil, err
		}
		ret.Clusters = append(ret.Clusters, cluster)
		if filter != nil {
			ret.Filters = append(ret.Filters, *filter)
		}
		// the credentials of a TURN-* cluster's tunnel become a policy the tunnel names
		if cl.TURNServer != nil && cl.TURNServer.Auth != nil && cluster.Tunnel != nil {
			ret.Auths = append(ret.Auths, convertAuth(*cl.TURNServer.Auth, cluster.Tunnel.Auth))
		}
	}

	for i, l := range c.Listeners {
		server := &ret.Servers[i]
		if server.Type != stnrv2.ServerTypeTURN.String() {
			continue
		}
		filter := stnrv2.FilterConfig{Name: server.Name}
		allow := func(block *stnrv2.IPBlock) {
			filter.Rules = append(filter.Rules, stnrv2.FilterRule{
				Action: stnrv2.FilterActionAllow.String(), IPBlock: block})
		}
		for _, cl := range c.Clusters {
			if !slices.Contains(l.Routes, cl.Name) {
				continue
			}
			if p, _ := NewClusterProtocol(cl.Protocol); p.IsTURN() {
				allow(nil)
				continue
			}
			// a domain endpoint of a cluster an L4 listener routes to first is no address block
			for _, ep := range cl.Endpoints {
				if e, _ := convertEndpoint(ep); e.block.IsValid() {
					allow(&stnrv2.IPBlock{CIDR: e.block.String()})
				}
			}
		}
		server.TURN = &stnrv2.TURNServerConfig{PermissionFilters: []string{filter.Name}}
		ret.Filters = append(ret.Filters, filter)
	}

	if err := ret.Validate(); err != nil {
		return nil, fmt.Errorf("converted config is invalid: %w", err)
	}
	return ret, nil
}

// ConvertListener converts a v1 listener into a v2 listener and the server it feeds, both named
// as the listener: a TURN server for the TURN-* protocols, where the listener keeps the transport
// underneath (TURN-UDP becomes a UDP listener), an l4 server for the others. A v1 listener address
// is a relay address, not a bind address, so the v2 listener binds every address and the address
// moves to the clusters, see ConvertCluster.
func ConvertListener(l ListenerConfig) (stnrv2.ListenerConfig, stnrv2.ServerConfig) {
	proto, _ := NewListenerProtocol(l.Protocol)
	serverType := stnrv2.ServerTypeL4
	switch proto {
	case ProtocolTURNUDP:
		proto, serverType = ProtocolUDP, stnrv2.ServerTypeTURN
	case ProtocolTURNTCP:
		proto, serverType = ProtocolTCP, stnrv2.ServerTypeTURN
	case ProtocolTURNTLS:
		proto, serverType = ProtocolTLS, stnrv2.ServerTypeTURN
	case ProtocolTURNDTLS:
		proto, serverType = ProtocolDTLS, stnrv2.ServerTypeTURN
	}

	return stnrv2.ListenerConfig{
		Name:        l.Name,
		Protocol:    proto.String(),
		Servers:     []string{l.Name},
		Port:        l.Port,
		PublicAddr:  l.PublicAddr,
		PublicPort:  l.PublicPort,
		PublicAddrs: l.PublicAddrs,
		Cert:        l.Cert,
		Key:         l.Key,
		PQCMode:     l.PQCMode,
	}, stnrv2.ServerConfig{
		ServerSpec: stnrv2.ServerSpec{Name: l.Name, Type: serverType.String(), Clusters: l.Routes},
	}
}

// ConvertCluster converts a v1 cluster, given the v1 listeners of its config, and returns the
// filter of its endpoint ports, nil for none. The listeners routing to it give its relay addresses,
// and the first of them whether its endpoints are dial targets or TURN peers.
func ConvertCluster(cl ClusterConfig, listeners []ListenerConfig) (stnrv2.ClusterConfig, *stnrv2.FilterConfig, error) {
	proto, _ := NewClusterProtocol(cl.Protocol)

	cluster := stnrv2.ClusterConfig{
		Name:     cl.Name,
		Type:     cl.Type,
		Protocol: proto.String(),
		Addrs:    []string{},
	}

	// the relay addresses: the first IP of each family among the addresses of the listeners
	// routing to the cluster, and every other address once, as it is (an environment variable
	// such as $STUNNER_ADDR, resolved where the config is loaded, or the operator's node address
	// placeholder); an unspecified address advertises nothing
	// the first listener routing to the cluster tells whether its endpoints are dial targets (an
	// L4 listener) or TURN peers
	v4, v6 := false, false
	l4, routed := false, false
	for _, l := range listeners {
		if !slices.Contains(l.Routes, cl.Name) {
			continue
		}
		if !routed {
			_, server := ConvertListener(l)
			l4, routed = server.Type != stnrv2.ServerTypeTURN.String(), true
		}
		addrs := l.Addrs
		if len(addrs) == 0 && l.Addr != "" {
			addrs = []string{l.Addr}
		}
		for _, a := range addrs {
			ip := net.ParseIP(a)
			switch {
			case ip == nil && !slices.Contains(cluster.Addrs, a):
				cluster.Addrs = append(cluster.Addrs, a)
			case ip == nil || ip.IsUnspecified():
			case ip.To4() != nil && !v4:
				cluster.Addrs, v4 = append(cluster.Addrs, a), true
			case ip.To4() == nil && !v6:
				cluster.Addrs, v6 = append(cluster.Addrs, a), true
			}
		}
	}

	// prefixes live in the permission filter only; endpoint ports become a cluster filter
	filter := &stnrv2.FilterConfig{Name: cl.Name}
	restricted := false
	for _, ep := range cl.Endpoints {
		e, err := convertEndpoint(ep)
		if err != nil {
			return stnrv2.ClusterConfig{}, nil, fmt.Errorf("cluster %q: %w", cl.Name, err)
		}
		if l4 {
			// an L4 server dials an address or domain at a single port
			switch {
			case e.prefix:
			case e.port != 0 && e.port == e.endPort:
				cluster.Endpoints = append(cluster.Endpoints, net.JoinHostPort(e.host, strconv.Itoa(e.port)))
			default:
				cluster.Endpoints = append(cluster.Endpoints, e.host)
			}
			continue
		}
		if !e.block.IsValid() {
			return stnrv2.ClusterConfig{}, nil, fmt.Errorf("cluster %q: cannot convert domain "+
				"endpoint %q behind a TURN listener: TURN permissions are filters, and a filter "+
				"cannot name a domain", cl.Name, ep)
		}
		if !e.prefix {
			cluster.Endpoints = append(cluster.Endpoints, e.host)
		}
		rule := stnrv2.FilterRule{Action: stnrv2.FilterActionAllow.String(),
			IPBlock: &stnrv2.IPBlock{CIDR: e.block.String()}}
		if e.port != 0 {
			restricted = true
			rule.Ports = []stnrv2.FilterPort{{Port: e.port, EndPort: e.endPort}}
		}
		filter.Rules = append(filter.Rules, rule)
	}
	if !restricted {
		filter = nil
	} else {
		filter.Rules = append(filter.Rules, stnrv2.FilterRule{Action: stnrv2.FilterActionDeny.String()})
	}
	if filter != nil {
		cluster.Filters = []string{filter.Name}
	}

	if proto.IsTURN() {
		cluster.Protocol = stnrv2.ProtocolUDP.String()
		tunnelProto, _ := stnrv2.NewProtocol(proto.String())
		cluster.Tunnel = &stnrv2.TunnelConfig{}
		if s := cl.TURNServer; s != nil {
			cluster.Tunnel.URL = (&stnrv2.URI{Protocol: tunnelProto, Host: s.Address,
				Port: s.Port}).String()
			cluster.Tunnel.Insecure, cluster.Tunnel.SNI = s.Insecure, s.SNI
			if s.Auth != nil {
				// the policy itself is added by ConvertToV2, under this name
				cluster.Tunnel.Auth = cl.Name
			}
		}
	}
	return cluster, filter, nil
}

// convertAuth converts a v1 auth config into a v2 authentication policy of a name.
func convertAuth(a AuthConfig, name string) stnrv2.AuthConfig {
	return stnrv2.AuthConfig{Name: name, Type: a.Type, Realm: a.Realm, Credentials: a.Credentials,
		Lifetime: a.Lifetime}
}

// v1Endpoint is a parsed v1 endpoint: the v2 endpoint host (an address, a domain, or the prefix as
// written), the address block of an IP endpoint, and the port range, 0 for none.
type v1Endpoint struct {
	host          string
	block         netip.Prefix
	prefix        bool
	port, endPort int
}

// convertEndpoint parses a v1 endpoint, "<IP>[/<prefix>][:<min-max>]" or a domain.
func convertEndpoint(ep string) (v1Endpoint, error) {
	e := v1Endpoint{host: ep}
	if m := portRangeMatcher.FindStringSubmatch(ep); len(m) == 4 {
		e.host = m[1]
		e.port, _ = strconv.Atoi(m[2])
		e.endPort, _ = strconv.Atoi(m[3])
		if e.port < 1 || e.endPort < e.port || e.endPort > 65535 {
			return e, fmt.Errorf("invalid port range in endpoint %q", ep)
		}
		if e.port == 1 && e.endPort == 65535 {
			e.port, e.endPort = 0, 0
		}
	}
	if p, err := netip.ParsePrefix(e.host); err == nil {
		e.block = p.Masked()
		if p.IsSingleIP() {
			e.host = p.Addr().String()
		} else {
			e.prefix = true
		}
		return e, nil
	}
	if a, err := netip.ParseAddr(e.host); err == nil {
		e.block = netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen())
		e.host = a.Unmap().String()
		return e, nil
	}
	if _, err := stnrv2.ParseEndpoint(e.host); err != nil {
		return e, fmt.Errorf("cannot convert endpoint: %w", err)
	}
	return e, nil
}
