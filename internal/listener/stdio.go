package listener

import (
	"net"
	"os"
	"sync"
	"time"

	"github.com/l7mp/stunner/v2/internal/util"
)

// stdioListener is the one-shot listener of a STDIN listener: it accepts a single conn, the
// process stdin/stdout pair. EOF on stdin ends the flow. The process owns stdio: closing the
// listener or the conn leaves it alone.
type stdioListener struct {
	accepted  sync.Once
	closed    chan struct{}
	closeOnce sync.Once
}

func newStdioListener() *stdioListener {
	return &stdioListener{closed: make(chan struct{})}
}

func (l *stdioListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.accepted.Do(func() { c = stdioConn{} })
	if c != nil {
		return c, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}

func (l *stdioListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *stdioListener) Addr() net.Addr { return &util.FileConnAddr{File: os.Stdin} }

// stdioConn is the duplex conn of the stdin/stdout pair.
type stdioConn struct{}

func (stdioConn) Read(b []byte) (int, error)  { return os.Stdin.Read(b) }
func (stdioConn) Write(b []byte) (int, error) { return os.Stdout.Write(b) }
func (stdioConn) Close() error                { return nil }
func (stdioConn) LocalAddr() net.Addr         { return &util.FileConnAddr{File: os.Stdout} }
func (stdioConn) RemoteAddr() net.Addr        { return &util.FileConnAddr{File: os.Stdin} }

func (stdioConn) SetDeadline(time.Time) error      { return nil }
func (stdioConn) SetReadDeadline(time.Time) error  { return nil }
func (stdioConn) SetWriteDeadline(time.Time) error { return nil }
