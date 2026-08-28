package turn

import (
	"net"

	"github.com/l7mp/stunner/v2/internal/netutil"
	"github.com/l7mp/stunner/v2/internal/upstream"
)

// The conn types below are the reason nothing above needs unwrapping: each embeds the accounting
// and admission wrapper and answers the upstream questions itself, so the outermost type is the
// upstream transport rather than a decorator hiding one.

var (
	_ upstream.PacketConn = &plainPacketConn{}
	_ upstream.PacketConn = &turnPacketConn{}
	_ upstream.Conn       = &plainConn{}
	_ upstream.Conn       = &turnConn{}
)

// plainPacketConn relays datagrams straight to the peer from the relay socket.
type plainPacketConn struct{ *netutil.PacketConn }

// TransportAddrs reports a nil remote: datagrams go straight to the peer they name, there is no
// fixed wire remote to match on.
func (c *plainPacketConn) TransportAddrs() (local, remote net.Addr) {
	return c.LocalAddr(), nil
}

func (c *plainPacketConn) Channel(net.Addr) (uint16, bool) { return 0, false }

// plainConn is a direct stream connection to the peer.
type plainConn struct{ *netutil.Conn }

// TransportAddrs reports a nil remote: the connection runs straight to the peer, there is no
// relay server in between to match on.
func (c *plainConn) TransportAddrs() (local, remote net.Addr) {
	return c.LocalAddr(), nil
}

func (c *plainConn) Channel(net.Addr) (uint16, bool) { return 0, false }

// turnPacketConn relays datagrams through an upstream TURN session, so the wire addresses and
// the channel framing are the session's to report.
type turnPacketConn struct {
	*netutil.PacketConn
	up upstream.PacketConn
}

func (c *turnPacketConn) TransportAddrs() (local, remote net.Addr) { return c.up.TransportAddrs() }
func (c *turnPacketConn) Channel(peer net.Addr) (uint16, bool)     { return c.up.Channel(peer) }

// turnConn is an RFC 6062 connection relayed through an upstream TURN server.
type turnConn struct {
	*netutil.Conn
	up upstream.Conn
}

func (c *turnConn) TransportAddrs() (local, remote net.Addr) { return c.up.TransportAddrs() }
func (c *turnConn) Channel(peer net.Addr) (uint16, bool)     { return c.up.Channel(peer) }
