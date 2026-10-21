package flow

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/offload"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Conntrack tracks the client conns of a server, keyed by their 5-tuple, each with an idle timer.
// Close closes every entry it ever tracked and refuses new ones.
type Conntrack struct {
	idle    time.Duration
	mu      sync.Mutex
	entries map[AddrPair]*Entry
	closed  bool
}

// NewConntrack creates a conntrack whose entries time out after idle.
func NewConntrack(idle time.Duration) *Conntrack {
	return &Conntrack{idle: idle, entries: map[AddrPair]*Entry{}}
}

// AddrPair is the conntrack key of a conn: its remote and local address and port.
type AddrPair struct{ Remote, Local netip.AddrPort }

// Key returns the conntrack key of a conn. A non-IP address, such as stdio's, is the zero value.
func Key(remote, local net.Addr) AddrPair { return AddrPair{addrPort(remote), addrPort(local)} }

func addrPort(a net.Addr) netip.AddrPort {
	var ap netip.AddrPort
	switch a := a.(type) {
	case *net.UDPAddr:
		ap = a.AddrPort()
	case *net.TCPAddr:
		ap = a.AddrPort()
	default:
		return ap
	}
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}

// EntryHooks are a server's callbacks on one entry.
type EntryHooks struct {
	// OnError is called by the forwarder on a failed write, before the entry closes.
	OnError func(msg string)
	// OnClose is called once, when the entry closes, before its conns close.
	OnClose func(reason string)
}

// Entry is a tracked client conn and, when forwarded or offloaded, its leg.
type Entry struct {
	Client api.Conn
	// Leg is the conn the client conn is relayed to: set before Add for a forwarded entry, by
	// SetLeg later for an entry the server relays itself; nil for a conn a server hands on, like
	// TURN.
	Leg   api.Conn
	Hooks EntryHooks

	key       AddrPair
	t         *Conntrack
	closed    atomic.Bool
	disarmed  atomic.Bool
	last      atomic.Int64 // UnixNano of the last activity
	timer     *time.Timer
	closeOnce sync.Once

	offMu sync.Mutex // guards a leg set by SetLeg, and the offload
	off   *offloaded // the kernel offload of the entry, nil if not offloaded
}

// SetLeg sets the leg of an entry the server relays itself, so that it can be offloaded.
func (e *Entry) SetLeg(leg api.Conn) {
	e.offMu.Lock()
	e.Leg = leg
	e.offMu.Unlock()
}

// offloaded is an entry handed to the kernel offload engine: its two sides as registered, and the
// engine's packet counter as of the last idle check.
type offloaded struct {
	engine offload.Engine
	a, b   offload.Connection
	pkts   uint64
}

// Add tracks an entry under its client's key and arms its idle timer, replacing a stale entry of
// the same key; false if the conntrack is closed, and the caller keeps the conns.
func (t *Conntrack) Add(e *Entry) bool {
	e.key, e.t = Key(e.Client.RemoteAddr(), e.Client.LocalAddr()), t
	e.Touch()
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return false
	}
	stale := t.entries[e.key]
	// armed before the entry is published, so a concurrent Close stops it, and set before it can
	// fire, since the callback uses it
	e.timer = time.AfterFunc(math.MaxInt64, e.checkIdle)
	e.timer.Reset(t.idle)
	t.entries[e.key] = e
	t.mu.Unlock()
	if stale != nil {
		stale.Close("replaced")
	}
	return true
}

// Get returns the entry of a key.
func (t *Conntrack) Get(key AddrPair) (*Entry, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[key]
	return e, ok
}

// Remove untracks the entry of a key without closing it.
func (t *Conntrack) Remove(key AddrPair) {
	t.mu.Lock()
	e, ok := t.entries[key]
	if ok {
		delete(t.entries, key)
	}
	t.mu.Unlock()
	if ok {
		e.timer.Stop()
	}
}

// Len returns the number of tracked entries.
func (t *Conntrack) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
}

// Close closes every entry and refuses new ones.
func (t *Conntrack) Close() {
	t.mu.Lock()
	t.closed = true
	entries := make([]*Entry, 0, len(t.entries))
	for _, e := range t.entries {
		entries = append(entries, e)
	}
	t.mu.Unlock()
	for _, e := range entries {
		e.Close("server shutting down")
	}
}

// Touch records activity on the entry.
func (e *Entry) Touch() { e.last.Store(time.Now().UnixNano()) }

// Disarm stops the idle timer for good: something else owns the entry's lifetime from now on.
func (e *Entry) Disarm() {
	e.disarmed.Store(true)
	e.timer.Stop()
}

// Closed reports whether the entry has closed.
func (e *Entry) Closed() bool { return e.closed.Load() }

// Close closes the entry and its conns once, and untracks it.
func (e *Entry) Close(reason string) {
	e.closeOnce.Do(func() {
		e.timer.Stop()
		e.closed.Store(true)
		e.offMu.Lock()
		if o := e.off; o != nil {
			e.off = nil
			_ = o.engine.Remove(o.a, o.b)
		}
		leg := e.Leg
		e.offMu.Unlock()
		if e.Hooks.OnClose != nil {
			e.Hooks.OnClose(reason)
		}
		_ = e.Client.Close()
		if leg != nil {
			_ = leg.Close()
		}
		e.t.mu.Lock()
		if e.t.entries[e.key] == e {
			delete(e.t.entries, e.key)
		}
		e.t.mu.Unlock()
	})
}

// checkIdle keeps the entry if it moved traffic within the idle timeout, through the forwarder or
// the kernel offload, and closes it otherwise.
func (e *Entry) checkIdle() {
	if e.disarmed.Load() {
		return
	}
	if elapsed := time.Since(e.LastActive()); elapsed < e.t.idle {
		e.timer.Reset(e.t.idle - elapsed)
		return
	}
	e.Close("idle timeout")
}

// LastActive returns the time of the entry's last activity, in user space or, for an offloaded
// entry, in the kernel: the kernel's traffic since the last call counts as activity now.
func (e *Entry) LastActive() time.Time {
	e.offMu.Lock()
	if o := e.off; o != nil {
		if pkts, known := o.engine.Packets(o.a, o.b); known && pkts != o.pkts {
			o.pkts = pkts
			e.Touch()
		}
	}
	e.offMu.Unlock()
	return time.Unix(0, e.last.Load())
}

// ErrNotOffloadable is the error of Offload on an entry the kernel cannot carry.
var ErrNotOffloadable = errors.New("not offloadable")

// OffloadBackoff and OffloadAttempts pace Offload on a leg tunnelled through TURN-UDP: the TURN
// client assigns the channel towards the peer at the first write and binds it a round trip later.
var (
	OffloadBackoff  = 100 * time.Millisecond
	OffloadAttempts = 5
)

// Offload hands the entry's datagrams to the kernel engine, both directions: a packet from the
// client goes out on the leg's wire and back, the packets matching the punt rules excepted. It
// takes a UDP client conn and a UDP or TURN-UDP leg, the latter once its channel towards the peer
// is bound, which Offload waits for, so it may block for a few seconds. Anything else, and a
// refusal of the engine (such as the offload limit), leaves the entry in user space. Offloading an
// offloaded entry again replaces its offload, for a leg that changed. An offloaded entry's idle
// check counts the kernel's traffic too, and closing the entry removes it from the engine.
func (e *Entry) Offload(engine offload.Engine, punt ...offload.PuntRule) error {
	e.offMu.Lock()
	leg := e.Leg
	e.offMu.Unlock()
	if leg == nil || e.Client.Tag().Proto != stnrv2.ProtocolUDP ||
		leg.Tag().Proto != stnrv2.ProtocolUDP && leg.Tag().Proto != stnrv2.ProtocolTURNUDP {
		return fmt.Errorf("%w: not a UDP client conn relayed through a UDP or TURN-UDP leg",
			ErrNotOffloadable)
	}
	cl, cr := e.Client.TransportAddrs()
	ll, lr := leg.TransportAddrs()
	var ch uint16
	// a tunnelled leg's wire runs to the TURN server, framed by the channel towards the peer
	if leg.Tag().Proto == stnrv2.ProtocolTURNUDP {
		bound := false
		for backoff, i := OffloadBackoff, 0; i < OffloadAttempts && !bound; backoff, i = backoff*2, i+1 {
			time.Sleep(backoff)
			if e.closed.Load() {
				return net.ErrClosed
			}
			ch, bound = leg.Channel(leg.RemoteAddr())
		}
		if !bound {
			return fmt.Errorf("%w: no channel bound towards the peer", ErrNotOffloadable)
		}
	}
	o := &offloaded{engine: engine,
		a: offload.Connection{RemoteAddr: cr, LocalAddr: cl, Protocol: offload.ProtocolUDP, Punt: punt},
		b: offload.Connection{RemoteAddr: lr, LocalAddr: ll, Protocol: offload.ProtocolUDP,
			ChannelID: uint32(ch), Punt: punt}}
	e.offMu.Lock()
	defer e.offMu.Unlock()
	if e.closed.Load() {
		return net.ErrClosed
	}
	if old := e.off; old != nil {
		e.off = nil
		_ = old.engine.Remove(old.a, old.b)
	}
	if err := engine.Upsert(o.a, o.b, e.Client.Tag().Name, leg.Tag().Name); err != nil {
		return err
	}
	e.off = o
	return nil
}
