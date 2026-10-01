package server

import (
	"net"
	"sync"
)

var _ net.Listener = &AcceptQueue{}

// Backlog bounds the accept queue of a server.
const Backlog = 128

// AcceptQueue holds the conns handed to a server until its accept loop takes them. It is a
// net.Listener, so a library with its own accept loop can take from it.
type AcceptQueue struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
	mu    sync.RWMutex // orders the pushes against the drain in Close
}

// NewAcceptQueue creates an accept queue of backlog conns.
func NewAcceptQueue(backlog int) *AcceptQueue {
	return &AcceptQueue{conns: make(chan net.Conn, backlog), done: make(chan struct{})}
}

// Push queues a conn, blocking while the queue is full.
func (q *AcceptQueue) Push(c net.Conn) error {
	q.mu.RLock()
	defer q.mu.RUnlock()
	select {
	case <-q.done:
		return ErrNotServing
	default:
	}
	select {
	case q.conns <- c:
		return nil
	case <-q.done:
		return ErrNotServing
	}
}

func (q *AcceptQueue) Accept() (net.Conn, error) {
	select {
	case c := <-q.conns:
		return c, nil
	case <-q.done:
		return nil, net.ErrClosed
	}
}

// Close unblocks the waiting pushes and closes the queued conns.
func (q *AcceptQueue) Close() error {
	q.once.Do(func() {
		close(q.done)
		q.mu.Lock()
		defer q.mu.Unlock()
		for {
			select {
			case c := <-q.conns:
				_ = c.Close()
			default:
				return
			}
		}
	})
	return nil
}

func (q *AcceptQueue) Addr() net.Addr { return &net.UDPAddr{} }
