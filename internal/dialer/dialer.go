// Package dialer implements the transport of a cluster: the conns, relay sockets and relayed
// listeners towards the peers, tagged and accounted under the cluster's name.
package dialer

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/pion/transport/v5/reuseport"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/netconn"
	"github.com/l7mp/stunner/v2/internal/netconn/account"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// New creates the dialer of a cluster config.
func New(conf *stnrv2.ClusterConfig, rt *runtime.Runtime) (api.Dialer, error) {
	proto, err := stnrv2.NewClusterProtocol(conf.Protocol)
	if err != nil {
		return nil, err
	}
	b := base{
		name:  conf.Name,
		proto: proto,
		tag:   api.Tag{Name: conf.Name, Proto: proto},
		rt:    rt,
	}

	if conf.Tunnel != nil {
		u, err := stnrv2.ParseURI(conf.Tunnel.URL)
		if err != nil {
			return nil, err
		}
		b.tunnel, b.transport = conf.Tunnel.DeepCopy(), u.Protocol
	}
	if proto == stnrv2.ProtocolTCP {
		return &tcpDialer{base: b}, nil
	}
	return &udpDialer{base: b}, nil
}

type base struct {
	name  string
	proto stnrv2.Protocol
	tag   api.Tag
	rt    *runtime.Runtime
	// tunnel is the upstream TURN server, nil for a direct dialer; transport is its transport.
	tunnel    *stnrv2.TunnelConfig
	transport stnrv2.Protocol
}

func (b *base) Name() string              { return b.name }
func (b *base) Protocol() stnrv2.Protocol { return b.proto }
func (b *base) Start() error              { return nil }
func (b *base) Close(bool) error          { return nil }

// wrap accounts and tags a peer conn.
func (b *base) wrap(c net.Conn, w netconn.Wire) api.Conn {
	accounted := account.Conn(b.rt.Telemetry, c, b.name, telemetry.ClusterType)
	return netconn.NewConn(accounted, b.tag, w)
}

// advertise returns local's port at the cluster's address of the family, else local itself.
func (b *base) advertise(network string, local net.Addr) net.Addr {
	wantV6 := strings.HasSuffix(network, "6")
	var addrs []string
	if conf := b.rt.ClusterConfig(b.name); conf != nil {
		addrs = conf.Addrs
	}
	for _, a := range addrs {
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

func (cl *udpDialer) Dial(ctx context.Context, _ net.Addr, addr string) (api.Conn, error) {
	if cl.tunnel != nil {
		peer, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return nil, err
		}
		session, err := cl.allocate(ctx)
		if err != nil {
			return nil, err
		}
		return cl.wrap(&peerConn{PacketConn: session, peer: peer}, session), nil
	}
	c, err := cl.rt.Net.Dial("udp", addr)
	if err != nil {
		return nil, err
	}
	return cl.wrap(c, nil), nil
}

func (cl *udpDialer) ListenPacket(network string, port int) (net.PacketConn, net.Addr, error) {
	if cl.tunnel != nil {
		session, err := cl.allocate(context.Background())
		if err != nil {
			return nil, nil, err
		}
		return account.PacketConn(cl.rt.Telemetry, session, cl.name, telemetry.ClusterType),
			session.LocalAddr(), nil
	}
	sock, err := cl.rt.Net.ListenPacket(network, net.JoinHostPort("", strconv.Itoa(sanitizePort(port))))
	if err != nil {
		return nil, nil, err
	}
	return account.PacketConn(cl.rt.Telemetry, sock, cl.name, telemetry.ClusterType),
		cl.advertise(network, sock.LocalAddr()), nil
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
		td, err := cl.client()
		if err != nil {
			return nil, err
		}
		c, err := td.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		return cl.wrap(c, c), nil
	}
	nd := cl.rt.Net.CreateDialer(&net.Dialer{LocalAddr: local, Control: reuseport.Control})
	c, err := nd.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return cl.wrap(c, nil), nil
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
	return account.Listener(cl.rt.Telemetry, l, cl.name, telemetry.ClusterType),
		cl.advertise(network, l.Addr()), nil
}

func sanitizePort(p int) int {
	if p <= 1 || p > 2<<16-1 {
		return 0
	}
	return p
}
