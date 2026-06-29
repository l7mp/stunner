package netutil

// code adopted from github.com/livekit/pkg/telemetry

import (
	"net"
	"sync"
	"time"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/telemetry"
)

// AdmitFunc decides whether an inbound connection is admitted for telemetry wrapping.
// It returns the metric name label and true on success, or "" and false on denial.
type AdmitFunc func(remote net.Addr) (name string, ok bool)

// Listener is a net.Listener that knows how to report to Prometheus with optional per-connection
// admission control function.
type Listener struct {
	net.Listener
	name      string
	connType  telemetry.ConnType
	telemetry *telemetry.Telemetry
	admit     AdmitFunc
	log       logging.LeveledLogger
}

// NewListener creates a telemetry-reporting net.Listener with optional per-connection admission.
// A nil admit admits every connection under the listener's own name; a non-nil admit decides per
// connection and supplies the metric name label.
func NewListener(l net.Listener, n string, t telemetry.ConnType, tm *telemetry.Telemetry, admit AdmitFunc, log logging.LeveledLogger) *Listener {
	if admit == nil {
		admit = func(_ net.Addr) (string, bool) {
			return n, true
		}
	}

	return &Listener{Listener: l, name: n, connType: t, telemetry: tm, admit: admit, log: log}
}

// Accept accepts a new connection on a Listener.
func (l *Listener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}

		name, ok := l.admit(conn.RemoteAddr())
		if !ok {
			if l.log != nil {
				l.log.Infof("dropping inbound relay connection from unadmitted peer %s",
					conn.RemoteAddr().String())
			}
			_ = conn.Close()
			continue
		}

		if name == "" {
			name = l.name
		}

		return NewConn(conn, name, l.connType, l.telemetry), nil
	}
}

// Conn is a net.Conn that knows how to report to Prometheus.
type Conn struct {
	net.Conn
	name      string
	connType  telemetry.ConnType
	telemetry *telemetry.Telemetry
}

// NewConn allocates a conn that knows its name and type and reports to telemetry.
func NewConn(c net.Conn, n string, t telemetry.ConnType, tm *telemetry.Telemetry) *Conn {
	tm.AddConnection(n, t)
	return &Conn{Conn: c, name: n, connType: t, telemetry: tm}
}

// Read reads from the Conn.
func (c *Conn) Read(b []byte) (n int, err error) {
	n, err = c.Conn.Read(b)
	if n > 0 {
		c.telemetry.IncrementBytes(c.name, c.connType, telemetry.Incoming, uint64(n))
		c.telemetry.IncrementPackets(c.name, c.connType, telemetry.Incoming, 1)
	}
	return
}

// Write writes to the Conn.
func (c *Conn) Write(b []byte) (n int, err error) {
	n, err = c.Conn.Write(b)
	if n > 0 {
		c.telemetry.IncrementBytes(c.name, c.connType, telemetry.Outgoing, uint64(n))
		c.telemetry.IncrementPackets(c.name, c.connType, telemetry.Outgoing, 1)
	}
	return
}

// Close closes the Conn.
func (c *Conn) Close() error {
	c.telemetry.SubConnection(c.name, c.connType)
	return c.Conn.Close()
}

// PacketConn is the datagram analog of Listener: a net.PacketConn that reports to Prometheus with
// optional per-packet admission. A nil admit admits every datagram under the conn's own name; a
// non-nil admit decides per peer endpoint and supplies the metric name label. Inbound datagrams
// from unadmitted peers are dropped (ReadFrom skips them); outbound writes to unadmitted peers are
// rejected with ErrPortProhibited.
type PacketConn struct {
	net.PacketConn
	name         string
	connType     telemetry.ConnType
	telemetry    *telemetry.Telemetry
	admit        AdmitFunc
	readDeadline time.Time
	mu           sync.Mutex
	log          logging.LeveledLogger
}

// NewPacketConn decorates a net.PacketConn with metric reporting and optional per-packet admission.
func NewPacketConn(c net.PacketConn, n string, t telemetry.ConnType, tm *telemetry.Telemetry, admit AdmitFunc, log logging.LeveledLogger) *PacketConn {
	if admit == nil {
		admit = func(_ net.Addr) (string, bool) {
			return n, true
		}
	}
	tm.AddConnection(n, t)
	return &PacketConn{PacketConn: c, name: n, connType: t, telemetry: tm, admit: admit, log: log}
}

// ReadFrom reads, dropping datagrams from unadmitted peers, and accounts incoming traffic under
// the admitted name.
func (c *PacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		c.mu.Lock()
		deadline := c.readDeadline
		c.mu.Unlock()
		if err := c.PacketConn.SetReadDeadline(deadline); err != nil {
			return 0, nil, err
		}

		n, addr, err := c.PacketConn.ReadFrom(p)
		if err != nil {
			return n, addr, err
		}

		name, ok := c.admit(addr)
		if !ok {
			continue
		}
		if name == "" {
			name = c.name
		}
		if n > 0 {
			c.telemetry.IncrementBytes(name, c.connType, telemetry.Incoming, uint64(n))
			c.telemetry.IncrementPackets(name, c.connType, telemetry.Incoming, 1)
		}
		return n, addr, nil
	}
}

// WriteTo admits the peer, writes, and accounts outgoing traffic under the admitted name.
func (c *PacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	name, ok := c.admit(addr)
	if !ok {
		return 0, ErrPortProhibited
	}
	if name == "" {
		name = c.name
	}
	n, err := c.PacketConn.WriteTo(p, addr)
	if n > 0 {
		c.telemetry.IncrementBytes(name, c.connType, telemetry.Outgoing, uint64(n))
		c.telemetry.IncrementPackets(name, c.connType, telemetry.Outgoing, 1)
	}
	return n, err
}

// SetReadDeadline stores the deadline applied by ReadFrom on each read attempt.
func (c *PacketConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	return nil
}

// Close closes the wrapped packet connection and drops its telemetry accounting.
func (c *PacketConn) Close() error {
	c.telemetry.SubConnection(c.name, c.connType)
	return c.PacketConn.Close()
}
