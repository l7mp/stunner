// Package upstream defines the transports the dataplane uses to reach the peers of a cluster.
// A plain cluster is reached directly from a relay socket, a TURN-* cluster through the upstream
// TURN server the cluster names; both look the same from above.
package upstream

import "net"

// BaseConn is what every upstream transport can answer about itself, whether it carries one peer
// or many, datagrams or a stream.
type BaseConn interface {
	// TransportAddrs returns the wire addresses traffic actually uses: the local socket it
	// leaves from, and the fixed remote everything is sent to, or a nil remote when traffic
	// goes straight to the peer. A kernel offload matches on these; LocalAddr on a TURN
	// session is the server-side relayed address, useless for a local match.
	TransportAddrs() (local, remote net.Addr)
	// Channel reports the channel number traffic towards peer is currently framed with, and
	// whether that framing is live on the wire. A transport that never frames, or one that
	// does not carry peer, returns (0, false).
	Channel(peer net.Addr) (uint16, bool)
}

// PacketConn is the multi-peer datagram transport of an allocation: the peer travels with every
// datagram, so one conn serves every peer routed to the same cluster.
type PacketConn interface {
	net.PacketConn
	BaseConn
}

// Conn is the single-peer stream transport: the peer is fixed when the conn is opened, so it
// never routes.
type Conn interface {
	net.Conn
	BaseConn
}
