// Package fifo implements in-process named fifos: a listener makes a fifo under a name and a dialer
// opens it. Every Write is one Read on the other end, as on a connected UDP socket.
package fifo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/pion/transport/v5"
	"github.com/pion/transport/v5/deadline"
)

// queueLen is the number of datagrams a conn buffers per direction.
const queueLen = 256

// dialTimeout bounds the wait of a dial for its fifo to be listened on and accept, when the
// dial's context has no deadline.
const dialTimeout = 5 * time.Second

// Network is the network name of the fifos.
const Network = "fifo"

var (
	_ transport.Dialer       = &Fifos{}
	_ transport.ListenConfig = &Fifos{}
)

var (
	errNetwork = errors.New("not the fifo network")
	errInUse   = errors.New("fifo already bound")
	errRefused = errors.New("no such fifo")
	errNoName  = errors.New("empty fifo name")
)

// Addr is the address of a fifo end: the name of the fifo, and for a dialed end a sequence number.
type Addr string

func (a Addr) Network() string { return Network }
func (a Addr) String() string  { return string(a) }

// Fifos are the fifos of a process, by name: a transport.ListenConfig whose Listen makes a fifo
// and a transport.Dialer whose Dial opens one, on the network "fifo".
type Fifos struct {
	mu    sync.Mutex
	fifos map[string]*Fifo
	seq   uint64
}

// New creates an empty set of fifos.
func New() *Fifos {
	return &Fifos{fifos: map[string]*Fifo{}}
}

// Listen binds the fifo of a name, taking over the dials already waiting for it; a name has one
// listener at a time.
func (fs *Fifos) Listen(_ context.Context, network, name string) (net.Listener, error) {
	if network != Network {
		return nil, fmt.Errorf("%w: %q", errNetwork, network)
	}
	if name == "" {
		return nil, errNoName
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	f, ok := fs.fifos[name]
	if !ok {
		f = fs.newFifo(name)
		fs.fifos[name] = f
	}
	select {
	case <-f.bound:
		return nil, fmt.Errorf("fifo %q: %w", name, errInUse)
	default:
	}
	close(f.bound)
	return f, nil
}

// ListenPacket fails: a fifo carries conns.
func (fs *Fifos) ListenPacket(_ context.Context, network, _ string) (net.PacketConn, error) {
	return nil, fmt.Errorf("%w: no packet conns on %q", errNetwork, network)
}

// Dial opens the fifo of a name.
func (fs *Fifos) Dial(network, name string) (net.Conn, error) {
	return fs.DialContext(context.Background(), network, name)
}

// DialContext opens the fifo of a name, waiting until the fifo is listened on and its accept
// queue is free, or the context (dialTimeout if it has no deadline) ends.
func (fs *Fifos) DialContext(ctx context.Context, network, name string) (net.Conn, error) {
	if network != Network {
		return nil, fmt.Errorf("%w: %q", errNetwork, network)
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, dialTimeout)
		defer cancel()
	}
	for {
		fs.mu.Lock()
		f, ok := fs.fifos[name]
		if !ok {
			f = fs.newFifo(name)
			fs.fifos[name] = f
		}
		f.waiters++
		fs.seq++
		local := Addr(fmt.Sprintf("%s#%d", name, fs.seq))
		fs.mu.Unlock()

		c, closed, err := f.open(ctx, local)

		fs.mu.Lock()
		f.waiters--
		// nobody listens and nobody waits: forget the name
		select {
		case <-f.bound:
		default:
			if f.waiters == 0 && fs.fifos[name] == f {
				delete(fs.fifos, name)
			}
		}
		fs.mu.Unlock()
		if !closed {
			return c, err
		}
		// the listener closed under the dial: wait for the next one
	}
}

func (fs *Fifos) newFifo(name string) *Fifo {
	return &Fifo{fifos: fs, name: name, conns: make(chan net.Conn, 1), bound: make(chan struct{}),
		done: make(chan struct{})}
}

// Fifo is the fifo of a name: a net.Listener once bound, the accept queue of the dials waiting
// for one before.
type Fifo struct {
	fifos   *Fifos
	name    string
	conns   chan net.Conn // the accept queue, of one conn
	bound   chan struct{} // closed by Listen
	done    chan struct{} // closed by Close
	once    sync.Once
	mu      sync.RWMutex // orders the opens against the drain in Close
	waiters int          // the dials using the fifo, under fifos.mu
}

// open waits for the fifo to be bound and queues a conn on it; closed reports that the fifo closed
// before it accepted the conn.
func (f *Fifo) open(ctx context.Context, local Addr) (net.Conn, bool, error) {
	select {
	case <-f.bound:
	case <-ctx.Done():
		return nil, false, fmt.Errorf("fifo %q: %w: %w", f.name, errRefused, ctx.Err())
	}
	a2b, b2a := make(chan []byte, queueLen), make(chan []byte, queueLen)
	client := newConn(local, Addr(f.name), b2a, a2b)
	server := newConn(Addr(f.name), local, a2b, b2a)
	client.peer, server.peer = server, client
	// the fifo's close drains its queue under the same lock, so a conn never lands in the
	// queue of a closed fifo
	f.mu.RLock()
	defer f.mu.RUnlock()
	select {
	case <-f.done:
		return nil, true, nil
	default:
	}
	select {
	case f.conns <- server:
		return client, false, nil
	case <-f.done:
		return nil, true, nil
	case <-ctx.Done():
		return nil, false, fmt.Errorf("fifo %q: %w", f.name, ctx.Err())
	}
}

func (f *Fifo) Accept() (net.Conn, error) {
	select {
	case <-f.done:
		return nil, net.ErrClosed
	default:
	}
	select {
	case c := <-f.conns:
		return c, nil
	case <-f.done:
		return nil, net.ErrClosed
	}
}

// Close unbinds the fifo, freeing its name, and closes the conn it has not accepted; accepted
// conns stay up.
func (f *Fifo) Close() error {
	f.once.Do(func() {
		close(f.done)
		f.fifos.mu.Lock()
		if f.fifos.fifos[f.name] == f {
			delete(f.fifos.fifos, f.name)
		}
		f.fifos.mu.Unlock()
		f.mu.Lock()
		defer f.mu.Unlock()
		for {
			select {
			case c := <-f.conns:
				_ = c.Close()
			default:
				return
			}
		}
	})
	return nil
}

func (f *Fifo) Addr() net.Addr { return Addr(f.name) }

// conn is one end of a fifo conn.
type conn struct {
	local, remote Addr
	rx            <-chan []byte
	tx            chan<- []byte
	peer          *conn
	done          chan struct{}
	once          sync.Once
	rd, wd        *deadline.Deadline
}

func newConn(local, remote Addr, rx <-chan []byte, tx chan<- []byte) *conn {
	return &conn{local: local, remote: remote, rx: rx, tx: tx, done: make(chan struct{}),
		rd: deadline.New(), wd: deadline.New()}
}

// Read reads one datagram; the part that does not fit p is lost.
func (c *conn) Read(p []byte) (int, error) {
	// queued datagrams first, even after the peer closed
	select {
	case b := <-c.rx:
		return copy(p, b), nil
	default:
	}
	select {
	case b := <-c.rx:
		return copy(p, b), nil
	case <-c.done:
		return 0, net.ErrClosed
	case <-c.peer.done:
		return 0, io.EOF
	case <-c.rd.Done():
		return 0, os.ErrDeadlineExceeded
	}
}

// Write sends p as one datagram, blocking while the peer's queue is full.
func (c *conn) Write(p []byte) (int, error) {
	// a closed end refuses before a free queue slot can win the race
	select {
	case <-c.done:
		return 0, net.ErrClosed
	case <-c.peer.done:
		return 0, io.ErrClosedPipe
	default:
	}
	b := make([]byte, len(p))
	copy(b, p)
	select {
	case c.tx <- b:
		return len(p), nil
	case <-c.done:
		return 0, net.ErrClosed
	case <-c.peer.done:
		return 0, io.ErrClosedPipe
	case <-c.wd.Done():
		return 0, os.ErrDeadlineExceeded
	}
}

func (c *conn) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func (c *conn) LocalAddr() net.Addr  { return c.local }
func (c *conn) RemoteAddr() net.Addr { return c.remote }

func (c *conn) SetDeadline(t time.Time) error {
	c.rd.Set(t)
	c.wd.Set(t)
	return nil
}

func (c *conn) SetReadDeadline(t time.Time) error  { c.rd.Set(t); return nil }
func (c *conn) SetWriteDeadline(t time.Time) error { c.wd.Set(t); return nil }
