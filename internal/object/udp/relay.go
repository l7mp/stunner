package udp

import (
	"net"
	"strconv"

	"github.com/pion/transport/v4"
	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/internal/resolver"
	objruntime "github.com/l7mp/stunner/internal/runtime"
	"github.com/l7mp/stunner/internal/util"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Relay is a UDP-capable cluster relay implementation.
type Relay struct {
	clusterName string
	typ         stnrv1.ClusterType
	endpoints   []*util.Endpoint
	domains     []string
	resolver    resolver.DnsResolver
	net         transport.Net
}

// NewRelay creates a UDP relay for a cluster.
func NewRelay(cluster string, typ stnrv1.ClusterType, endpoints []*util.Endpoint, domains []string, dns resolver.DnsResolver, vnet transport.Net) *Relay {
	return &Relay{
		clusterName: cluster,
		typ:         typ,
		endpoints:   append([]*util.Endpoint(nil), endpoints...),
		domains:     append([]string(nil), domains...),
		resolver:    dns,
		net:         vnet,
	}
}

// ClusterName returns the owning cluster name.
func (r *Relay) ClusterName() string { return r.clusterName }

// Protocol returns the relay protocol.
func (r *Relay) Protocol() stnrv1.ClusterProtocol { return stnrv1.ClusterProtocolUDP }

// Validate verifies relay runtime configuration.
func (r *Relay) Validate() error { return nil }

// AllocateListener is unsupported for UDP relays.
func (r *Relay) AllocateListener(turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	return nil, nil, objruntime.NewUnsupportedNetOpError("listen", "udp")
}

// AllocateConn is unsupported for UDP relays.
func (r *Relay) AllocateConn(turn.AllocateConnConfig) (net.Conn, error) {
	return nil, objruntime.NewUnsupportedNetOpError("dial", "udp")
}

// AllocatePacketConn opens a UDP packet connection for a TURN allocation.
func (r *Relay) AllocatePacketConn(conf turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	requestedPort := conf.RequestedPort
	if requestedPort <= 1 || requestedPort > 2<<16-1 {
		requestedPort = 0
	}

	conn, err := r.net.ListenPacket(conf.Network, net.JoinHostPort("0.0.0.0", strconv.Itoa(requestedPort)))
	if err != nil {
		return nil, nil, err
	}

	return conn, conn.LocalAddr(), nil
}

// Start registers strict-DNS domains with the resolver.
func (r *Relay) Start() error {
	if r.typ == stnrv1.ClusterTypeStrictDNS {
		for _, d := range r.domains {
			if err := r.resolver.Register(d); err != nil {
				return err
			}
		}
	}
	return nil
}

// Close unregisters strict-DNS domains from the resolver.
func (r *Relay) Close() error {
	if r.typ == stnrv1.ClusterTypeStrictDNS {
		for _, d := range r.domains {
			r.resolver.Unregister(d)
		}
	}
	return nil
}

// Match reports whether a peer endpoint is admitted by the cluster policy.
func (r *Relay) Match(peer net.IP, port int) bool {
	switch r.typ {
	case stnrv1.ClusterTypeStatic:
		for _, e := range r.endpoints {
			if e.Match(peer, port) {
				return true
			}
		}
	case stnrv1.ClusterTypeStrictDNS:
		for _, d := range r.domains {
			hosts, err := r.resolver.Lookup(d)
			if err != nil {
				continue
			}
			for _, h := range hosts {
				if h.Equal(peer) {
					return true
				}
			}
		}
	}

	return false
}
