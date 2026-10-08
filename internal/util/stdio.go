package util

import (
	"net"
	"os"
	"sync"
	"time"
)

// StdioListener is the one-shot listener of a STDIN listener: it accepts a single conn, the
// process stdin/stdout pair. EOF on stdin ends the flow. The process owns stdio: closing the
// listener or the conn leaves it alone.
type StdioListener struct {
	accepted  sync.Once
	closed    chan struct{}
	closeOnce sync.Once
}

// NewStdioListener creates the listener of the process stdin/stdout pair.
func NewStdioListener() *StdioListener {
	return &StdioListener{closed: make(chan struct{})}
}

func (l *StdioListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.accepted.Do(func() { c = stdioConn{} })
	if c != nil {
		return c, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}

func (l *StdioListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *StdioListener) Addr() net.Addr { return &FileConnAddr{File: os.Stdin} }

// stdioConn is the duplex conn of the stdin/stdout pair.
type stdioConn struct{}

func (stdioConn) Read(b []byte) (int, error)  { return os.Stdin.Read(b) }
func (stdioConn) Write(b []byte) (int, error) { return os.Stdout.Write(b) }
func (stdioConn) Close() error                { return nil }
func (stdioConn) LocalAddr() net.Addr         { return &FileConnAddr{File: os.Stdout} }
func (stdioConn) RemoteAddr() net.Addr        { return &FileConnAddr{File: os.Stdin} }

func (stdioConn) SetDeadline(time.Time) error      { return nil }
func (stdioConn) SetReadDeadline(time.Time) error  { return nil }
func (stdioConn) SetWriteDeadline(time.Time) error { return nil }
