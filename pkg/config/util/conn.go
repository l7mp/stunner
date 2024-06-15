package util

import (
	"sync"

	"github.com/gorilla/websocket"
)

// Conn represents a client WebSocket connection. An added lock guards the underlying connection
// from concurrent write to websocket connection errors.
type Conn struct {
	*websocket.Conn
	readLock, writeLock sync.Mutex // for writemessage

}

// NewConn wraps a WebSocket connection.
func NewConn(conn *websocket.Conn) *Conn {
	return &Conn{Conn: conn}
}

// WriteMessage writes a message to the client connection with proper locking.
func (c *Conn) WriteMessage(messageType int, data []byte) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	return c.Conn.WriteMessage(messageType, data)
}

// ReadMessage reads a message from the client connection with proper locking.
func (c *Conn) ReadMessage() (int, []byte, error) {
	c.readLock.Lock()
	defer c.readLock.Unlock()
	return c.Conn.ReadMessage()
}
