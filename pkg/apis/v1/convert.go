package v1

import (
	"fmt"
	"net"
	"regexp"
	"slices"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// portRangeMatcher matches a v1 endpoint with a port range: "<IP>[/<prefix>]:<min-max>".
var portRangeMatcher = regexp.MustCompile("^(.*):<([0-9]+)-([0-9]+)>$")

// ConvertToV2 converts a v1 config into a v2 config: every v1 listener with ConvertListener,
// every v1 cluster with ConvertCluster.
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
		Auth: stnrv2.AuthConfig(c.Auth),
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
		cluster, err := ConvertCluster(cl, c.Listeners)
		if err != nil {
			return nil, err
		}
		ret.Clusters = append(ret.Clusters, cluster)
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
		Name:     l.Name,
		Type:     serverType.String(),
		Clusters: l.Routes,
	}
}

// ConvertCluster converts a v1 cluster, given the v1 listeners of its config. The cluster gets the
// addresses of the listeners routing to it as relay addresses, the first IP per family in listener
// order and any non-IP address (an environment variable or a placeholder) as it is, and the
// routing policy of the first of them: FILTER behind a TURN listener, ROUND_ROBIN behind any other
// (a cluster behind both kinds keeps the first, and the other server skips it). A TURN-* cluster
// becomes a UDP cluster tunnelled through the cluster's TURN server, and admits every peer, so its
// endpoints convert to the catch-all prefixes.
func ConvertCluster(cl ClusterConfig, listeners []ListenerConfig) (stnrv2.ClusterConfig, error) {
	proto, _ := NewClusterProtocol(cl.Protocol)

	// v1 writes a port range as "<IP>:<min-max>", v2 as "<IP>:<min>-<max>" (a single port as
	// "<IP>:<port>"); the full range 1-65535 is any port, which v2 writes as no port at all
	endpoints := []string{}
	for _, ep := range cl.Endpoints {
		if m := portRangeMatcher.FindStringSubmatch(ep); len(m) == 4 {
			switch {
			case m[2] == "1" && m[3] == "65535":
				ep = m[1]
			case m[2] == m[3]:
				ep = net.JoinHostPort(m[1], m[2])
			default:
				ep = net.JoinHostPort(m[1], m[2]+"-"+m[3])
			}
		}
		if _, err := stnrv2.ParseEndpoint(ep); err != nil {
			return stnrv2.ClusterConfig{}, fmt.Errorf("cluster %q: cannot convert endpoint: %w",
				cl.Name, err)
		}
		endpoints = append(endpoints, ep)
	}
	if proto.IsTURN() && len(endpoints) == 0 {
		endpoints = []string{"0.0.0.0/0", "::/0"}
	}

	cluster := stnrv2.ClusterConfig{
		Name:      cl.Name,
		Type:      cl.Type,
		Endpoints: endpoints,
		Protocol:  proto.String(),
		Addrs:     []string{},
	}

	// the relay addresses: the first IP of each family among the addresses of the listeners
	// routing to the cluster, and every other address once, as it is (an environment variable
	// such as $STUNNER_ADDR, resolved where the config is loaded, or the operator's node address
	// placeholder); an unspecified address advertises nothing
	v4, v6 := false, false
	for _, l := range listeners {
		if !slices.Contains(l.Routes, cl.Name) {
			continue
		}
		if cluster.RoutingPolicy == "" {
			cluster.RoutingPolicy = stnrv2.RoutingPolicyRoundRobin.String()
			if _, server := ConvertListener(l); server.Type == stnrv2.ServerTypeTURN.String() {
				cluster.RoutingPolicy = stnrv2.RoutingPolicyFilter.String()
			}
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

	if proto.IsTURN() {
		cluster.Protocol = stnrv2.ProtocolUDP.String()
		tunnelProto, _ := stnrv2.NewProtocol(proto.String())
		cluster.Tunnel = &stnrv2.TunnelConfig{}
		if s := cl.TURNServer; s != nil {
			cluster.Tunnel.URL = (&stnrv2.URI{Protocol: tunnelProto, Host: s.Address,
				Port: s.Port}).String()
			cluster.Tunnel.Insecure, cluster.Tunnel.SNI = s.Insecure, s.SNI
			if s.Auth != nil {
				auth := stnrv2.AuthConfig(*s.Auth)
				cluster.Tunnel.Auth = &auth
			}
		}
	}
	return cluster, nil
}
