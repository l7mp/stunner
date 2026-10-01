// Package udp implements UDP listeners as net.Listeners: every client, identified by its address,
// gets a conn of its own.
package udp

import (
	"errors"
	"net"
	"os"
	goruntime "runtime"
	"sync"
	"time"

	"github.com/pion/transport/v5"
	"github.com/pion/transport/v5/deadline"
	"github.com/pion/transport/v5/reuseport"
	"github.com/pion/transport/v5/stdnet"
	"k8s.io/utils/lru"
)

const (
	// maxDatagramSize bounds a UDP datagram.
	maxDatagramSize = 65535
	// queueSize bounds the datagrams queued on a conn.
	queueSize = 256
	// backlog bounds the conns not yet accepted; a full backlog blocks the read loops.
	backlog = 128
	// drainWait is how long a closing connected conn waits for each datagram left on its socket.
	drainWait = time.Millisecond
	// connectedTableSize bounds the table of connected conns.
	connectedTableSize = 4096
	// readBufferSize is the receive buffer asked for the listener sockets, best effort.
	readBufferSize = 1 << 20
)

// aLongTimeAgo is a read deadline in the past: setting it wakes a Read blocked on a socket.
var aLongTimeAgo = time.Unix(1, 0)

// ListenConfig configures a UDP listener.
type ListenConfig struct {
	// Net is the network to listen on, nil for the OS network.
	Net transport.Net
	// ConnectBack connects a socket back to every client so the kernel demuxes; it needs
	// reuse-port, so only the OS network can do it.
	ConnectBack bool
}

// DefaultListenConfig is the config the dataplane listens with.
var DefaultListenConfig = ListenConfig{}

// Listen binds a UDP listener at address.
func (lc ListenConfig) Listen(network, address string) (net.Listener, error) {
	var socks []net.PacketConn
	reuse := false
	if _, onOS := lc.Net.(*stdnet.Net); lc.Net == nil || onOS {
		var err error
		if socks, reuse, err = listenReusePort(network, address, goruntime.GOMAXPROCS(0)); err != nil {
			return nil, err
		}
	} else {
		sock, err := lc.Net.ListenPacket(network, address)
		if err != nil {
			return nil, err
		}
		socks = []net.PacketConn{sock}
	}
	l := &listener{
		socks:       socks,
		connectBack: reuse && lc.ConnectBack,
		demuxed:     map[string]*conn{},
		connected:   lru.New(connectedTableSize),
		acceptQueue: make(chan *conn, backlog),
		closed:      make(chan struct{}),
	}
	for _, sock := range socks {
		if b, ok := sock.(interface{ SetReadBuffer(int) error }); ok {
			_ = b.SetReadBuffer(readBufferSize)
		}
		go l.catch(sock)
	}
	return l, nil
}

// listener reads its sockets in one loop each. Demuxing, every datagram is queued on the conn of
// its source. Connecting back, the first datagram of a client binds a socket connected back to it;
// datagrams racing the connect are not lost: the client's own ones caught by a read loop are queued
// on its conn, other clients' ones caught by the new socket are handed back to the listener.
type listener struct {
	socks       []net.PacketConn
	connectBack bool

	mu        sync.Mutex
	demuxed   map[string]*conn // the demuxed conns, by client address
	connected *lru.Cache       // client address -> the most recently active connected conns

	acceptQueue chan *conn
	acceptMu    sync.RWMutex // orders the pushes to accept against Close
	closed      chan struct{}
	closeOnce   sync.Once
}

func (l *listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.acceptQueue:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *listener) Addr() net.Addr { return l.socks[0].LocalAddr() }

// Close closes the sockets, the conns not yet accepted, and the demuxed conns.
func (l *listener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closed)
		for _, sock := range l.socks {
			_ = sock.Close()
		}
		l.acceptMu.Lock()
		for {
			select {
			case c := <-l.acceptQueue:
				_ = c.Close()
				continue
			default:
			}
			break
		}
		l.acceptMu.Unlock()
		l.mu.Lock()
		demuxed := make([]*conn, 0, len(l.demuxed))
		for _, c := range l.demuxed {
			demuxed = append(demuxed, c)
		}
		l.mu.Unlock()
		for _, c := range demuxed {
			_ = c.Close()
		}
	})
	return nil
}

// catch reads a listener socket until it closes.
func (l *listener) catch(sock net.PacketConn) {
	buf := make([]byte, maxDatagramSize)
	for {
		n, src, err := sock.ReadFrom(buf)
		if err != nil {
			return
		}
		l.intake(sock, src, buf[:n])
	}
}

// intake queues a datagram on the conn of its source, making the conn on the first one.
func (l *listener) intake(sock net.PacketConn, src net.Addr, p []byte) {
	select {
	case <-l.closed:
		return
	default:
	}

	key := src.String()
	l.mu.Lock()
	c, ok := l.demuxed[key]
	if !ok {
		if v, found := l.connected.Get(key); found {
			c, ok = v.(*conn), true
		}
	}
	if !ok {
		c = l.newConn(sock, src)
		if c.conn != nil {
			l.connected.Add(key, c)
		} else {
			l.demuxed[key] = c
		}
	}
	l.mu.Unlock()

	c.deliver(p)
	if ok {
		return
	}
	l.acceptMu.RLock()
	defer l.acceptMu.RUnlock()
	select {
	case l.acceptQueue <- c:
	case <-l.closed:
		_ = c.Close()
	}
}

func (l *listener) newConn(sock net.PacketConn, src net.Addr) *conn {
	c := &conn{
		l:        l,
		sock:     sock,
		peer:     src,
		queue:    make(chan []byte, queueSize),
		deadline: deadline.New(),
		closed:   make(chan struct{}),
	}
	if !l.connectBack {
		return c
	}
	d := net.Dialer{LocalAddr: sock.LocalAddr(), Control: reuseport.Control}
	raw, err := d.Dial("udp", src.String()) //nolint:noctx
	if err != nil {
		return c
	}
	c.conn = raw.(*net.UDPConn)
	return c
}

// forget drops the conn of a client, if it is still the one registered.
func (l *listener) forget(key string, c *conn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.demuxed[key] == c {
		delete(l.demuxed, key)
	}
	if v, ok := l.connected.Get(key); ok && v.(*conn) == c {
		l.connected.Remove(key)
	}
}

// conn is the conn of one client: a socket connected back to it, or the listener socket that read
// it.
type conn struct {
	l    *listener
	sock net.PacketConn // the listener socket that read the client first
	conn *net.UDPConn   // the connected socket, nil when demuxing
	peer net.Addr

	queue    chan []byte
	deadline *deadline.Deadline // the read deadline

	// mu serializes the read deadline changes of the connected socket with closing.
	mu        sync.Mutex
	closed    chan struct{}
	closeOnce sync.Once
}

// deliver queues a datagram, waking a Read blocked on the connected socket.
func (c *conn) deliver(p []byte) {
	select {
	case c.queue <- append([]byte(nil), p...):
	default:
		return
	}
	if c.conn == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.closed:
	default:
		_ = c.conn.SetReadDeadline(aLongTimeAgo)
	}
}

func (c *conn) Read(p []byte) (int, error) {
	for {
		select {
		case d := <-c.queue:
			return copy(p, d), nil
		default:
		}

		if c.conn == nil {
			select {
			case d := <-c.queue:
				return copy(p, d), nil
			case <-c.closed:
				return 0, net.ErrClosed
			case <-c.deadline.Done():
				return 0, os.ErrDeadlineExceeded
			}
		}

		n, from, err := c.conn.ReadFrom(p)
		if err != nil {
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				return 0, err
			}
			d, set := c.deadline.Deadline()
			if set && !time.Now().Before(d) {
				return 0, err
			}
			// woken by deliver: restore the caller's deadline and look at the queue again
			c.mu.Lock()
			select {
			case <-c.closed:
				c.mu.Unlock()
				return 0, net.ErrClosed
			default:
			}
			err := c.conn.SetReadDeadline(d)
			c.mu.Unlock()
			if err != nil {
				return 0, err
			}
			continue
		}
		if from.String() != c.peer.String() {
			// another client's datagram, caught before the socket was connected
			c.l.intake(c.sock, from, p[:n])
			continue
		}
		return n, nil
	}
}

func (c *conn) Write(p []byte) (int, error) {
	if c.conn != nil {
		return c.conn.Write(p)
	}
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
	}
	return c.sock.WriteTo(p, c.peer)
}

// Close closes the conn; a connected one first hands other clients' datagrams back to the listener.
func (c *conn) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		close(c.closed)
		c.mu.Unlock()
		c.l.forget(c.peer.String(), c)
		if c.conn == nil {
			return
		}
		buf := make([]byte, maxDatagramSize)
		for i := 0; i < queueSize; i++ {
			_ = c.conn.SetReadDeadline(time.Now().Add(drainWait))
			n, from, err := c.conn.ReadFrom(buf)
			if err != nil {
				break
			}
			if from.String() != c.peer.String() {
				c.l.intake(c.sock, from, buf[:n])
			}
		}
		_ = c.conn.Close()
	})
	return nil
}

func (c *conn) LocalAddr() net.Addr {
	if c.conn != nil {
		return c.conn.LocalAddr()
	}
	return c.sock.LocalAddr()
}

func (c *conn) RemoteAddr() net.Addr { return c.peer }

func (c *conn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

// SetReadDeadline sets the read deadline, keeping the wake-up of a datagram already queued.
func (c *conn) SetReadDeadline(t time.Time) error {
	c.deadline.Set(t)
	if c.conn == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.closed:
		return nil
	default:
	}
	if err := c.conn.SetReadDeadline(t); err != nil {
		return err
	}
	if len(c.queue) > 0 {
		return c.conn.SetReadDeadline(aLongTimeAgo)
	}
	return nil
}

func (c *conn) SetWriteDeadline(t time.Time) error {
	if c.conn != nil {
		return c.conn.SetWriteDeadline(t)
	}
	return nil
}
