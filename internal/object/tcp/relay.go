package tcp

import (
	"net"

	"github.com/pion/transport/v4"
	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/internal/resolver"
	objruntime "github.com/l7mp/stunner/internal/runtime"
	"github.com/l7mp/stunner/internal/util"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Relay is a TCP-capable cluster relay implementation.
type Relay struct {
	clusterName string
	typ         stnrv1.ClusterType
	endpoints   []*util.Endpoint
	domains     []string
	resolver    resolver.DnsResolver
	net         transport.Net
}

// NewRelay creates a TCP relay for a cluster.
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
func (r *Relay) Protocol() stnrv1.ClusterProtocol { return stnrv1.ClusterProtocolTCP }

// Validate verifies relay runtime configuration.
func (r *Relay) Validate() error { return nil }

// AllocatePacketConn is unsupported for TCP relays.
func (r *Relay) AllocatePacketConn(turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	return nil, nil, objruntime.NewUnsupportedNetOpError("listen", "tcp")
}

// AllocateConn opens an outgoing TCP connection for a TURN allocation.
func (r *Relay) AllocateConn(conf turn.AllocateConnConfig) (net.Conn, error) {
	local := conf.LocalAddr.(*net.TCPAddr)
	remote := conf.RemoteAddr.(*net.TCPAddr)
	return r.net.DialTCP(conf.Network, local, remote)
}

// AllocateListener opens a TCP listener for a TURN allocation.
func (r *Relay) AllocateListener(conf turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	local := &net.TCPAddr{IP: net.IPv4zero, Port: conf.RequestedPort}
	l, err := r.net.ListenTCP(conf.Network, local)
	if err != nil {
		return nil, nil, err
	}
	return l, l.Addr(), nil
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
