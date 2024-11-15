package icetester

import (
	"sync"

	"github.com/gorilla/websocket"
)

type MessageType int

const (
	MessageTypeUnknown MessageType = iota
	MessageTypeIceCandidate
	MessageTypeOffer
	MessageTypeAnswer
	MessageTypeClose
)

type Message struct {
	Type MessageType `json:"type"`
	Data string      `json:"data"`
}

// ThreadSafeWriter represents a client WebSocket connection. An added lock guards the underlying connection
// from concurrent write to websocket connection errors.
type ThreadSafeWriter struct {
	*websocket.Conn
	readLock, writeLock sync.Mutex // for writemessage
}

// WriteMessage writes a message to the client connection with proper locking.
func (c *ThreadSafeWriter) WriteMessage(messageType int, data []byte) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	return c.Conn.WriteMessage(messageType, data)
}

func (c *ThreadSafeWriter) WriteJSON(v any) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()
	return c.Conn.WriteJSON(v)
}

// ReadMessage reads a message from the client connection with proper locking.
func (c *ThreadSafeWriter) ReadMessage() (int, []byte, error) {
	c.readLock.Lock()
	defer c.readLock.Unlock()
	return c.Conn.ReadMessage()
}
