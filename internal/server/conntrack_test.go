package server

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/netconn"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// addrConn is a pipe end with a remote address of its own, so test entries get distinct keys.
type addrConn struct {
	net.Conn
	remote net.Addr
}

func (c addrConn) RemoteAddr() net.Addr { return c.remote }

func newTestEntry(port int, hooks EntryHooks) *Entry {
	client, leg := net.Pipe()
	remote := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: port}
	return &Entry{
		Client: netconn.NewConn(addrConn{Conn: client, remote: remote},
			api.Tag{Name: "li", Proto: stnrv2.ProtocolUDP}, nil),
		Leg:   netconn.NewConn(leg, api.Tag{Name: "cl", Proto: stnrv2.ProtocolUDP}, nil),
		Hooks: hooks,
	}
}

// TestConntrackIdle pins the idle decision: an entry quiet in user space survives only if the
// Active hook reports traffic moved outside the forwarder, and an entry busy in user space
// survives without asking.
func TestConntrackIdle(t *testing.T) {
	for _, c := range []struct {
		name         string
		moved, known bool
		survives     bool
	}{
		{name: "moved outside the forwarder", moved: true, known: true, survives: true},
		{name: "known but nothing moved", known: true},
		{name: "nobody knows"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ct := NewConntrack(time.Minute)
			e := newTestEntry(1, EntryHooks{Active: func() (bool, bool) { return c.moved, c.known }})
			require.True(t, ct.Add(e))
			e.last.Store(time.Now().Add(-2 * time.Minute).UnixNano())
			e.checkIdle()
			assert.Equal(t, !c.survives, e.Closed(), "entry torn down")
			assert.Equal(t, c.survives, ct.Len() == 1, "entry still tracked")
			ct.Close()
		})
	}

	t.Run("busy in user space", func(t *testing.T) {
		ct := NewConntrack(time.Minute)
		e := newTestEntry(1, EntryHooks{Active: func() (bool, bool) {
			t.Error("an entry busy in user space must not ask")
			return false, true
		}})
		require.True(t, ct.Add(e))
		e.checkIdle()
		assert.False(t, e.Closed())
		ct.Close()
	})

	t.Run("disarmed", func(t *testing.T) {
		ct := NewConntrack(10 * time.Millisecond)
		e := newTestEntry(1, EntryHooks{})
		require.True(t, ct.Add(e))
		e.Disarm()
		time.Sleep(50 * time.Millisecond)
		assert.False(t, e.Closed(), "a disarmed entry never times out")
		ct.Close()
	})
}

// TestConntrackTable pins the keyed table: lookup by the client's 5-tuple, Remove untracks without
// closing, a new entry of the same key replaces and closes the stale one, and Close tears down
// every entry, with its hook, and refuses new ones.
func TestConntrackTable(t *testing.T) {
	ct := NewConntrack(time.Minute)
	reasons := make(chan string, 4)
	hooks := EntryHooks{OnClose: func(r string) { reasons <- r }}

	a, b := newTestEntry(1, hooks), newTestEntry(2, hooks)
	require.True(t, ct.Add(a))
	require.True(t, ct.Add(b))
	got, ok := ct.Get(Key(a.Client.RemoteAddr(), a.Client.LocalAddr()))
	require.True(t, ok)
	assert.Same(t, a, got)

	ct.Remove(Key(b.Client.RemoteAddr(), b.Client.LocalAddr()))
	assert.False(t, b.Closed(), "Remove does not close")
	assert.Equal(t, 1, ct.Len())

	a2 := newTestEntry(1, hooks)
	require.True(t, ct.Add(a2))
	assert.True(t, a.Closed(), "the stale entry of the key is closed")
	assert.Equal(t, "replaced", <-reasons)
	assert.Equal(t, 1, ct.Len())

	ct.Close()
	assert.True(t, a2.Closed())
	assert.Equal(t, "server shutting down", <-reasons)
	assert.Zero(t, ct.Len())
	assert.False(t, ct.Add(newTestEntry(3, EntryHooks{})), "a closed conntrack refuses entries")
}
