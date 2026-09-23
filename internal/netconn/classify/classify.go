// Package classify is the enrichment that assigns every datagram and connection a class. The
// class is whatever traffic is attributed to: the cluster admitting a peer on a relay leg, the
// listener itself on a listener socket. Traffic that fits no class is dropped on the way in and
// rejected on the way out, so classification is also admission. Everything stacked above reads
// the class from here instead of resolving it again.
package classify

import (
	"errors"
	"net"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/netconn/wire"
)

// ErrProhibited rejects traffic towards a peer that fits no class.
var ErrProhibited = errors.New("peer endpoint administratively prohibited")

// Func resolves the class of a peer endpoint, false when it fits none.
type Func func(peer net.Addr) (class string, ok bool)

// Const classifies every peer into the one class.
func Const(class string) Func {
	return func(net.Addr) (string, bool) { return class, true }
}

// PacketConn is a datagram transport whose peers are classified.
type PacketConn interface {
	wire.PacketConn
	// Class returns the class of traffic to and from peer, false when it fits none.
	Class(peer net.Addr) (string, bool)
}

// NewPacketConn classifies the peers of c with f: datagrams from unclassified peers are
// dropped, writes to them fail with ErrProhibited.
func NewPacketConn(c wire.PacketConn, f Func) PacketConn {
	return &packetConn{PacketConn: c, classify: f}
}

type packetConn struct {
	wire.PacketConn
	classify Func
}

func (c *packetConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		n, addr, err := c.PacketConn.ReadFrom(p)
		if err != nil {
			return n, addr, err
		}
		if _, ok := c.classify(addr); ok {
			return n, addr, nil
		}
	}
}

func (c *packetConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if _, ok := c.classify(addr); !ok {
		return 0, ErrProhibited
	}
	return c.PacketConn.WriteTo(p, addr)
}

func (c *packetConn) Class(peer net.Addr) (string, bool) { return c.classify(peer) }

// Conn is a stream transport to one peer, classified when the conn was made.
type Conn interface {
	net.Conn
	// Class returns the class of the conn's traffic.
	Class() string
}

// NewConn tags c with its class.
func NewConn(c net.Conn, class string) Conn { return conn{Conn: c, class: class} }

type conn struct {
	net.Conn
	class string
}

func (c conn) Class() string { return c.class }

// Listener is a stream listener that classifies the peers it accepts from.
type Listener interface {
	net.Listener
	// AcceptConn accepts the next connection from a classified peer; Accept returns the
	// same conn as a net.Conn.
	AcceptConn() (Conn, error)
}

// NewListener classifies the peers accepted on l with f, closing connections from peers that fit
// no class. A nil log drops them silently.
func NewListener(l net.Listener, f Func, log logging.LeveledLogger) Listener {
	return &listener{Listener: l, classify: f, log: log}
}

type listener struct {
	net.Listener
	classify Func
	log      logging.LeveledLogger
}

func (l *listener) AcceptConn() (Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		class, ok := l.classify(c.RemoteAddr())
		if !ok {
			if l.log != nil {
				l.log.Infof("dropping inbound connection from unclassified peer %s",
					c.RemoteAddr().String())
			}
			_ = c.Close()
			continue
		}
		return NewConn(c, class), nil
	}
}

func (l *listener) Accept() (net.Conn, error) {
	c, err := l.AcceptConn()
	if err != nil {
		return nil, err
	}
	return c, nil
}
