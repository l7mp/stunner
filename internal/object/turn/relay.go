package turn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/pion/transport/v5/stdnet"
	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/v2/internal/netutil"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	"github.com/l7mp/stunner/v2/pkg/upstream"
	"github.com/l7mp/stunner/v2/pkg/utils/turnclient"
)

var errNilConn = errors.New("cannot allocate relay connection")

// isTURNCluster matches the TURN-* protocol cluster a relaying listener routes to.
func isTURNCluster(c objruntime.Cluster) bool { return c.Protocol().IsTURN() }

// Relay adapts the dataplane relay transport to the pion RelayAddressGenerator interface for one
// listener context. A listener routing to a TURN-* protocol cluster relays through the upstream
// TURN server the cluster names; every other listener relays to the peers directly.
type Relay struct {
	listener string
	runtime  *objruntime.Runtime
	// addrs are the candidate relay addresses advertised to the client per family; the one
	// matching the allocation family is used.
	addrs    []net.IP
	fallback net.IP
}

// NewRelay creates a relay address generator for a listener context.
func NewRelay(listener string, rt *objruntime.Runtime) *Relay {
	conf := rt.GetConfig(objruntime.TypeListener, listener).(*stnrv1.ListenerConfig)
	addrs, fallback := parseRelayAddrs(conf)
	if fallback == nil && len(addrs) == 0 {
		panic(fmt.Sprintf("turn: no valid relay address for %q: address=%q addresses=%v",
			listener, conf.Addr, conf.Addrs))
	}
	return &Relay{listener: listener, runtime: rt, addrs: addrs, fallback: fallback}
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

// relayIPFor returns the relay address to advertise for an allocation of the given network's family
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

// Validate is called on server startup and confirms the RelayAddressGenerator is configured.
func (r *Relay) Validate() error { return nil }

// AllocatePacketConn allocates the UDP relayed transport of an allocation. On a listener routing
// to a TURN-* cluster the allocation is made on the upstream TURN server, dialed synchronously
// with per-allocation credentials: the returned conn is the upstream allocation itself (closing
// it tears down the whole session), the advertised relayed address is the upstream server's
// relayed address, and the requested port is the upstream server's business. Otherwise a local
// relay socket is bound, wrapped so every datagram is routed/admitted via the Router and
// accounted in telemetry.
func (r *Relay) AllocatePacketConn(conf turn.AllocateListenerConfig) (upstream.PacketConn, net.Addr, error) {
	if c, ok := r.runtime.Router.Route(r.listener, isTURNCluster); ok {
		turnConf, err := turnclient.NewConfig(c.TURNServer(), c.Protocol())
		if err != nil {
			return nil, nil, err
		}
		turnConf.LoggerFactory = r.runtime.Logger

		pc, err := turnclient.Dialer{Config: turnConf}.ListenPacket(context.Background(), "udp", "")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to dial upstream TURN server for cluster %q: %w",
				c.Name(), err)
		}
		// admission is the upstream server's business, so the conn only accounts
		wrapped := &turnPacketConn{
			PacketConn: netutil.NewPacketConn(pc, c.Name(), telemetry.ClusterType,
				r.runtime.Telemetry, nil,
				r.runtime.Logger.NewLogger(fmt.Sprintf("turn-relay-%s", r.listener))),
			up: pc,
		}
		return wrapped, pc.LocalAddr(), nil
	}

	// Empty host is the unspecified address: on dual-stack hosts this binds a socket reachable
	// from both IPv4 and IPv6 peers, so relays work for IPv6-only peers (e.g. IPv6-only EKS
	// pods). Hardcoding "0.0.0.0" would be IPv4-only.
	conn, err := r.runtime.Net.ListenPacket(conf.Network,
		net.JoinHostPort("", strconv.Itoa(sanitizePort(conf.RequestedPort))))
	if err != nil {
		return nil, nil, err
	}

	prc := &plainPacketConn{PacketConn: netutil.NewPacketConn(conn, r.listener,
		telemetry.ClusterType, r.runtime.Telemetry,
		func(addr net.Addr) (string, bool) { return routeRemote(r.runtime, r.listener, addr) },
		r.runtime.Logger.NewLogger(fmt.Sprintf("relay-%s", r.listener)))}

	relayAddr, ok := prc.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, nil, errNilConn
	}
	relayAddr.IP = r.relayIPFor(conf.Network)
	return prc, relayAddr, nil
}

// AllocateConn opens an outgoing connection for an RFC 6062 Connect request. On a listener
// routing to a TURN-* cluster the connection to the peer is relayed through the upstream TURN
// server (an upstream TCP allocation, per-allocation credentials); otherwise the peer is
// routed/admitted once and dialed directly from the allocation's relayed transport address
// (shared with its listener, hence the reuse socket options).
func (r *Relay) AllocateConn(conf turn.AllocateConnConfig) (upstream.Conn, error) {
	if c, ok := r.runtime.Router.Route(r.listener, isTURNCluster); ok {
		turnConf, err := turnclient.NewConfig(c.TURNServer(), c.Protocol())
		if err != nil {
			return nil, err
		}
		turnConf.LoggerFactory = r.runtime.Logger

		conn, err := turnclient.Dialer{Config: turnConf}.DialContext(context.Background(),
			"tcp", conf.RemoteAddr.String())
		if err != nil {
			return nil, err
		}
		// account the relayed connection under the cluster in telemetry
		return &turnConn{
			Conn: netutil.NewConn(conn, c.Name(), telemetry.ClusterType, r.runtime.Telemetry),
			up:   conn,
		}, nil
	}

	cluster, ok := routeRemote(r.runtime, r.listener, conf.RemoteAddr)
	if !ok {
		return nil, netutil.ErrPortProhibited
	}
	remote, ok := conf.RemoteAddr.(*net.TCPAddr)
	if !ok {
		return nil, netutil.ErrPortProhibited
	}
	network := "tcp4"
	if remote.IP.To4() == nil {
		network = "tcp6"
	}

	d := r.runtime.Net.CreateDialer(&net.Dialer{LocalAddr: conf.LocalAddr, Control: netutil.ReuseAddrControl})
	conn, err := d.Dial(network, remote.String())
	if err != nil {
		return nil, err
	}
	return &plainConn{Conn: netutil.NewConn(conn, cluster, telemetry.ClusterType,
		r.runtime.Telemetry)}, nil
}

// AllocateListener binds the relayed transport address of an RFC 6062 TCP allocation, admitting
// incoming connections at accept time. On a listener routing to a TURN-* cluster the TCP
// allocation is accepted only for a TURN-TCP or TURN-TLS cluster (RFC 6062 needs a TCP/TLS
// control connection to the upstream); the bound listener is the advertised relayed address
// (inbound connections are rejected, the relaying is outbound, client-speaks-first). Otherwise
// it fails early if the listener routes to no cluster of the requested protocol. The relayed
// address is shared with the allocation's outgoing dials, so it is bound with the reuse socket
// options.
func (r *Relay) AllocateListener(conf turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	if c, ok := r.runtime.Router.Route(r.listener, isTURNCluster); ok {
		if p := c.Protocol(); p != stnrv1.ProtocolTURNTCP && p != stnrv1.ProtocolTURNTLS {
			return nil, nil, netutil.ErrPortProhibited
		}
	} else if _, ok := r.runtime.Router.Route(r.listener, func(c objruntime.Cluster) bool {
		return c.Protocol() == stnrv1.ClusterProtocolTCP
	}); !ok {
		// RFC 6062 allocations are TCP: fail early on a listener with no TCP cluster
		return nil, nil, netutil.ErrPortProhibited
	}

	l, err := listenTCP(r.runtime, conf.Network, sanitizePort(conf.RequestedPort))
	if err != nil {
		return nil, nil, err
	}

	prl := netutil.NewListener(l, r.listener, telemetry.ClusterType, r.runtime.Telemetry,
		func(addr net.Addr) (string, bool) { return routeRemote(r.runtime, r.listener, addr) },
		r.runtime.Logger.NewLogger(fmt.Sprintf("relay-%s", r.listener)))

	if tcpAddr, ok := l.Addr().(*net.TCPAddr); ok {
		relayAddr := *tcpAddr
		relayAddr.IP = r.relayIPFor(conf.Network)
		return prl, &relayAddr, nil
	}
	return prl, l.Addr(), nil
}

// relayAddressGenerator hands a Relay to the pion TURN server. Go has no covariant returns, so
// the Relay methods, which return transports the dataplane can ask about themselves, do not
// satisfy RelayAddressGenerator on their own; this narrows them to the bare net.* types the
// server takes. AllocateListener and Validate already match and are promoted as they are.
type relayAddressGenerator struct{ *Relay }

var _ turn.RelayAddressGenerator = relayAddressGenerator{}

func (g relayAddressGenerator) AllocatePacketConn(conf turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	return g.Relay.AllocatePacketConn(conf)
}

func (g relayAddressGenerator) AllocateConn(conf turn.AllocateConnConfig) (net.Conn, error) {
	return g.Relay.AllocateConn(conf)
}

// routeRemote resolves the cluster admitting a remote peer endpoint (protocol and port derived from
// the address type), or ("", false) if none does.
func routeRemote(rt *objruntime.Runtime, listener string, remote net.Addr) (string, bool) {
	var (
		proto stnrv1.ClusterProtocol
		peer  net.IP
		port  int
	)
	switch a := remote.(type) {
	case *net.UDPAddr:
		proto, peer, port = stnrv1.ClusterProtocolUDP, a.IP, a.Port
	case *net.TCPAddr:
		proto, peer, port = stnrv1.ClusterProtocolTCP, a.IP, a.Port
	default:
		return "", false
	}
	return rt.Router.RoutePeer(listener, proto, peer, port)
}

// listenTCP binds the relayed TCP transport address with the reuse socket options so outgoing dials
// can share it. transport.Net has no listen-config hook, so the kernel path uses net.ListenConfig
// directly; only vnet-backed tests go through rt.Net.
func listenTCP(rt *objruntime.Runtime, network string, port int) (net.Listener, error) {
	wildcard := net.IPv4zero
	if strings.HasSuffix(network, "6") {
		wildcard = net.IPv6unspecified
	}
	laddr := &net.TCPAddr{IP: wildcard, Port: port}

	if _, ok := rt.Net.(*stdnet.Net); ok {
		lc := net.ListenConfig{Control: netutil.ReuseAddrControl}
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
