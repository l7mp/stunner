// Package relay builds the relayed transports of a listener context: the legs that carry a
// client's traffic towards its peers. A listener routing to a TURN-* protocol cluster relays
// through the upstream TURN server the cluster names; every other listener relays to the peers
// directly. Both engines draw their legs from here, the TURN server through its pion allocator
// adapter and the flow engine directly, and every leg comes back classified and accounted:
// wire facts from the transport, the class from the Router, the counters from telemetry.
package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/pion/transport/v5/reuseport"
	"github.com/pion/transport/v5/stdnet"

	"github.com/l7mp/stunner/v2/internal/netconn/account"
	"github.com/l7mp/stunner/v2/internal/netconn/classify"
	"github.com/l7mp/stunner/v2/internal/netconn/wire"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	"github.com/l7mp/stunner/v2/pkg/utils/turnclient"
)

var errNilConn = errors.New("cannot allocate relay connection")

// TURNCluster returns the TURN-* protocol cluster the listener routes to, if any: the upstream
// TURN server its relay legs tunnel through.
func TURNCluster(rt *runtime.Runtime, listener string) (runtime.Cluster, bool) {
	return rt.Router.Route(listener, func(c runtime.Cluster) bool { return c.Protocol().IsTURN() })
}

// Relay builds the relay legs of one listener context.
type Relay struct {
	listener string
	rt       *runtime.Runtime
	// addrs are the candidate relay addresses advertised to the client per family; the one
	// matching the leg's family is used.
	addrs    []net.IP
	fallback net.IP
}

// New creates the relay of a listener context.
func New(listener string, rt *runtime.Runtime) *Relay {
	conf := rt.GetConfig(runtime.TypeListener, listener).(*stnrv1.ListenerConfig)
	addrs, fallback := parseRelayAddrs(conf)
	if fallback == nil && len(addrs) == 0 {
		panic(fmt.Sprintf("relay: no valid relay address for %q: address=%q addresses=%v",
			listener, conf.Addr, conf.Addrs))
	}
	return &Relay{listener: listener, rt: rt, addrs: addrs, fallback: fallback}
}

// parseRelayAddrs derives a listener's candidate relay addresses (Addrs, at most one per family) and
// the Addr fallback. "localhost" is not an IP literal, so it is treated as the dual-stack loopback so
// relayIPFor can still match either family. An empty Addr (STDIN listeners carry no listener
// address) falls back to the unspecified address: their relayed address is never advertised.
func parseRelayAddrs(conf *stnrv1.ListenerConfig) (addrs []net.IP, fallback net.IP) {
	fallback = net.ParseIP(conf.Addr)
	for _, a := range conf.Addrs {
		if ip := net.ParseIP(a); ip != nil {
			addrs = append(addrs, ip)
		}
	}
	if fallback == nil && len(addrs) == 0 {
		switch conf.Addr {
		case "localhost":
			addrs = []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
		case "":
			fallback = net.IPv4zero
		}
	}
	return addrs, fallback
}

// relayIPFor returns the relay address to advertise for a leg of the given network's family
// ("udp4"/"udp6"/"tcp4"/"tcp6"): the first configured Addrs entry of that family, else the listener
// Addr fallback.
func (r *Relay) relayIPFor(network string) net.IP {
	wantV6 := strings.HasSuffix(network, "6")
	for _, ip := range r.addrs {
		if (ip.To4() == nil) == wantV6 {
			return ip
		}
	}
	return r.fallback
}

// classifyPeer is the Router-backed classifier of the direct legs: a peer's class is the cluster
// admitting it, resolved per datagram on a shared relay socket.
func (r *Relay) classifyPeer(peer net.Addr) (string, bool) {
	var (
		proto stnrv1.ClusterProtocol
		ip    net.IP
		port  int
	)
	switch a := peer.(type) {
	case *net.UDPAddr:
		proto, ip, port = stnrv1.ClusterProtocolUDP, a.IP, a.Port
	case *net.TCPAddr:
		proto, ip, port = stnrv1.ClusterProtocolTCP, a.IP, a.Port
	default:
		return "", false
	}
	return r.rt.Router.RoutePeer(r.listener, proto, ip, port)
}

// PacketConn builds the datagram leg of a session and returns it with the relayed address to
// advertise. On a listener routing to a TURN-* cluster the leg is an allocation on the upstream
// TURN server, dialed synchronously with per-session credentials: closing the leg tears down
// the whole upstream session, the advertised address is the upstream server's relayed address,
// and the requested port is the upstream server's business. Otherwise a local relay socket is
// bound on the network ("udp4"/"udp6"), on port if nonzero.
func (r *Relay) PacketConn(network string, port int) (classify.PacketConn, net.Addr, error) {
	if c, ok := TURNCluster(r.rt, r.listener); ok {
		conf, err := turnclient.NewConfig(c.TURNServer(), c.Protocol())
		if err != nil {
			return nil, nil, err
		}
		conf.LoggerFactory = r.rt.Logger
		session, err := turnclient.Dialer{Config: conf}.ListenPacket(context.Background(), "udp", "")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to dial upstream TURN server for cluster %q: %w",
				c.Name(), err)
		}
		// admission is the upstream server's business: every peer is in the cluster's class
		leg := classify.NewPacketConn(session, classify.Const(c.Name()))
		return account.PacketConn(r.rt.Telemetry, leg, c.Name(), telemetry.ClusterType), session.LocalAddr(), nil
	}

	// Empty host is the unspecified address: on dual-stack hosts this binds a socket reachable
	// from both IPv4 and IPv6 peers, so relays work for IPv6-only peers (e.g. IPv6-only EKS
	// pods). Hardcoding "0.0.0.0" would be IPv4-only.
	sock, err := r.rt.Net.ListenPacket(network, net.JoinHostPort("", strconv.Itoa(sanitizePort(port))))
	if err != nil {
		return nil, nil, err
	}
	relayAddr, ok := sock.LocalAddr().(*net.UDPAddr)
	if !ok {
		_ = sock.Close()
		return nil, nil, errNilConn
	}
	advertised := *relayAddr
	advertised.IP = r.relayIPFor(network)

	leg := classify.NewPacketConn(wire.Direct(sock), r.classifyPeer)
	return account.PacketConn(r.rt.Telemetry, leg, r.listener, telemetry.ClusterType), &advertised, nil
}

// Conn opens the stream leg of an RFC 6062 Connect request towards peer. On a listener routing
// to a TURN-* cluster the connection is relayed through the upstream TURN server (an upstream
// TCP allocation, per-session credentials); otherwise the peer is classified once and dialed
// directly from local, the session's relayed transport address, which is shared with the
// session's listener and hence bound with the reuse socket options.
func (r *Relay) Conn(local, peer net.Addr) (classify.Conn, error) {
	if c, ok := TURNCluster(r.rt, r.listener); ok {
		conf, err := turnclient.NewConfig(c.TURNServer(), c.Protocol())
		if err != nil {
			return nil, err
		}
		conf.LoggerFactory = r.rt.Logger
		conn, err := turnclient.Dialer{Config: conf}.DialContext(context.Background(), "tcp",
			peer.String())
		if err != nil {
			return nil, err
		}
		return account.Conn(r.rt.Telemetry, classify.NewConn(conn, c.Name()), telemetry.ClusterType), nil
	}

	class, ok := r.classifyPeer(peer)
	if !ok {
		return nil, classify.ErrProhibited
	}
	remote, ok := peer.(*net.TCPAddr)
	if !ok {
		return nil, classify.ErrProhibited
	}
	network := "tcp4"
	if remote.IP.To4() == nil {
		network = "tcp6"
	}

	d := r.rt.Net.CreateDialer(&net.Dialer{LocalAddr: local, Control: reuseport.Control})
	conn, err := d.Dial(network, remote.String())
	if err != nil {
		return nil, err
	}
	return account.Conn(r.rt.Telemetry, classify.NewConn(conn, class), telemetry.ClusterType), nil
}

// Listener binds the relayed transport address of an RFC 6062 TCP session on the network
// ("tcp4"/"tcp6"), on port if nonzero, classifying inbound connections at accept time. On a
// listener routing to a TURN-* cluster the session is accepted only for a TURN-TCP or TURN-TLS
// cluster (RFC 6062 needs a TCP/TLS control connection to the upstream) and the bound listener
// is the advertised relayed address only: inbound connections are rejected, the relaying is
// outbound, client-speaks-first. Otherwise it fails early if the listener routes to no cluster of
// the requested protocol. The relayed address is shared with the session's outgoing dials, so it
// is bound with the reuse socket options.
func (r *Relay) Listener(network string, port int) (classify.Listener, net.Addr, error) {
	if c, ok := TURNCluster(r.rt, r.listener); ok {
		if p := c.Protocol(); p != stnrv1.ProtocolTURNTCP && p != stnrv1.ProtocolTURNTLS {
			return nil, nil, classify.ErrProhibited
		}
	} else if _, ok := r.rt.Router.Route(r.listener, func(c runtime.Cluster) bool {
		return c.Protocol() == stnrv1.ClusterProtocolTCP
	}); !ok {
		// RFC 6062 sessions are TCP: fail early on a listener with no TCP cluster
		return nil, nil, classify.ErrProhibited
	}

	l, err := listenTCP(r.rt, network, sanitizePort(port))
	if err != nil {
		return nil, nil, err
	}
	leg := classify.NewListener(l, r.classifyPeer,
		r.rt.Logger.NewLogger(fmt.Sprintf("relay-%s", r.listener)))
	top := account.Listener(r.rt.Telemetry, leg, telemetry.ClusterType)

	if tcpAddr, ok := l.Addr().(*net.TCPAddr); ok {
		advertised := *tcpAddr
		advertised.IP = r.relayIPFor(network)
		return top, &advertised, nil
	}
	return top, l.Addr(), nil
}

// listenTCP binds the relayed TCP transport address with the reuse socket options so outgoing dials
// can share it. transport.Net has no listen-config hook, so the kernel path uses net.ListenConfig
// directly; only vnet-backed tests go through rt.Net.
func listenTCP(rt *runtime.Runtime, network string, port int) (net.Listener, error) {
	wildcard := net.IPv4zero
	if strings.HasSuffix(network, "6") {
		wildcard = net.IPv6unspecified
	}
	laddr := &net.TCPAddr{IP: wildcard, Port: port}

	if _, ok := rt.Net.(*stdnet.Net); ok {
		lc := net.ListenConfig{Control: reuseport.Control}
		return lc.Listen(context.Background(), network, laddr.String())
	}
	return rt.Net.ListenTCP(network, laddr)
}

func sanitizePort(p int) int {
	if p <= 1 || p > 2<<16-1 {
		return 0
	}
	return p
}
