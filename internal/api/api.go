// Package api holds the contracts between the dataplane objects, which refer to each other by name.
package api

import (
	"context"
	"errors"
	"net"
	"net/netip"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// ErrNotSupported is returned for a request a Dialer cannot serve.
var ErrNotSupported = errors.New("not supported")

// Named is an object with a name.
type Named interface {
	Name() string
}

// Tag names the listener or cluster that created a conn, and its protocol.
type Tag struct {
	Name  string
	Proto stnrv2.Protocol
}

// Conn is a connected transport with its tag and the wire facts the offload engine needs.
type Conn interface {
	net.Conn
	Tag() Tag
	// TransportAddrs returns the local and remote addresses of the transport on the wire.
	TransportAddrs() (local, remote net.Addr)
	// Channel returns the TURN channel the traffic towards peer is framed with, if any.
	Channel(peer net.Addr) (uint16, bool)
}

// Verdict is what a server did with a conn.
type Verdict int

const (
	// Consumed means the server took the conn.
	Consumed Verdict = iota
	// Next means the returned conn goes to the next server.
	Next
)

// Server takes client conns.
type Server interface {
	Named
	Start() error
	Close(shutdown bool) error
	// Serve takes a conn, and may block until it can. On error the caller keeps the conn.
	Serve(c Conn) (Conn, Verdict, error)
	// Sessions returns the number of live sessions: TURN allocations or L4 flows.
	Sessions() int
}

// Dialer is the transport that reaches the peers of a cluster. What it cannot do fails with
// ErrNotSupported.
type Dialer interface {
	Named
	Start() error
	Close(shutdown bool) error
	// Protocol is the network the peers are reached over.
	Protocol() stnrv2.Protocol
	// Dial connects to addr, from local if not nil.
	Dial(ctx context.Context, local net.Addr, addr string) (Conn, error)
	// ListenPacket makes a relay socket and returns the address it is advertised at.
	ListenPacket(network string, port int) (net.PacketConn, net.Addr, error)
	// Listen makes a relayed listener and returns the address it is advertised at.
	Listen(network string, port int) (net.Listener, net.Addr, error)
}

// Router decides where the traffic of a cluster goes.
type Router interface {
	Named
	// Route returns the destination of traffic towards dst, or chooses one when dst is the zero
	// value. ok is false when there is none, err when the router cannot route the request.
	Route(dst netip.AddrPort) (netip.AddrPort, bool, error)
}

// Listener emits client conns.
type Listener interface {
	Named
	Start() error
	Close() error
}
