package fifo

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// short is a context for the dials that must fail.
func short(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestFifo(t *testing.T) {
	ctx := context.Background()
	fs := New()
	_, err := fs.Listen(ctx, Network, "")
	assert.Error(t, err, "a fifo has a name")
	f, err := fs.Listen(ctx, Network, "rtp control/1")
	require.NoError(t, err, "any string names a fifo")
	_, err = fs.Listen(ctx, Network, "rtp control/1")
	assert.Error(t, err, "a name binds once")
	_, err = fs.DialContext(short(t), Network, "rtp control/2")
	assert.ErrorIs(t, err, errRefused, "nobody listens there")

	c, err := fs.DialContext(ctx, Network, "rtp control/1")
	require.NoError(t, err)
	s, err := f.Accept()
	require.NoError(t, err)
	assert.Equal(t, c.LocalAddr().String(), s.RemoteAddr().String())
	assert.Equal(t, "rtp control/1", c.RemoteAddr().String())
	assert.Equal(t, "fifo", c.RemoteAddr().Network())
	_, err = fs.Dial("udp", "rtp control/1")
	assert.Error(t, err, "only the fifo network")
	_, err = fs.ListenPacket(ctx, Network, "x")
	assert.Error(t, err, "no packet conns")

	// datagram boundaries survive, both ways
	_, err = c.Write([]byte("hello"))
	require.NoError(t, err)
	_, err = c.Write([]byte("world"))
	require.NoError(t, err)
	buf := make([]byte, 100)
	k, err := s.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf[:k]))
	k, err = s.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "world", string(buf[:k]))
	_, err = s.Write([]byte("back"))
	require.NoError(t, err)
	k, err = c.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "back", string(buf[:k]))

	// a read deadline times out
	require.NoError(t, c.SetReadDeadline(time.Now().Add(10*time.Millisecond)))
	_, err = c.Read(buf)
	assert.True(t, errors.Is(err, os.ErrDeadlineExceeded))
	var ne net.Error
	assert.True(t, errors.As(err, &ne) && ne.Timeout())
	require.NoError(t, c.SetReadDeadline(time.Time{}))

	// queued datagrams are delivered before the close is
	_, err = s.Write([]byte("last"))
	require.NoError(t, err)
	require.NoError(t, s.Close())
	k, err = c.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "last", string(buf[:k]))
	_, err = c.Read(buf)
	assert.ErrorIs(t, err, io.EOF)
	_, err = c.Write([]byte("x"))
	assert.Error(t, err)

	// a conn dialed but never accepted is closed with the fifo: its dialer sees the EOF
	waiting, err := fs.DialContext(ctx, Network, "rtp control/1")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	_, err = waiting.Read(buf)
	assert.ErrorIs(t, err, io.EOF)
	_, err = f.Accept()
	assert.ErrorIs(t, err, net.ErrClosed, "a closed fifo accepts nothing")

	// a closed fifo frees its name
	_, err = fs.DialContext(short(t), Network, "rtp control/1")
	assert.ErrorIs(t, err, errRefused)
	_, err = fs.Listen(ctx, Network, "rtp control/1")
	assert.NoError(t, err)
}

// TestFifoDial pins the accept queue of one conn and the dials that wait for a listener: before
// the first one and across a restart.
func TestFifoDial(t *testing.T) {
	ctx := context.Background()
	fs := New()

	// a dial before the listen waits for it
	dialed := make(chan error, 1)
	go func() { _, err := fs.DialContext(ctx, Network, "f"); dialed <- err }()
	time.Sleep(20 * time.Millisecond)
	f, err := fs.Listen(ctx, Network, "f")
	require.NoError(t, err)
	require.NoError(t, <-dialed, "the early dial is taken over")

	// the queue holds one conn: the next dial waits until it is accepted
	_, err = fs.DialContext(short(t), Network, "f")
	assert.ErrorIs(t, err, context.DeadlineExceeded, "the queue is full")
	_, err = f.Accept()
	require.NoError(t, err)

	// a dial waiting on a full queue survives a restart of the listener
	_, err = fs.DialContext(ctx, Network, "f")
	require.NoError(t, err)
	go func() { _, err := fs.DialContext(ctx, Network, "f"); dialed <- err }()
	time.Sleep(20 * time.Millisecond)
	require.NoError(t, f.Close())
	f, err = fs.Listen(ctx, Network, "f")
	require.NoError(t, err)
	require.NoError(t, <-dialed, "the waiting dial reaches the new listener")
	_, err = f.Accept()
	assert.NoError(t, err)
}
