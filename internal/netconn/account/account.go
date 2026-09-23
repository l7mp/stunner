// Package account is the accounting layer of the conn stack: it counts the traffic of a
// classified transport in telemetry, under the class it reads from the layer below, and adds no
// getter of its own, so what comes out is the same contract that went in.
package account

import (
	"net"

	"github.com/l7mp/stunner/v2/internal/netconn/classify"
	"github.com/l7mp/stunner/v2/internal/telemetry"
)

// PacketConn accounts the traffic of a classified packet conn: the connection is counted under
// name, the bytes and packets of each datagram under the class of its peer.
func PacketConn(t *telemetry.Telemetry, c classify.PacketConn, name string, ct telemetry.ConnType) classify.PacketConn {
	t.AddConnection(name, ct)
	return &packetConn{PacketConn: c, name: name, connType: ct, telemetry: t}
}

type packetConn struct {
	classify.PacketConn
	name      string
	connType  telemetry.ConnType
	telemetry *telemetry.Telemetry
}

func (c *packetConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(p)
	if n > 0 {
		account(c.telemetry, c.class(addr), c.connType, telemetry.Incoming, n)
	}
	return n, addr, err
}

func (c *packetConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(p, addr)
	if n > 0 {
		account(c.telemetry, c.class(addr), c.connType, telemetry.Outgoing, n)
	}
	return n, err
}

// class is the label a datagram is accounted under: the peer's class, or the conn's own name if
// the peer fell out of every class between the transfer and the lookup.
func (c *packetConn) class(peer net.Addr) string {
	if class, ok := c.Class(peer); ok {
		return class
	}
	return c.name
}

func (c *packetConn) Close() error {
	c.telemetry.SubConnection(c.name, c.connType)
	return c.PacketConn.Close()
}

// Conn accounts the traffic of a classified stream conn under its class.
func Conn(t *telemetry.Telemetry, c classify.Conn, ct telemetry.ConnType) classify.Conn {
	t.AddConnection(c.Class(), ct)
	return &conn{Conn: c, connType: ct, telemetry: t}
}

type conn struct {
	classify.Conn
	connType  telemetry.ConnType
	telemetry *telemetry.Telemetry
}

func (c *conn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		account(c.telemetry, c.Class(), c.connType, telemetry.Incoming, n)
	}
	return n, err
}

func (c *conn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		account(c.telemetry, c.Class(), c.connType, telemetry.Outgoing, n)
	}
	return n, err
}

func (c *conn) Close() error {
	c.telemetry.SubConnection(c.Class(), c.connType)
	return c.Conn.Close()
}

// Listener accounts every connection accepted on a classified listener under its class.
func Listener(t *telemetry.Telemetry, l classify.Listener, ct telemetry.ConnType) classify.Listener {
	return &listener{Listener: l, connType: ct, telemetry: t}
}

type listener struct {
	classify.Listener
	connType  telemetry.ConnType
	telemetry *telemetry.Telemetry
}

func (l *listener) AcceptConn() (classify.Conn, error) {
	c, err := l.Listener.AcceptConn()
	if err != nil {
		return nil, err
	}
	return Conn(l.telemetry, c, l.connType), nil
}

func (l *listener) Accept() (net.Conn, error) {
	c, err := l.AcceptConn()
	if err != nil {
		return nil, err
	}
	return c, nil
}

func account(t *telemetry.Telemetry, name string, ct telemetry.ConnType, d telemetry.Direction, n int) {
	t.IncrementBytes(name, ct, d, uint64(n))
	t.IncrementPackets(name, ct, d, 1)
}
