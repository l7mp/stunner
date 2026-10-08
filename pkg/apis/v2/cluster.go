package v2

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// TunnelConfig is the tunnel a cluster reaches its peers through: an upstream TURN server.
type TunnelConfig struct {
	// URL is the upstream TURN server as an ICE server URI (RFC 7065), for example
	// "turn:host:port?transport=tcp" or "turns:host:port?transport=udp" (DTLS). Mandatory.
	URL string `json:"url"`
	// Auth names the authentication policy whose credentials the tunnel presents to the TURN
	// server, static or ephemeral; none for a server that authenticates nobody.
	Auth string `json:"auth,omitempty"`
	// Insecure skips the verification of the TURN server's certificate over TLS and DTLS.
	Insecure bool `json:"insecure,omitempty"`
	// SNI overrides the server name the TURN server's certificate is verified against.
	SNI string `json:"sni,omitempty"`
}

// Validate checks a tunnel configuration and injects defaults.
func (t *TunnelConfig) Validate() error {
	u, err := ParseURI(t.URL)
	if err != nil {
		return fmt.Errorf("invalid tunnel URL: %w", err)
	}
	if !u.Protocol.IsTURN() {
		return fmt.Errorf("invalid tunnel URL %q: not a TURN URL", t.URL)
	}
	return nil
}

// DeepCopy copies a tunnel configuration.
func (t *TunnelConfig) DeepCopy() *TunnelConfig {
	ret := *t
	return &ret
}

// ClusterConfig is a named set of peer endpoints and the transport that reaches them.
type ClusterConfig struct {
	// Name of the cluster.
	Name string `json:"name"`
	// Type is STATIC (addresses) or STRICT_DNS (domains). Default is STATIC.
	Type string `json:"type,omitempty"`
	// Endpoints are addresses or domains with an optional port, such as "10.1.0.7:5000" (see
	// ParseEndpoint). They never filter.
	Endpoints []string `json:"endpoints,omitempty"`
	// RoutingPolicy is ORIGINAL_DST (the destination the client names, the default) or ROUND_ROBIN.
	RoutingPolicy string `json:"routing_policy,omitempty"`
	// Protocol is the network the peers are reached over: UDP, TCP, or FIFO. Mandatory.
	Protocol string `json:"protocol"`
	// Tunnel is the upstream TURN server the peers are reached through, nil for direct.
	Tunnel *TunnelConfig `json:"tunnel,omitempty"`
	// Addrs are the relay addresses, at most one per family; the operator sets status.podIPs
	// and FIFOs use custom strings.
	Addrs []string `json:"addresses,omitempty"`
	// Filters name the filters checking the cluster's peers, at dial time and on every relayed packet.
	Filters []string `json:"filters,omitempty"`
}

// Validate checks a configuration and injects defaults.
func (req *ClusterConfig) Validate() error {
	if req.Name == "" {
		return fmt.Errorf("missing name in cluster configuration: %s", req.String())
	}
	if req.Type == "" {
		req.Type = DefaultClusterType
	}
	t, err := NewClusterType(req.Type)
	if err != nil {
		return err
	}
	req.Type = t.String()

	if req.Endpoints == nil {
		req.Endpoints = []string{}
	}
	for _, ep := range req.Endpoints {
		if _, err := ParseEndpoint(ep); err != nil {
			return err
		}
	}
	sort.Strings(req.Endpoints)

	if req.RoutingPolicy == "" {
		req.RoutingPolicy = DefaultRoutingPolicy
	}
	rp, err := NewRoutingPolicy(req.RoutingPolicy)
	if err != nil {
		return err
	}
	req.RoutingPolicy = rp.String()

	p, err := NewClusterProtocol(req.Protocol)
	if err != nil {
		return fmt.Errorf("%w in cluster configuration: %s", err, req.String())
	}
	req.Protocol = p.String()

	if req.Tunnel != nil {
		if err := req.Tunnel.Validate(); err != nil {
			return fmt.Errorf("%w in cluster configuration: %s", err, req.String())
		}
	}

	if req.Filters == nil {
		req.Filters = []string{}
	}

	if p == ProtocolFIFO && (len(req.Addrs) == 0 || req.Addrs[0] == "") {
		return fmt.Errorf("FIFO cluster %q names no fifo in its address", req.Name)
	}

	// Addrs may arrive comma-separated (the k8s downward API renders status.podIPs as
	// "ip1,ip2"): normalize to one entry per address, dropping blanks.
	addrs := make([]string, 0, len(req.Addrs))
	for _, a := range req.Addrs {
		for _, part := range strings.Split(a, ",") {
			if part = strings.TrimSpace(part); part != "" {
				addrs = append(addrs, part)
			}
		}
	}
	req.Addrs = addrs

	return nil
}

// ConfigName returns the name of the object to be configured.
func (req *ClusterConfig) ConfigName() string {
	return req.Name
}

// DeepEqual compares two configurations.
func (req *ClusterConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// DeepCopyInto copies a configuration.
func (req *ClusterConfig) DeepCopyInto(dst Config) {
	ret := dst.(*ClusterConfig)
	*ret = *req
	ret.Endpoints = slices.Clone(req.Endpoints)
	ret.Addrs = slices.Clone(req.Addrs)
	ret.Filters = slices.Clone(req.Filters)
	if req.Tunnel != nil {
		ret.Tunnel = req.Tunnel.DeepCopy()
	}
}

// String stringifies the configuration.
func (req *ClusterConfig) String() string {
	n := "-"
	if req.Name != "" {
		n = req.Name
	}
	status := []string{fmt.Sprintf("type=%q", req.Type),
		fmt.Sprintf("endpoints=[%s]", strings.Join(req.Endpoints, ",")),
		fmt.Sprintf("routing_policy=%q", req.RoutingPolicy), fmt.Sprintf("protocol=%q", req.Protocol)}
	if req.Tunnel != nil {
		status = append(status, fmt.Sprintf("tunnel=%q", req.Tunnel.URL))
		if req.Tunnel.SNI != "" {
			status = append(status, fmt.Sprintf("tunnel-sni=%q", req.Tunnel.SNI))
		}
		if req.Tunnel.Auth != "" {
			status = append(status, fmt.Sprintf("tunnel-auth=%q", req.Tunnel.Auth))
		}
	}
	if len(req.Addrs) > 0 {
		status = append(status, fmt.Sprintf("addresses=[%s]", strings.Join(req.Addrs, ",")))
	}
	if len(req.Filters) > 0 {
		status = append(status, fmt.Sprintf("filters=[%s]", strings.Join(req.Filters, ",")))
	}
	return fmt.Sprintf("%q:{%s}", n, strings.Join(status, ","))
}

// ClusterStatus is the status of a cluster: its config and the offload counters of the peer-side
// traffic it carries.
type ClusterStatus struct {
	*ClusterConfig
	Stats OffloadDirStat `json:"stats"`
}

// String stringifies the status.
func (req *ClusterStatus) String() string {
	return fmt.Sprintf("%s,offload(rx/tx): %d/%d pkts %d/%d bytes", req.ClusterConfig.String(),
		req.Stats.Rx.Pkts, req.Stats.Tx.Pkts, req.Stats.Rx.Bytes, req.Stats.Tx.Bytes)
}
