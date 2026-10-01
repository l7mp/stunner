// Package netconn tags a conn with its listener or cluster and its wire.
package netconn

import (
	"net"

	"github.com/l7mp/stunner/v2/internal/api"
)

// Wire describes the wire of a conn tunnelled through a TURN session.
type Wire interface {
	TransportAddrs() (local, remote net.Addr)
	Channel(peer net.Addr) (uint16, bool)
}

// NewConn tags c. A nil wire describes a direct conn: its own addresses, no framing.
func NewConn(c net.Conn, tag api.Tag, wire Wire) api.Conn {
	return &conn{Conn: c, tag: tag, wire: wire}
}

type conn struct {
	net.Conn
	tag  api.Tag
	wire Wire
}

func (c *conn) Tag() api.Tag { return c.tag }

func (c *conn) TransportAddrs() (local, remote net.Addr) {
	if c.wire != nil {
		return c.wire.TransportAddrs()
	}
	return c.LocalAddr(), c.RemoteAddr()
}

func (c *conn) Channel(peer net.Addr) (uint16, bool) {
	if c.wire != nil {
		return c.wire.Channel(peer)
	}
	return 0, false
}
