// Package dialer implements the transport of a cluster: the conns, relay sockets and relayed
// listeners towards the peers, tagged and accounted under the cluster's name, and the socket of a
// dialing listener, which reaches its peer through a dialer of its own, accounted as the listener.
package dialer

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/logging"
	"github.com/pion/transport/v5/reuseport"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/netconn"
	"github.com/l7mp/stunner/v2/internal/netconn/account"
	"github.com/l7mp/stunner/v2/internal/proto/fifo"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// handshakeTimeout bounds the TLS or DTLS handshake of a dialed conn.
const handshakeTimeout = 10 * time.Second

// keepAlive is the TCP keepalive of the dialed TCP conns: it refreshes NAT and load-balancer state
// and turns a dead peer into a read error within about a minute.
var keepAlive = net.KeepAliveConfig{Enable: true, Idle: 30 * time.Second, Interval: 10 * time.Second,
	Count: 3}

// New creates the dialer of a cluster config; admit applies the cluster's filters to a peer.
func New(conf *stnrv2.ClusterConfig, rt *runtime.Runtime, admit func(net.Addr) bool) (api.Dialer, error) {
	return newDialer(conf, rt, admit, telemetry.ClusterType)
}

// newDialer creates a dialer accounting its conns as kind under the config's name.
func newDialer(conf *stnrv2.ClusterConfig, rt *runtime.Runtime, admit func(net.Addr) bool, kind telemetry.ConnType) (api.Dialer, error) {
	proto, err := stnrv2.NewClusterProtocol(conf.Protocol)
	if err != nil {
		return nil, err
	}
	b := base{
		name:  conf.Name,
		proto: proto,
		kind:  kind,
		tag:   api.Tag{Name: conf.Name, Proto: proto},
		rt:    rt,
		admit: admit,
		log:   rt.Logger.NewLogger(fmt.Sprintf("cluster-%s", conf.Name)),
		addrs: slices.Clone(conf.Addrs),
	}
	if conf.Tunnel != nil {
		u, err := stnrv2.ParseURI(conf.Tunnel.URL)
		if err != nil {
			return nil, err
		}
		b.tunnel, b.transport = conf.Tunnel.DeepCopy(), u.Protocol
		// the tag names the protocol on the wire: a datagram conn travels in the tunnel's
		// transport, a stream conn (RFC 6062) stays a TCP conn to its peer
		if proto == stnrv2.ProtocolUDP {
			b.tag.Proto = u.Protocol
		}
	}
	if conf.TLS != nil && proto != stnrv2.ProtocolFIFO {
		// a secured conn is TLS or DTLS on the wire, as a listener's
		b.tag.Proto = stnrv2.ProtocolDTLS
		if proto == stnrv2.ProtocolTCP {
			b.tag.Proto = stnrv2.ProtocolTLS
		}
		if b.tls, err = conf.TLS.DialConfig(""); err != nil {
			return nil, err
		}
	}
	switch proto {
	case stnrv2.ProtocolTCP:
		return &tcpDialer{base: b}, nil
	case stnrv2.ProtocolFIFO:
		return &fifoDialer{base: b, fifo: conf.Addrs[0]}, nil
	}
	return &udpDialer{base: b}, nil
}

type base struct {
	name  string
	proto stnrv2.Protocol
	kind  telemetry.ConnType
	tag   api.Tag
	rt    *runtime.Runtime
	admit func(net.Addr) bool
	log   logging.LeveledLogger
	addrs []string // the addresses of the cluster, advertised for its relays
	// tunnel is the upstream TURN server, nil for a direct dialer; transport is its transport.
	tunnel    *stnrv2.TunnelConfig
	transport stnrv2.Protocol
	// tls makes the dialed conns TLS over TCP and DTLS over UDP, nil for plain; an empty
	// ServerName is the host dialed.
	tls *tls.Config
}

func (b *base) Name() string              { return b.name }
func (b *base) Protocol() stnrv2.Protocol { return b.proto }
func (b *base) Tag() api.Tag              { return b.tag }
func (b *base) Start() error              { return nil }
func (b *base) Close(bool) error          { return nil }

// establish completes a conn dialed to addr into the conn the dialer returns: the TLS or DTLS
// handshake if the dialer secures its conns, then the accounting and the tag.
func (b *base) establish(ctx context.Context, c net.Conn, w netconn.Wire, addr string) (api.Conn, error) {
	if b.tls != nil {
		conf := b.tls.Clone()
		if conf.ServerName == "" {
			conf.ServerName, _, _ = net.SplitHostPort(addr)
		}
		ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
		defer cancel()
		var handshake func(context.Context) error
		if b.proto == stnrv2.ProtocolTCP {
			tc := tls.Client(c, conf)
			c, handshake = tc, tc.HandshakeContext
		} else {
			opts := []dtls.ClientOption{dtls.WithServerName(conf.ServerName),
				dtls.WithRootCAs(conf.RootCAs), dtls.WithInsecureSkipVerify(conf.InsecureSkipVerify)}
			if len(conf.Certificates) > 0 {
				// pion refuses an empty certificate option
				opts = append(opts, dtls.WithCertificates(conf.Certificates...))
			}
			dc, err := dtls.ClientWithOptions(netconn.PacketConn{Conn: c}, c.RemoteAddr(), opts...)
			if err != nil {
				_ = c.Close()
				return nil, err
			}
			c, handshake = dc, dc.HandshakeContext
		}
		if err := handshake(ctx); err != nil {
			_ = c.Close()
			return nil, fmt.Errorf("%s handshake with %s failed: %w", b.name, addr, err)
		}
	}
	accounted := account.Conn(b.rt.Telemetry, c, b.name, b.kind)
	return netconn.NewConn(accounted, b.tag, w), nil
}

// advertise returns local's port at the cluster's address of the family, else local itself.
func (b *base) advertise(network string, local net.Addr) net.Addr {
	wantV6 := strings.HasSuffix(network, "6")
	for _, a := range b.addrs {
		ip := net.ParseIP(a)
		if ip == nil || (ip.To4() == nil) != wantV6 {
			continue
		}
		switch a := local.(type) {
		case *net.UDPAddr:
			return &net.UDPAddr{IP: ip, Port: a.Port}
		case *net.TCPAddr:
			return &net.TCPAddr{IP: ip, Port: a.Port}
		}
	}
	return local
}

// udpDialer reaches UDP peers, directly or with a fresh upstream allocation per conn.
type udpDialer struct {
	base
}

func (cl *udpDialer) Dial(ctx context.Context, local net.Addr, addr string) (api.Conn, error) {
	if cl.tunnel != nil {
		peer, err := cl.rt.Net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return nil, err
		}
		if err := cl.checkPeer(peer); err != nil {
			return nil, err
		}
		session, err := cl.allocate(ctx)
		if err != nil {
			return nil, err
		}
		return cl.establish(ctx, &peerConn{PacketConn: session, peer: peer}, session, addr)
	}
	peer, err := cl.rt.Net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	if err := cl.checkPeer(peer); err != nil {
		return nil, err
	}
	c, err := cl.rt.Net.CreateDialer(&net.Dialer{LocalAddr: local}).Dial("udp", peer.String())
	if err != nil {
		return nil, err
	}
	return cl.establish(ctx, c, nil, addr)
}

func (cl *udpDialer) ListenPacket(network string, port int) (net.PacketConn, net.Addr, error) {
	if cl.tunnel != nil {
		session, err := cl.allocate(context.Background())
		if err != nil {
			return nil, nil, err
		}
		return netconn.FilterPacketConn(account.PacketConn(cl.rt.Telemetry, session, cl.name,
			cl.kind), cl.admit), session.LocalAddr(), nil
	}
	sock, err := cl.rt.Net.ListenPacket(network, net.JoinHostPort("", strconv.Itoa(sanitizePort(port))))
	if err != nil {
		return nil, nil, err
	}
	return netconn.FilterPacketConn(account.PacketConn(cl.rt.Telemetry, sock, cl.name,
		cl.kind), cl.admit), cl.advertise(network, sock.LocalAddr()), nil
}

func (cl *udpDialer) Listen(string, int) (net.Listener, net.Addr, error) {
	return nil, nil, api.ErrNotSupported
}

// tcpDialer reaches TCP peers, directly or with an upstream RFC 6062 allocation. pion dials the
// peers of an allocation from its relayed address, so the relayed listener and the dials share a
// port: both set the reuse socket options.
type tcpDialer struct {
	base
}

func (cl *tcpDialer) Dial(ctx context.Context, local net.Addr, addr string) (api.Conn, error) {
	if cl.tunnel != nil {
		if t := cl.transport; t != stnrv2.ProtocolTURNTCP && t != stnrv2.ProtocolTURNTLS {
			return nil, fmt.Errorf("dialer %s cannot reach a TCP peer over a %s tunnel: RFC 6062 "+
				"needs a stream transport", cl.name, t.String())
		}
		peer, err := cl.rt.Net.ResolveTCPAddr("tcp", addr)
		if err != nil {
			return nil, err
		}
		if err := cl.checkPeer(peer); err != nil {
			return nil, err
		}
		td, err := cl.client()
		if err != nil {
			return nil, err
		}
		c, err := td.DialContext(ctx, "tcp", peer.String())
		if err != nil {
			return nil, err
		}
		return cl.establish(ctx, c, c, addr)
	}
	peer, err := cl.rt.Net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}
	if err := cl.checkPeer(peer); err != nil {
		return nil, err
	}
	nd := cl.rt.Net.CreateDialer(&net.Dialer{LocalAddr: local, Control: reuseport.Control,
		KeepAliveConfig: keepAlive})
	c, err := nd.Dial("tcp", peer.String())
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return cl.establish(ctx, c, nil, addr)
}

func (cl *tcpDialer) ListenPacket(string, int) (net.PacketConn, net.Addr, error) {
	return nil, nil, api.ErrNotSupported
}

// Listen binds a relayed TCP address. Through a tunnel relaying is outbound only, so the listener
// rejects what it accepts.
func (cl *tcpDialer) Listen(network string, port int) (net.Listener, net.Addr, error) {
	lc := cl.rt.Net.CreateListenConfig(&net.ListenConfig{Control: reuseport.Control})
	l, err := lc.Listen(context.Background(), network,
		net.JoinHostPort("", strconv.Itoa(sanitizePort(port))))
	if err != nil {
		return nil, nil, err
	}
	if cl.tunnel != nil {
		l = rejectListener{l}
	}
	return netconn.FilterListener(account.Listener(cl.rt.Telemetry, l, cl.name,
		cl.kind), cl.admit), cl.advertise(network, l.Addr()), nil
}

// fifoDialer opens the fifo the first address of its cluster names, whatever it is asked to dial.
type fifoDialer struct {
	base
	fifo string
}

func (cl *fifoDialer) Dial(ctx context.Context, _ net.Addr, _ string) (api.Conn, error) {
	c, err := cl.rt.Fifos.DialContext(ctx, fifo.Network, cl.fifo)
	if err != nil {
		return nil, err
	}
	return cl.establish(ctx, c, nil, cl.fifo)
}

func (cl *fifoDialer) ListenPacket(string, int) (net.PacketConn, net.Addr, error) {
	return nil, nil, api.ErrNotSupported
}

func (cl *fifoDialer) Listen(string, int) (net.Listener, net.Addr, error) {
	return nil, nil, api.ErrNotSupported
}

// checkPeer fails the dial of a peer the cluster's filters refuse.
func (b *base) checkPeer(peer net.Addr) error {
	if b.admit(peer) {
		return nil
	}
	b.log.Infof("cluster %s: filters refuse peer %s", b.name, peer.String())
	return fmt.Errorf("cluster %s: %w: %s", b.name, errRefused, peer.String())
}

var errRefused = errors.New("peer refused by the filters")

func sanitizePort(p int) int {
	if p <= 1 || p > 65535 {
		return 0
	}
	return p
}
