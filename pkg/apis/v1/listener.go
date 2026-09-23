package v1

import (
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// ListenerConfig specifies a server socket on which STUN/TURN connections will be served.
type ListenerConfig struct {
	// Name of the listener.
	Name string `json:"name,omitempty"`
	// Protocol is the listener protocol. The TURN protocols ("TURN-UDP", "TURN-TCP",
	// "TURN-TLS", "TURN-DTLS") serve TURN over the given transport. The plain protocols
	// ("UDP", "TCP") relay every client flow to the static peer address in PeerAddr, and
	// "STDIN" relays a single flow between the stdin/stdout pair and PeerAddr. Default is
	// "TURN-UDP".
	Protocol string `json:"protocol,omitempty"`
	// PublicAddr is the Internet-facing public address for the listener (ignored by STUNner). It
	// must be a bare host: an IP literal (IPv4 or IPv6) or a DNS name, without brackets, port, or
	// scheme.
	PublicAddr string `json:"public_address,omitempty"`
	// PublicPort is the Internet-facing public port for the listener (ignored by STUNner).
	PublicPort int `json:"public_port,omitempty"`
	// PublicAddrs is the list of Internet-facing public addresses for the listener. Ignored by
	// stunnerd, used only to build ICE server configurations for dual-stack servers.
	PublicAddrs []string `json:"public_addresses,omitempty"`
	// Addr is the IP address for the listener. Default is "0.0.0.0".
	Addr string `json:"address,omitempty"`
	// Addrs is the list of IP addresses advertised to clients as the relayed transport
	// address. On a dual-stack deployment it holds one address per family, single-stack holds
	// a single-element list. Set to status.podIPs by the operator.
	Addrs []string `json:"addresses,omitempty"`
	// Port is the port for the listener. Default is the standard TURN port (3478).
	Port int `json:"port,omitempty"`
	// Cert is the base64-encoded TLS cert.
	Cert string `json:"cert,omitempty"`
	// Key is the base64-encoded TLS key.
	Key string `json:"key,omitempty"`
	// PQCMode is the post-quantum encryption policy of a TURN-TLS listener: "default",
	// "preferred" or "enforced".
	PQCMode string `json:"pqc_mode,omitempty"`
	// PeerAddr is the peer to which a plain UDP/TCP or STDIN listener relays every client
	// flow, as "<udp|tcp>://host:port". Ignored for TURN listeners.
	PeerAddr string `json:"peer_addr,omitempty"`
	// Routes specifies the list of Routes allowed via a listener.
	Routes []string `json:"routes,omitempty"`
}

// Validate checks a configuration and injects defaults.
func (req *ListenerConfig) Validate() error {
	if req.Name == "" {
		return fmt.Errorf("missing name in listener configuration: %s", req.String())
	}

	// Normalize
	if req.Protocol == "" {
		req.Protocol = DefaultProtocol
	}
	proto, err := NewListenerProtocol(req.Protocol)
	if err != nil {
		return err
	}
	req.Protocol = proto.String()

	// STDIN listeners serve the single pre-accepted stdin/stdout flow: there is no
	// listener socket, so no address, port, or TLS credentials.
	if proto == ProtocolSTDIN {
		if req.Addr != "" || req.Port != 0 {
			return fmt.Errorf("address/port set for %s listener: %s", proto.String(),
				req.String())
		}
		if req.Cert != "" || req.Key != "" {
			return fmt.Errorf("TLS cert/key set for %s listener: %s", proto.String(),
				req.String())
		}
	} else {
		if req.Addr == "" {
			req.Addr = "0.0.0.0"
		}

		if req.Port == 0 {
			req.Port = DefaultPort
		}
		if req.Port <= 0 || req.Port > 65535 {
			return fmt.Errorf("invalid port: %d", req.Port)
		}
	}

	if proto == ListenerProtocolTURNTLS || proto == ListenerProtocolTURNDTLS ||
		proto == ListenerProtocolTLS || proto == ListenerProtocolDTLS {
		if req.Cert == "" {
			return fmt.Errorf("empty TLS cert for %s listener", proto.String())
		}
		if req.Key == "" {
			return fmt.Errorf("empty TLS key for %s listener", proto.String())
		}
	}

	// The PQC mode is a TLS-only policy: DTLS has no post-quantum key exchange to speak of.
	pqcMode, err := NewPQCMode(req.PQCMode)
	if err != nil {
		return err
	}
	if pqcMode != PQCModeDefault && proto != ListenerProtocolTURNTLS {
		return fmt.Errorf("PQC mode %q set for %s listener: only TURN-TLS listeners take a PQC mode",
			pqcMode.String(), proto.String())
	}
	req.PQCMode = ""
	if pqcMode != PQCModeDefault {
		req.PQCMode = pqcMode.String()
	}

	// Plain listeners relay every client flow to a single static peer: raw flows carry no
	// in-band peer address. TURN-* listeners take their peer addresses in-band, so PeerAddr
	// is ignored there.
	if proto == ProtocolUDP || proto == ProtocolTCP || proto == ProtocolSTDIN {
		if req.PeerAddr == "" {
			return fmt.Errorf("missing peer address for %s listener", proto.String())
		}
		if i := strings.Index(req.PeerAddr, "://"); i >= 0 {
			if s := strings.ToLower(req.PeerAddr[:i]); s != "udp" && s != "tcp" {
				return fmt.Errorf("invalid peer transport %q in peer address %q",
					s, req.PeerAddr)
			}
		}
		_, hostport := req.PeerEndpoint()
		host, port, err := net.SplitHostPort(hostport)
		if err != nil || host == "" {
			return fmt.Errorf("invalid peer address %q: expecting "+
				"[udp://|tcp://]host:port", req.PeerAddr)
		}
		if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("invalid port in peer address %q", req.PeerAddr)
		}
	}

	if req.PublicAddrs == nil {
		req.PublicAddrs = []string{}
	}

	// Addrs may arrive comma-separated (the k8s downward API renders status.podIPs as
	// "ip1,ip2"): normalize to one entry per address, dropping blanks.
	normalizedAddrs := make([]string, 0, len(req.Addrs))
	for _, a := range req.Addrs {
		for _, part := range strings.Split(a, ",") {
			if part = strings.TrimSpace(part); part != "" {
				normalizedAddrs = append(normalizedAddrs, part)
			}
		}
	}
	req.Addrs = normalizedAddrs

	if req.Routes == nil {
		req.Routes = []string{}
	}

	sort.Strings(req.Routes)
	return nil
}

// PeerEndpoint returns the transport protocol and the "host:port" endpoint of the peer of a
// plain listener; a bare peer address defaults to a UDP peer.
func (req *ListenerConfig) PeerEndpoint() (Protocol, string) {
	addr := req.PeerAddr
	proto := ProtocolUDP
	switch lower := strings.ToLower(addr); {
	case strings.HasPrefix(lower, "udp://"):
		addr = addr[len("udp://"):]
	case strings.HasPrefix(lower, "tcp://"):
		proto = ProtocolTCP
		addr = addr[len("tcp://"):]
	}
	return proto, addr
}

// Name returns the name of the object to be configured.
func (req *ListenerConfig) ConfigName() string {
	return req.Name
}

// DeepEqual compares two configurations. Routes must be sorted in both configs!
func (req *ListenerConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// DeepCopyInto copies a configuration.
func (req *ListenerConfig) DeepCopyInto(dst Config) {
	ret := dst.(*ListenerConfig)
	*ret = *req
	ret.Routes = make([]string, len(req.Routes))
	copy(ret.Routes, req.Routes)
	ret.PublicAddrs = make([]string, len(req.PublicAddrs))
	copy(ret.PublicAddrs, req.PublicAddrs)
	ret.Addrs = make([]string, len(req.Addrs))
	copy(ret.Addrs, req.Addrs)
}

// String stringifies the configuration.
func (req *ListenerConfig) String() string {
	status := []string{}

	n := "-"
	if req.Name != "" {
		n = req.Name
	}

	addr := "0.0.0.0"
	if req.Addr != "" && req.Addr != "$STUNNER_ADDR" {
		addr = req.Addr
	}

	status = append(status, fmt.Sprintf("turn://%s?transport=%s", net.JoinHostPort(addr, strconv.Itoa(req.Port)), req.Protocol))

	a, p := "-", "-"
	if req.PublicAddr != "" {
		a = req.PublicAddr
	}
	if req.PublicPort != 0 {
		p = fmt.Sprintf("%d", req.PublicPort)
	}
	status = append(status, fmt.Sprintf("public=%s", net.JoinHostPort(a, p)))

	if req.PeerAddr != "" {
		status = append(status, fmt.Sprintf("peer=%s", req.PeerAddr))
	}

	c, k := "-", "-"
	if req.Cert != "" {
		c = "<SECRET>"
	}
	if req.Key != "" {
		k = "<SECRET>"
	}
	status = append(status, fmt.Sprintf("cert/key=%s/%s", c, k))
	if req.PQCMode != "" {
		status = append(status, fmt.Sprintf("pqc=%s", req.PQCMode))
	}
	status = append(status, fmt.Sprintf("routes=[%s]", strings.Join(req.Routes, ",")))

	return fmt.Sprintf("%q:{%s}", n, strings.Join(status, ","))
}

type ListenerStatus struct {
	*ListenerConfig
	Stats OffloadDirStat `json:"stats"`
}

// String stringifies the configuration.
func (req *ListenerStatus) String() string {
	status := req.ListenerConfig.String()
	status += fmt.Sprintf(",offload(rx/tx): %d/%d pkts %d/%d bytes",
		req.Stats.Rx.Pkts, req.Stats.Tx.Pkts, req.Stats.Rx.Bytes, req.Stats.Tx.Bytes)
	return status
}
