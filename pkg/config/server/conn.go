package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/l7mp/stunner/pkg/config/util"
)

// Conn represents a client WebSocket connection.
type Conn struct {
	*util.Conn
	ch     chan *Config
	closed bool
	cancel context.CancelFunc
}

// NewConn wraps a WebSocket connection.
func NewConn(conn *websocket.Conn, ch chan *Config, cancel context.CancelFunc) *Conn {
	return &Conn{
		Conn:   util.NewConn(conn),
		ch:     ch,
		cancel: cancel,
	}
}

// Id returns the IP 5-tuple for a client connection.
func (c *Conn) Id() string {
	return fmt.Sprintf("%s:%s", c.RemoteAddr().Network(), c.RemoteAddr().String())
}

// ConnTrack represents the server's connection tracking table.
type ConnTrack struct {
	conns []*Conn
	lock  sync.RWMutex
}

// NewConnTrack creates a new connection tracking table.
func NewConnTrack() *ConnTrack {
	return &ConnTrack{
		conns: []*Conn{},
	}
}

// Get returns a client connection by the IP 5-tuple.
func (t *ConnTrack) Get(cid string) *Conn {
	t.lock.RLock()
	defer t.lock.RUnlock()
	for _, c := range t.conns {
		if c.Id() == cid {
			return c
		}
	}
	return nil
}

// Upsert insert a new client connection.
func (t *ConnTrack) Upsert(c *Conn) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.conns = append(t.conns, c)
}

// Delete removes a client connection.
func (t *ConnTrack) Delete(conn *Conn) {
	id := conn.Id()
	t.lock.Lock()
	defer t.lock.Unlock()
	for i, c := range t.conns {
		if c.Id() == id {
			t.conns = append(t.conns[:i], t.conns[i+1:]...)
		}
	}
}

// Snapshot creates a snapshot of the connection tracking table.
func (t *ConnTrack) Snapshot() []*Conn {
	t.lock.RLock()
	defer t.lock.RUnlock()
	ret := make([]*Conn, len(t.conns))
	copy(ret, t.conns)
	return ret
}
