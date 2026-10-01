package v1

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/l7mp/stunner/v2/internal/util"
)

// ClusterConfig specifies a set of upstream peers to which STUNner can open transport relay
// connections. There are two address resolution policies. In STATIC clusters the allowed peer IP
// addresses are explicitly listed in the endpoint list. In STRICT_DNS clusters the endpoints are
// assumed to be proper DNS domain names: STUNner will resolve each domain name in the background
// and admit a new connection only if the peer address matches one of the IP addresses returned by
// the DNS resolver for one of the endpoints. STRICT_DNS clusters are best used with headless
// Kubernetes services.
type ClusterConfig struct {
	// Name of the cluster. Name is mandatory.
	Name string `json:"name"`
	// Type specifies the cluster address resolution policy, either STATIC or
	// STRICT_DNS. Default is "STATIC".
	Type string `json:"type,omitempty"`
	// Protocol specifies the protocol to be used with the cluster: UDP (default) or TCP for
	// direct relay connections to the peers, or TURN-UDP, TURN-TCP, TURN-TLS or TURN-DTLS to
	// pass the traffic through an upstream TURN server reached over the given transport.
	// TURN-* protocol clusters relay through an upstream TURN server: with no endpoints they admit
	// every peer, leaving peer admission to the upstream server.
	Protocol string `json:"protocol,omitempty"`
	// Endpoints specifies the peers that can be reached via this cluster. Optional for TURN-*
	// protocol clusters.
	Endpoints []string `json:"endpoints,omitempty"`
	// TURNServer specifies the upstream TURN server for TURN-* protocol clusters. Mandatory
	// for TURN-* protocols, must be omitted otherwise.
	TURNServer *TURNServer `json:"turnServer,omitempty"`
}

// TURNServer describes an upstream TURN server that a TURN-* protocol cluster relays through.
type TURNServer struct {
	// Address is the IP address or DNS name of the TURN server. Address is mandatory.
	Address string `json:"address"`
	// Port is the port of the TURN server. Port is mandatory.
	Port int `json:"port"`
	// Auth specifies how to authenticate to the TURN server: "static" with a fixed
	// username/password pair, or "ephemeral" with a shared secret from which a time-limited
	// credential is generated per allocation, exactly as a STUNner listener authenticates its
	// own clients. Omit it for a server that takes no credentials.
	Auth *AuthConfig `json:"auth,omitempty"`
	// Insecure allows TLS certificate verification to be skipped for the TURN-TLS and
	// TURN-DTLS transports.
	Insecure bool `json:"insecure,omitempty"`
	// SNI overrides the server name used for TLS/DTLS certificate verification when it
	// differs from Address (e.g. when the server is addressed by an IP literal). Valid only
	// for clusters reaching the server over the TURN-TLS or TURN-DTLS transport.
	SNI string `json:"sni,omitempty"`
}

// HostPort returns the server's address in the "host:port" form the TURN client dials, bracketing
// IPv6 hosts. Note that Address holds the bare host, so it is not interchangeable with this.
func (s *TURNServer) HostPort() string {
	return net.JoinHostPort(s.Address, strconv.Itoa(s.Port))
}

// Validate checks a TURN server configuration and injects defaults. Messages are phrased to compose
// into the error of whatever config embeds the server.
func (s *TURNServer) Validate() error {
	if s.Address == "" {
		return errors.New("missing TURN server address")
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("invalid TURN server port %d", s.Port)
	}

	// no auth block means the server takes no credentials; a block that is present is
	// validated like any other, defaults and all
	if s.Auth != nil {
		if err := s.Auth.Validate(); err != nil {
			return fmt.Errorf("invalid TURN server auth: %w", err)
		}
	}

	return nil
}

// Validate checks a configuration and injects defaults.
func (req *ClusterConfig) Validate() error {
	if req.Name == "" {
		return fmt.Errorf("missing name in cluster configuration: %s", req.String())
	}

	// Normalize
	if req.Type == "" {
		req.Type = DefaultClusterType
	}
	t, err := NewClusterType(req.Type)
	if err != nil {
		return err
	}
	req.Type = t.String()

	// Normalize
	if req.Protocol == "" {
		req.Protocol = DefaultClusterProtocol
	}
	p, err := NewClusterProtocol(req.Protocol)
	if err != nil {
		return err
	}
	req.Protocol = p.String()

	// Do endpoints parse?
	if t == ClusterTypeStatic {
		for _, ep := range req.Endpoints {
			if _, err := util.ParseEndpoint(ep); err != nil {
				return err
			}
		}
	}

	if req.Endpoints == nil {
		req.Endpoints = []string{}
	}

	sort.Strings(req.Endpoints)

	// TURN-* protocol clusters require an upstream TURN server, direct clusters forbid it.
	if p.IsTURN() {
		if req.TURNServer == nil {
			return fmt.Errorf("missing TURN server in %q protocol cluster configuration: %s",
				req.Protocol, req.String())
		}
		if err := req.TURNServer.Validate(); err != nil {
			return fmt.Errorf("%w in cluster configuration: %s", err, req.String())
		}
		if req.TURNServer.SNI != "" && p != ProtocolTURNTLS && p != ProtocolTURNDTLS {
			return fmt.Errorf("TURN server SNI set for the non-TLS %q protocol in "+
				"cluster configuration: %s", req.Protocol, req.String())
		}
	} else if req.TURNServer != nil {
		return fmt.Errorf("TURN server set in %q protocol cluster configuration: %s",
			req.Protocol, req.String())
	}

	return nil
}

// Name returns the name of the object to be configured.
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
	ret.Endpoints = make([]string, len(req.Endpoints))
	copy(ret.Endpoints, req.Endpoints)
	if req.TURNServer != nil {
		s := *req.TURNServer
		if req.TURNServer.Auth != nil {
			// the auth config owns a map: copy it instead of aliasing it
			a := AuthConfig{}
			req.TURNServer.Auth.DeepCopyInto(&a)
			s.Auth = &a
		}
		ret.TURNServer = &s
	}
}

// String stringifies the configuration.
func (req *ClusterConfig) String() string {
	status := []string{}

	n := "-"
	if req.Name != "" {
		n = req.Name
	}

	if req.Type != "" {
		status = append(status, fmt.Sprintf("type=%q", req.Type))
	}

	if req.Protocol != "" {
		status = append(status, fmt.Sprintf("protocol=%q", req.Protocol))
	}

	if req.TURNServer != nil {
		status = append(status, fmt.Sprintf("turn-server=%q", req.TURNServer.HostPort()))
		if req.TURNServer.SNI != "" {
			status = append(status, fmt.Sprintf("turn-server-sni=%q", req.TURNServer.SNI))
		}
		if req.TURNServer.Auth != nil {
			// AuthConfig.String redacts the credentials for us
			status = append(status, "turn-server-auth="+req.TURNServer.Auth.String())
		}
	}

	status = append(status, fmt.Sprintf("endpoints=[%s]",
		strings.Join(req.Endpoints, ",")))

	return fmt.Sprintf("%q:{%s}", n, strings.Join(status, ","))
}

type ClusterStatus struct {
	*ClusterConfig
	Stats OffloadDirStat `json:"stats"`
}

// String stringifies the configuration.
func (req *ClusterStatus) String() string {
	status := req.ClusterConfig.String()
	status += fmt.Sprintf(",offload(rx/tx): %d/%d pkts %d/%d bytes",
		req.Stats.Rx.Pkts, req.Stats.Tx.Pkts, req.Stats.Rx.Bytes, req.Stats.Tx.Bytes)
	return status
}
