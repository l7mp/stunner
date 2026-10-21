package flow

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/netconn"
	"github.com/l7mp/stunner/v2/internal/offload"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// addrConn is a pipe end with addresses of its own, so test entries get distinct keys and UDP
// wires.
type addrConn struct {
	net.Conn
	local, remote net.Addr
}

func (c addrConn) LocalAddr() net.Addr  { return c.local }
func (c addrConn) RemoteAddr() net.Addr { return c.remote }

func udpAddr(last byte, port int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(192, 0, 2, last), Port: port}
}

// newTestEntry makes an entry of a UDP client from port and a direct UDP leg, or a leg over wire if
// wire is not nil.
func newTestEntry(port int, wire netconn.Wire) *Entry {
	client, leg := net.Pipe()
	// a leg with a wire of its own runs through a TURN-UDP tunnel
	legTag := api.Tag{Name: "cl", Proto: stnrv2.ProtocolUDP}
	if wire != nil {
		legTag.Proto = stnrv2.ProtocolTURNUDP
	}
	return &Entry{
		Client: netconn.NewConn(addrConn{Conn: client, local: udpAddr(2, 3478), remote: udpAddr(1, port)},
			api.Tag{Name: "li", Proto: stnrv2.ProtocolUDP}, nil),
		Leg: netconn.NewConn(addrConn{Conn: leg, local: udpAddr(2, 40000), remote: udpAddr(9, 9000)},
			legTag, wire),
	}
}

// fakeEngine records the flows offloaded and reports a settable packet counter.
type fakeEngine struct {
	offload.NullEngine
	mu      sync.Mutex
	flows   map[string][2]offload.Connection
	pkts    uint64
	known   bool
	refuse  error
	removed int
}

func newFakeEngine() *fakeEngine { return &fakeEngine{flows: map[string][2]offload.Connection{}} }

func (f *fakeEngine) Upsert(a, b offload.Connection, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse != nil {
		return f.refuse
	}
	f.flows[a.String()] = [2]offload.Connection{a, b}
	return nil
}

func (f *fakeEngine) Remove(a, _ offload.Connection) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.flows, a.String())
	f.removed++
	return nil
}

func (f *fakeEngine) Packets(_, _ offload.Connection) (uint64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pkts, f.known
}

// TestConntrackIdle pins the idle decision: an entry quiet in user space survives only if the
// kernel moved its packets since the last check, and an entry busy in user space survives without
// asking.
func TestConntrackIdle(t *testing.T) {
	for _, c := range []struct {
		name     string
		pkts     uint64
		known    bool
		survives bool
	}{
		{name: "moved in the kernel", pkts: 10, known: true, survives: true},
		{name: "known but nothing moved", known: true},
		{name: "the engine does not know"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ct := NewConntrack(time.Minute)
			e := newTestEntry(1, nil)
			require.True(t, ct.Add(e))
			engine := newFakeEngine()
			require.NoError(t, e.Offload(engine))
			engine.pkts, engine.known = c.pkts, c.known
			e.last.Store(time.Now().Add(-2 * time.Minute).UnixNano())
			e.checkIdle()
			assert.Equal(t, !c.survives, e.Closed(), "entry torn down")
			assert.Equal(t, c.survives, ct.Len() == 1, "entry still tracked")
			ct.Close()
		})
	}

	t.Run("the same counter twice", func(t *testing.T) {
		ct := NewConntrack(time.Minute)
		e := newTestEntry(1, nil)
		require.True(t, ct.Add(e))
		engine := newFakeEngine()
		require.NoError(t, e.Offload(engine))
		engine.pkts, engine.known = 10, true
		e.last.Store(time.Now().Add(-2 * time.Minute).UnixNano())
		e.checkIdle()
		require.False(t, e.Closed(), "moved once")
		e.last.Store(time.Now().Add(-2 * time.Minute).UnixNano())
		e.checkIdle()
		assert.True(t, e.Closed(), "a counter that stood still is idle")
		ct.Close()
	})

	t.Run("busy in user space", func(t *testing.T) {
		ct := NewConntrack(time.Minute)
		e := newTestEntry(1, nil)
		require.True(t, ct.Add(e))
		e.checkIdle()
		assert.False(t, e.Closed())
		ct.Close()
	})

	t.Run("disarmed", func(t *testing.T) {
		ct := NewConntrack(10 * time.Millisecond)
		e := newTestEntry(1, nil)
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

	a, b := newTestEntry(1, nil), newTestEntry(2, nil)
	a.Hooks, b.Hooks = hooks, hooks
	require.True(t, ct.Add(a))
	require.True(t, ct.Add(b))
	got, ok := ct.Get(Key(a.Client.RemoteAddr(), a.Client.LocalAddr()))
	require.True(t, ok)
	assert.Same(t, a, got)

	ct.Remove(Key(b.Client.RemoteAddr(), b.Client.LocalAddr()))
	assert.False(t, b.Closed(), "Remove does not close")
	assert.Equal(t, 1, ct.Len())

	a2 := newTestEntry(1, nil)
	a2.Hooks = hooks
	require.True(t, ct.Add(a2))
	assert.True(t, a.Closed(), "the stale entry of the key is closed")
	assert.Equal(t, "replaced", <-reasons)
	assert.Equal(t, 1, ct.Len())

	ct.Close()
	assert.True(t, a2.Closed())
	assert.Equal(t, "server shutting down", <-reasons)
	assert.Zero(t, ct.Len())
	assert.False(t, ct.Add(newTestEntry(3, nil)), "a closed conntrack refuses entries")
}

// tunnelWire is the wire of a leg through a TURN-UDP server, with the channel bound after the
// given number of polls.
type tunnelWire struct {
	mu    sync.Mutex
	polls int
}

func (w *tunnelWire) TransportAddrs() (local, remote net.Addr) {
	return udpAddr(2, 50000), udpAddr(7, 3478)
}

func (w *tunnelWire) Channel(net.Addr) (uint16, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.polls--
	return 0x4001, w.polls < 0
}

// TestEntryOffload pins the flows Offload hands to the engine, and the ones it refuses.
func TestEntryOffload(t *testing.T) {
	defer func(b time.Duration) { OffloadBackoff = b }(OffloadBackoff)
	OffloadBackoff = time.Millisecond
	punt := offload.PuntRule{Offset: 0, Mask: 0xffff0000, Value: 0x80050000}

	t.Run("direct", func(t *testing.T) {
		ct := NewConntrack(time.Minute)
		defer ct.Close()
		e := newTestEntry(1, nil)
		require.True(t, ct.Add(e))
		engine := newFakeEngine()
		require.NoError(t, e.Offload(engine, punt))
		require.Len(t, engine.flows, 1)
		for _, f := range engine.flows {
			assert.Equal(t, udpAddr(1, 1).String(), f[0].RemoteAddr.String(), "the client")
			assert.Equal(t, udpAddr(2, 3478).String(), f[0].LocalAddr.String(), "the listener")
			assert.Equal(t, udpAddr(9, 9000).String(), f[1].RemoteAddr.String(), "the peer")
			assert.Equal(t, udpAddr(2, 40000).String(), f[1].LocalAddr.String(), "the leg's socket")
			assert.Zero(t, f[0].ChannelID)
			assert.Zero(t, f[1].ChannelID, "a direct leg is raw")
			assert.Equal(t, []offload.PuntRule{punt}, f[0].Punt)
			assert.Equal(t, []offload.PuntRule{punt}, f[1].Punt)
		}
		e.Close("test")
		assert.Empty(t, engine.flows, "closing the entry removes the offload")
	})

	t.Run("tunnelled", func(t *testing.T) {
		ct := NewConntrack(time.Minute)
		defer ct.Close()
		e := newTestEntry(1, &tunnelWire{polls: 2})
		require.True(t, ct.Add(e))
		engine := newFakeEngine()
		require.NoError(t, e.Offload(engine), "the channel binds on the third poll")
		for _, f := range engine.flows {
			assert.Equal(t, udpAddr(7, 3478).String(), f[1].RemoteAddr.String(), "the TURN server")
			assert.Equal(t, udpAddr(2, 50000).String(), f[1].LocalAddr.String(), "the transport")
			assert.Equal(t, uint32(0x4001), f[1].ChannelID)
		}
	})

	t.Run("refused", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			entry func() *Entry
		}{
			{"a channel never bound", func() *Entry { return newTestEntry(1, &tunnelWire{polls: 100}) }},
			{"a secured leg", func() *Entry {
				e := newTestEntry(1, nil)
				e.Leg = netconn.NewConn(e.Leg, api.Tag{Name: "cl", Proto: stnrv2.ProtocolDTLS}, nil)
				return e
			}},
			{"a tunnel over TURN-TCP", func() *Entry {
				e := newTestEntry(1, nil)
				e.Leg = netconn.NewConn(e.Leg, api.Tag{Name: "cl", Proto: stnrv2.ProtocolTURNTCP},
					&tunnelWire{})
				return e
			}},
			{"no leg", func() *Entry { e := newTestEntry(1, nil); e.Leg = nil; return e }},
			{"a TCP client", func() *Entry {
				e := newTestEntry(1, nil)
				e.Client = netconn.NewConn(e.Client, api.Tag{Name: "li", Proto: stnrv2.ProtocolTCP}, nil)
				return e
			}},
		} {
			t.Run(c.name, func(t *testing.T) {
				ct := NewConntrack(time.Minute)
				defer ct.Close()
				e := c.entry()
				require.True(t, ct.Add(e))
				engine := newFakeEngine()
				assert.ErrorIs(t, e.Offload(engine), ErrNotOffloadable)
				assert.Empty(t, engine.flows)
			})
		}
	})

	t.Run("refused by the engine", func(t *testing.T) {
		ct := NewConntrack(time.Minute)
		defer ct.Close()
		e := newTestEntry(1, nil)
		require.True(t, ct.Add(e))
		engine := newFakeEngine()
		engine.refuse = offload.ErrOffloadLimit
		assert.ErrorIs(t, e.Offload(engine), offload.ErrOffloadLimit)
		e.Close("test")
		assert.Zero(t, engine.removed, "nothing offloaded, nothing to remove")
	})

	t.Run("a new leg replaces the offload", func(t *testing.T) {
		ct := NewConntrack(time.Minute)
		defer ct.Close()
		e := newTestEntry(1, nil)
		leg := e.Leg
		e.Leg = nil
		require.True(t, ct.Add(e))
		engine := newFakeEngine()
		assert.ErrorIs(t, e.Offload(engine), ErrNotOffloadable, "no leg yet")
		e.SetLeg(leg)
		require.NoError(t, e.Offload(engine))
		require.NoError(t, e.Offload(engine), "again, the leg changed")
		assert.Equal(t, 1, engine.removed, "the old offload is removed")
		assert.Len(t, engine.flows, 1)
	})

	t.Run("kernel activity", func(t *testing.T) {
		ct := NewConntrack(time.Minute)
		defer ct.Close()
		e := newTestEntry(1, nil)
		require.True(t, ct.Add(e))
		engine := newFakeEngine()
		require.NoError(t, e.Offload(engine))
		old := time.Now().Add(-time.Hour)
		e.last.Store(old.UnixNano())
		assert.Equal(t, old.UnixNano(), e.LastActive().UnixNano(), "the kernel moved nothing")
		engine.pkts, engine.known = 5, true
		assert.WithinDuration(t, time.Now(), e.LastActive(), time.Second, "the kernel moved packets")
	})

	t.Run("closed", func(t *testing.T) {
		ct := NewConntrack(time.Minute)
		defer ct.Close()
		e := newTestEntry(1, nil)
		require.True(t, ct.Add(e))
		e.Close("test")
		engine := newFakeEngine()
		assert.True(t, errors.Is(e.Offload(engine), net.ErrClosed))
		assert.Empty(t, engine.flows)
	})
}
