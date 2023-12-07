package server

import (
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
)

type Conn struct {
	*websocket.Conn
	Filter              FilterConfig
	readLock, writeLock sync.Mutex // for writemessage
}

func NewConn(conn *websocket.Conn, filter FilterConfig) *Conn {
	return &Conn{
		Conn:   conn,
		Filter: filter,
	}
}

func (c *Conn) Id() string {
	return fmt.Sprintf("%s:%s", c.RemoteAddr().Network(), c.RemoteAddr().String())
}

func (c *Conn) WriteMessage(messageType int, data []byte) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	return c.Conn.WriteMessage(messageType, data)
}

func (c *Conn) ReadMessage() (int, []byte, error) {
	c.readLock.Lock()
	defer c.readLock.Unlock()
	return c.Conn.ReadMessage()
}

type ConnTrack struct {
	conns []*Conn
	lock  sync.RWMutex
}

func NewConnTrack() *ConnTrack {
	return &ConnTrack{
		conns: []*Conn{},
	}
}

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

func (t *ConnTrack) Upsert(c *Conn) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.conns = append(t.conns, c)
}

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

func (t *ConnTrack) Snapshot() []*Conn {
	t.lock.RLock()
	defer t.lock.RUnlock()
	ret := make([]*Conn, len(t.conns))
	copy(ret, t.conns)
	return ret
}
