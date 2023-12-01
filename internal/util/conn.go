package util

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v3"
)

var (
	ErrPortProhibited      = errors.New("peer port administratively prohibited")
	ErrInvalidPeerProtocol = errors.New("unknown peer transport protocol")
)

type FileConnAddr struct {
	File *os.File
}

func (s *FileConnAddr) Network() string { return "file" }
func (s *FileConnAddr) String() string  { return s.File.Name() }

type FileConn struct {
	file *os.File
}

func (f *FileConn) Read(b []byte) (n int, err error) {
	return f.file.Read(b)
}

func (f *FileConn) Write(b []byte) (n int, err error) {
	return f.file.Write(b)
}

func (f *FileConn) Close() error {
	return f.file.Close()
}

func (f *FileConn) LocalAddr() net.Addr {
	return &FileConnAddr{File: f.file}
}

func (f *FileConn) RemoteAddr() net.Addr {
	return &FileConnAddr{File: f.file}
}

func (f *FileConn) SetDeadline(t time.Time) error {
	return nil
}

func (f *FileConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (f *FileConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// NewFileConn returns a wrapper that shows an os.File as a net.Conn.
func NewFileConn(file *os.File) net.Conn {
	return &FileConn{file: file}
}

// PacketConnPool is a factory to create pools of related PacketConns, which may either be a set of
// PacketConns bound to the same local IP using SO_REUSEPORT (on unix, under certain circumstances)
// that can do multithreaded readloops, or a single PacketConn as a fallback for non-unic
// architectures and for testing.
type PacketConnPool interface {
	// Make creates a PacketConnPool, caller must make sure to close the sockets.
	Make(network, address string) ([]net.PacketConn, error)
	// Size returns the number of sockets in the pool.
	Size() int
}

// defaultPacketConPool implements a socketpool that consists of only a single socket, used as a fallback for architectures that do not support SO_REUSEPORT or when socket pooling is disabled.
type defaultPacketConnPool struct {
	transport.Net
}

// Make creates a PacketConnPool, caller must make sure to close the sockets.
func (p *defaultPacketConnPool) Make(network, address string) ([]net.PacketConn, error) {
	conns := []net.PacketConn{}

	conn, err := p.ListenPacket(network, address)
	if err != nil {
		return []net.PacketConn{}, fmt.Errorf("failed to create PacketConn at %s "+
			"(REUSEPORT: false): %s", address, err)
	}
	conns = append(conns, conn)

	return conns, nil
}

func (p *defaultPacketConnPool) Size() int { return 1 }

// PortRangePacketConn is a net.PacketConn that filters on the target port range.
type PortRangePacketConn struct {
	net.PacketConn
	name             string
	minPort, maxPort int
	log              logging.LeveledLogger
	readDeadline     time.Time
}

// NewPortRangePacketConn decorates a PacketConn with filtering on a target port range. Errors are reported per listener name.
func NewPortRangePacketConn(c net.PacketConn, listenerName string, minPort, maxPort int, log logging.LeveledLogger) net.PacketConn {
	return &PortRangePacketConn{
		PacketConn: c,
		name:       listenerName,
		minPort:    minPort,
		maxPort:    maxPort,
		log:        log,
	}
}

// WriteTo writes to the PacketConn.
func (c *PortRangePacketConn) WriteTo(p []byte, peerAddr net.Addr) (int, error) {
	switch addr := peerAddr.(type) {
	case *net.UDPAddr:
		if addr.Port < c.minPort || addr.Port > c.maxPort {
			// c.log.Infof("sending UDP packet with invalid peer port %d rejected on listener %q (must be in [%d:%d])",
			// 	addr.Port, c.name, c.minPort, c.maxPort)
			return 0, ErrPortProhibited
		}
	case *net.TCPAddr:
		if addr.Port < c.minPort || addr.Port > c.maxPort {
			// c.log.Infof("sending TCP packet with invalid peer port %d rejected on listener %q (must be in [%d:%d])",
			// 	addr.Port, c.name, c.minPort, c.maxPort)
			return 0, ErrPortProhibited
		}
	default:
		return 0, ErrInvalidPeerProtocol
	}

	return c.PacketConn.WriteTo(p, peerAddr)
}

// ReadFrom reads from the PortRangePacketConn. Blocks until a packet from the speciifed port range
// is received and drops all other packets.
func (c *PortRangePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		var peerAddr net.Addr

		err := c.PacketConn.SetReadDeadline(c.readDeadline)
		if err != nil {
			return 0, peerAddr, err
		}

		n, peerAddr, err := c.PacketConn.ReadFrom(p)

		// Return errors unconditionally: peerAddr will most probably not be valid anyway
		// so it is not worth checking
		if err != nil {
			return n, peerAddr, err
		}

		switch addr := peerAddr.(type) {
		case *net.UDPAddr:
			if addr.Port >= c.minPort && addr.Port <= c.maxPort {
				return n, peerAddr, err
			}
			// c.log.Infof("received UDP packet with invalid peer port %d dropped on listener %q (must be in [%d:%d])",
			// 	addr.Port, c.name, c.minPort, c.maxPort)
		case *net.TCPAddr:
			if addr.Port >= c.minPort && addr.Port <= c.maxPort {
				return n, peerAddr, err
			}
			// c.log.Infof("received TCP packet with invalid peer port %d dropped on listener %q (must be in [%d:%d])",
			// 	addr.Port, c.name, c.minPort, c.maxPort)
		}
	}
}

func (c *PortRangePacketConn) SetReadDeadline(t time.Time) error {
	c.readDeadline = t
	return nil
}
