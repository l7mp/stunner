// Package account is the accounting layer of the conn stack: it counts the traffic of a transport
// in telemetry under the name of the listener or cluster the transport belongs to, and adds no
// getter of its own, so what comes out is the same contract that went in.
package account

import (
	"net"
	"sync"

	"github.com/l7mp/stunner/v2/internal/telemetry"
)

// PacketConn accounts the connection and the traffic of a packet conn under name.
func PacketConn(t *telemetry.Telemetry, c net.PacketConn, name string, ct telemetry.ConnType) net.PacketConn {
	t.AddConnection(name, ct)
	return &packetConn{PacketConn: c, name: name, connType: ct, telemetry: t,
		counters: t.Counters(name, ct)}
}

type packetConn struct {
	net.PacketConn
	name      string
	connType  telemetry.ConnType
	telemetry *telemetry.Telemetry
	counters  *telemetry.Counters
	closeOnce sync.Once
}

func (c *packetConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(p)
	if n > 0 {
		c.counters.Add(telemetry.Incoming, n)
	}
	return n, addr, err
}

func (c *packetConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(p, addr)
	if n > 0 {
		c.counters.Add(telemetry.Outgoing, n)
	}
	return n, err
}

// Close uncounts the connection once, however many times the conn is closed.
func (c *packetConn) Close() error {
	c.closeOnce.Do(func() { c.telemetry.SubConnection(c.name, c.connType) })
	return c.PacketConn.Close()
}

// Conn accounts the connection and the traffic of a conn under name.
func Conn(t *telemetry.Telemetry, c net.Conn, name string, ct telemetry.ConnType) net.Conn {
	t.AddConnection(name, ct)
	return &conn{Conn: c, name: name, connType: ct, telemetry: t, counters: t.Counters(name, ct)}
}

type conn struct {
	net.Conn
	name      string
	connType  telemetry.ConnType
	telemetry *telemetry.Telemetry
	counters  *telemetry.Counters
	closeOnce sync.Once
}

func (c *conn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.counters.Add(telemetry.Incoming, n)
	}
	return n, err
}

func (c *conn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		c.counters.Add(telemetry.Outgoing, n)
	}
	return n, err
}

// Close uncounts the connection once, however many times the conn is closed.
func (c *conn) Close() error {
	c.closeOnce.Do(func() { c.telemetry.SubConnection(c.name, c.connType) })
	return c.Conn.Close()
}

// Listener accounts every connection accepted on a listener under name.
func Listener(t *telemetry.Telemetry, l net.Listener, name string, ct telemetry.ConnType) net.Listener {
	return &listener{Listener: l, name: name, connType: ct, telemetry: t}
}

type listener struct {
	net.Listener
	name      string
	connType  telemetry.ConnType
	telemetry *telemetry.Telemetry
}

func (l *listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return Conn(l.telemetry, c, l.name, l.connType), nil
}
