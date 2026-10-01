package server

import (
	"math"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/l7mp/stunner/v2/internal/api"
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
	// Active reports, on an idle check, whether the entry moved traffic outside the
	// forwarder (the kernel offload); known is false when nobody knows.
	Active func() (moved, known bool)
	// OnError is called by the forwarder on a failed write, before the entry closes.
	OnError func(msg string)
	// OnClose is called once, when the entry closes, before its conns close.
	OnClose func(reason string)
}

// Entry is a tracked client conn and, when forwarded, its leg.
type Entry struct {
	Client api.Conn
	Leg    api.Conn // nil for a conn a server hands on, like TURN
	Hooks  EntryHooks

	key       AddrPair
	t         *Conntrack
	closed    atomic.Bool
	disarmed  atomic.Bool
	last      atomic.Int64 // UnixNano of the last activity
	timer     *time.Timer
	closeOnce sync.Once
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
		if e.Hooks.OnClose != nil {
			e.Hooks.OnClose(reason)
		}
		_ = e.Client.Close()
		if e.Leg != nil {
			_ = e.Leg.Close()
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
	elapsed := time.Duration(time.Now().UnixNano() - e.last.Load())
	if elapsed < e.t.idle {
		e.timer.Reset(e.t.idle - elapsed)
		return
	}
	if e.Hooks.Active != nil {
		if moved, known := e.Hooks.Active(); known && moved {
			e.Touch()
			e.timer.Reset(e.t.idle)
			return
		}
	}
	e.Close("idle timeout")
}
