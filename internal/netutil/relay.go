// Package netutil holds STUNner's networking utilities in one place: the TURN relay-address
// transport (per-allocation packet conn and TCP listener/dialer that route and admit peers via the
// runtime Router), the UDP listener socket pool, the SO_REUSEADDR control, and the
// telemetry-instrumented net.Conn/PacketConn/Listener wrappers. It carries no pion/turn dependency.
package netutil

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/pion/transport/v4/stdnet"

	"github.com/l7mp/stunner/internal/runtime"
	"github.com/l7mp/stunner/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// ErrPortProhibited is returned when a peer endpoint is not admitted by any routed cluster.
var ErrPortProhibited = errors.New("peer endpoint administratively prohibited")

var errNilConn = errors.New("cannot allocate relay connection")

// NewRelayPacketConn creates the UDP relay socket for an allocation, wrapped so every datagram is
// routed/admitted via the Router and accounted in telemetry. relayIP is the address advertised to
// the client.
func NewRelayPacketConn(rt *runtime.Runtime, listener string, relayIP net.IP, network string, requestedPort int) (net.PacketConn, net.Addr, error) {
	// Empty host is the unspecified address: on dual-stack hosts this binds a
	// socket reachable from both IPv4 and IPv6 peers, so relays work for
	// IPv6-only peers (e.g. IPv6-only EKS pods). Hardcoding "0.0.0.0" would be
	// IPv4-only.
	conn, err := rt.Net.ListenPacket(network, net.JoinHostPort("", strconv.Itoa(sanitizePort(requestedPort))))
	if err != nil {
		return nil, nil, err
	}

	prc := NewPacketConn(conn, listener, telemetry.ClusterType, rt.Telemetry, routeChecker(rt, listener),
		rt.Logger.NewLogger(fmt.Sprintf("relay-%s", listener)))

	relayAddr, ok := prc.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, nil, errNilConn
	}
	relayAddr.IP = relayIP
	return prc, relayAddr, nil
}

// NewRelayListener binds the relayed TCP transport address of an RFC 6062 allocation and wraps it
// so every accepted connection is routed/admitted at accept time. The relayed address is shared
// with the allocation's outgoing dials (Dial), so it is bound with the reuse socket options.
func NewRelayListener(rt *runtime.Runtime, listener string, relayIP net.IP, network string, requestedPort int) (net.Listener, net.Addr, error) {
	l, err := listenTCP(rt, network, sanitizePort(requestedPort))
	if err != nil {
		return nil, nil, err
	}

	admit := func(addr net.Addr) (string, bool) {
		return routeRemote(rt, listener, addr)
	}
	prl := NewListener(l, listener, telemetry.ClusterType, rt.Telemetry, admit,
		rt.Logger.NewLogger(fmt.Sprintf("relay-%s", listener)))

	if tcpAddr, ok := l.Addr().(*net.TCPAddr); ok {
		relayAddr := *tcpAddr
		relayAddr.IP = relayIP
		return prl, &relayAddr, nil
	}
	return prl, l.Addr(), nil
}

// Dial opens an outgoing connection for an RFC 6062 Connect: the peer is routed/admitted once, then
// dialed from laddr (the allocation's relayed transport address, shared with its listener, hence
// the reuse socket options). The returned conn is accounted in telemetry under the serving cluster.
func Dial(rt *runtime.Runtime, listener string, laddr, raddr net.Addr) (net.Conn, error) {
	cluster, ok := routeRemote(rt, listener, raddr)
	if !ok {
		return nil, ErrPortProhibited
	}

	remote, ok := raddr.(*net.TCPAddr)
	if !ok {
		return nil, ErrPortProhibited
	}
	network := "tcp4"
	if remote.IP.To4() == nil {
		network = "tcp6"
	}

	d := rt.Net.CreateDialer(&net.Dialer{LocalAddr: laddr, Control: ReuseAddrControl})
	conn, err := d.Dial(network, remote.String())
	if err != nil {
		return nil, err
	}
	return NewConn(conn, cluster, telemetry.ClusterType, rt.Telemetry), nil
}

// HasRoutedCluster reports whether the listener routes to any cluster of the given protocol. Used
// to fail TCP allocations early on listeners with no TCP cluster.
func HasRoutedCluster(rt *runtime.Runtime, listener string, proto stnrv1.ClusterProtocol) bool {
	for _, name := range listenerRoutes(rt, listener) {
		if c, ok := rt.GetConfig(runtime.TypeCluster, name).(*stnrv1.ClusterConfig); ok && c != nil {
			if p, _ := stnrv1.NewClusterProtocol(c.Protocol); p == proto {
				return true
			}
		}
	}
	return false
}

// ProtocolFromNetwork maps a transport network string ("udp", "tcp4", …) to a cluster protocol.
func ProtocolFromNetwork(network string) stnrv1.ClusterProtocol {
	if strings.HasPrefix(network, "udp") {
		return stnrv1.ClusterProtocolUDP
	}
	return stnrv1.ClusterProtocolTCP
}

// routeChecker returns an AdmitFunc that routes a peer endpoint through the Router for a listener,
// using the serving cluster name as the admission/metric label.
func routeChecker(rt *runtime.Runtime, listener string) AdmitFunc {
	return func(addr net.Addr) (string, bool) {
		return routeRemote(rt, listener, addr)
	}
}

// routeRemote resolves the cluster admitting a remote peer endpoint (protocol and port derived from
// the address type), or ("", false) if none does.
func routeRemote(rt *runtime.Runtime, listener string, remote net.Addr) (string, bool) {
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
	return rt.Router.Route(listener, listenerRoutes(rt, listener), proto, peer, port)
}

func listenerRoutes(rt *runtime.Runtime, listener string) []string {
	if c, ok := rt.GetConfig(runtime.TypeListener, listener).(*stnrv1.ListenerConfig); ok && c != nil {
		return c.Routes
	}
	return nil
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
		lc := net.ListenConfig{Control: ReuseAddrControl}
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
