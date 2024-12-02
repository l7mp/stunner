package util

import (
	"fmt"
	"net"

	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/pion/transport/v3"
)

// PacketConnPool is a factory to create pools of related PacketConns, which may either be a set of
// PacketConns bound to the same local IP using SO_REUSEPORT (on unix, under certain circumstances)
// that can do multithreaded readloops, or a single PacketConn as a fallback for non-unic
// architectures and for testing.
type PacketConnPool interface {
	// Make creates a PacketConnPool, caller must make sure to close the sockets.
	Make(network, address string) ([]net.PacketConn, error)
	// Size returns the number of sockets in the pool.
	Size() int
}

// defaultPacketConPool implements a socketpool that consists of only a single socket, used as a
// fallback for architectures that do not support SO_REUSEPORT or when socket pooling is disabled.
type defaultPacketConnPool struct {
	transport.Net
	listenerName string
	telemetry    *telemetry.Telemetry
}

// Make creates a PacketConnPool, caller must make sure to close the sockets.
func (p *defaultPacketConnPool) Make(network, address string) ([]net.PacketConn, error) {
	conns := []net.PacketConn{}

	conn, err := p.ListenPacket(network, address)
	if err != nil {
		return []net.PacketConn{}, fmt.Errorf("failed to create PacketConn at %s "+
			"(REUSEPORT: false): %s", address, err)
	}

	conn = telemetry.NewPacketConn(conn, p.listenerName, telemetry.ListenerType, p.telemetry)
	conns = append(conns, conn)
	return conns, nil
}

func (p *defaultPacketConnPool) Size() int { return 1 }
