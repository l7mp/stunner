package server

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAcceptQueue pins that a full queue blocks the push until a conn is accepted, and that a close
// unblocks the push and closes the queued conns.
func TestAcceptQueue(t *testing.T) {
	q := NewAcceptQueue(Backlog)
	peers := make([]net.Conn, 0, Backlog)
	for range Backlog {
		c, peer := net.Pipe()
		peers = append(peers, peer)
		require.NoError(t, q.Push(c))
	}

	pushed := make(chan error, 1)
	c, _ := net.Pipe()
	go func() { pushed <- q.Push(c) }()
	select {
	case <-pushed:
		require.FailNow(t, "a push on a full backlog returned")
	case <-time.After(50 * time.Millisecond):
	}

	accepted, err := q.Accept()
	require.NoError(t, err)
	_ = accepted.Close()
	select {
	case err := <-pushed:
		require.NoError(t, err, "an accept makes room")
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the push did not resume")
	}

	c2, _ := net.Pipe()
	go func() { pushed <- q.Push(c2) }()
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, q.Close())
	select {
	case err := <-pushed:
		assert.ErrorIs(t, err, ErrNotServing, "a close unblocks the push")
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the close did not unblock the push")
	}

	// the conns left in the backlog are closed: their peers read EOF
	_, err = peers[len(peers)-1].Read(make([]byte, 1))
	assert.ErrorIs(t, err, io.EOF)
}
