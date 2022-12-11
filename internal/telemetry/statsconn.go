package telemetry

// code adopted from github.com/livekit/pkg/telemetry

import (
	"net"
)

// Listener is a net.Listener that knows how to report to Prometheus.
type Listener struct {
	net.Listener
	name     string
	connType ConnType
}

// NewListener creates a net.Listener that knows its name and type.
func NewListener(l net.Listener, n string, t ConnType) *Listener {
	return &Listener{Listener: l, name: n, connType: t}
}

// Accept accepts a new connection on a Listener.
func (l *Listener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	return NewConn(conn, l.name, l.connType), nil
}

// Conn is a net.Conn that knows how to report to Prometheus.
type Conn struct {
	net.Conn
	name     string
	connType ConnType
}

// NewConn allocates a stats conn that knows its name and type.
func NewConn(c net.Conn, n string, t ConnType) *Conn {
	AddConnection(n, t)
	return &Conn{Conn: c, name: n, connType: t}
}

// Read reads from the Conn.
func (c *Conn) Read(b []byte) (n int, err error) {
	n, err = c.Conn.Read(b)
	if n > 0 {
		IncrementBytes(c.name, c.connType, Incoming, uint64(n))
		IncrementPackets(c.name, c.connType, Incoming, 1)
	}
	return
}

// Write writes to the Conn.
func (c *Conn) Write(b []byte) (n int, err error) {
	n, err = c.Conn.Write(b)
	if n > 0 {
		IncrementBytes(c.name, c.connType, Outgoing, uint64(n))
		IncrementPackets(c.name, c.connType, Outgoing, 1)
	}
	return
}

// Close closes the Conn.
func (c *Conn) Close() error {
	SubConnection(c.name, c.connType)
	return c.Conn.Close()
}

// PacketConn is a net.PacketConn that knows how to report to Prometheus.
type PacketConn struct {
	net.PacketConn
	name     string
	connType ConnType
}

// NewPacketConn allocates a stats conn that knows its name and type.
func NewPacketConn(c net.PacketConn, n string, t ConnType) *PacketConn {
	AddConnection(n, t)
	return &PacketConn{PacketConn: c, name: n, connType: t}
}

// ReadFrom reads from the PacketConn.
func (c *PacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	n, addr, err = c.PacketConn.ReadFrom(p)
	if n > 0 {
		IncrementBytes(c.name, c.connType, Incoming, uint64(n))
		IncrementPackets(c.name, c.connType, Incoming, 1)
	}
	return
}

// WriteTo writes to the PacketConn.
func (c *PacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	n, err = c.PacketConn.WriteTo(p, addr)
	if n > 0 {
		IncrementBytes(c.name, c.connType, Outgoing, uint64(n))
		IncrementPackets(c.name, c.connType, Outgoing, 1)
	}
	return
}

// ReadFrom reads from the PacketConn.
// WriteTo writes to the PacketConn.
// Close closes the PacketConn.
func (c *PacketConn) Close() error {
	SubConnection(c.name, c.connType)
	return c.PacketConn.Close()
}
