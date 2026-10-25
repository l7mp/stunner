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

// Tag names the listener or cluster that created a conn, and the protocol its packets travel on:
// the listener's protocol, or for a cluster's conn TLS or DTLS when the cluster secures it and the
// TURN transport of the tunnel (TURN-UDP, TURN-TCP, TURN-TLS, TURN-DTLS) for a datagram conn
// through one. Labels are the labels of the listener, nil for none.
type Tag struct {
	Name   string
	Proto  stnrv2.Protocol
	Labels map[string]string
	// PublicAddr and PublicPort are the public address of the listener, where set.
	PublicAddr string
	PublicPort int
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
	// Tag is the tag of the conns the dialer makes.
	Tag() Tag
	// Dial connects to addr, from local if not nil.
	Dial(ctx context.Context, local net.Addr, addr string) (Conn, error)
	// ListenPacket makes a relay socket and returns the address it is advertised at.
	ListenPacket(network string, port int) (net.PacketConn, net.Addr, error)
	// Listen makes a relayed listener and returns the address it is advertised at.
	Listen(network string, port int) (net.Listener, net.Addr, error)
}

// Router lists the destinations of a cluster.
type Router interface {
	Named
	// Targets returns the endpoints of the cluster that carry a port, resolved: the destinations
	// a server dialing a peer of its own choice chooses from.
	Targets() []netip.AddrPort
}

// Filter admits or drops traffic by the address and port of its client or peer: Match returns the
// verdict of its first rule matching the address. Set Port to zero to ignore the port rules during
// Match.
type Filter interface {
	Named
	Match(a netip.AddrPort) (allow, matched bool)
}
