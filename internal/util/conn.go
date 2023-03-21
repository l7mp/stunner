package util

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/pion/transport/v2"
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
