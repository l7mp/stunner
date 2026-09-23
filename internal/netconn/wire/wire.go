// Package wire is the lowest layer of relay transport; it knows it's transport addresses and
// channel (maybe a fake channel for plane l4 connection). This is the minimum set of accessors
// offload will need.
package wire

import "net"

// PacketConn is a datagram transport that can describe its wire.
type PacketConn interface {
	net.PacketConn
	// TransportAddrs returns the wire addresses traffic actually uses: the local socket it
	// leaves from, and the fixed remote everything is sent to, or a nil remote when
	// datagrams go straight to the peer they name.
	TransportAddrs() (local, remote net.Addr)
	// Channel returns the channel number traffic towards peer is currently framed with, and
	// whether that framing is live on the wire. A transport that never frames, or one that
	// does not carry peer, returns (0, false).
	Channel(peer net.Addr) (uint16, bool)
}

// Direct adapts a kernel socket: datagrams go straight to the peer, so there is no fixed remote
// and no framing.
func Direct(c net.PacketConn) PacketConn { return direct{c} }

type direct struct{ net.PacketConn }

func (d direct) TransportAddrs() (local, remote net.Addr) { return d.LocalAddr(), nil }
func (direct) Channel(net.Addr) (uint16, bool)            { return 0, false }
