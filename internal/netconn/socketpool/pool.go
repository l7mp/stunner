// Package socketpool binds the listener sockets of a UDP listener: a set of sockets sharing the
// same address through the reuse-port socket options, one per readloop thread, so the kernel
// spreads clients over them, or a single socket where that does not work out.
package socketpool

import (
	"context"
	"fmt"
	"net"

	"github.com/pion/transport/v5"
	"github.com/pion/transport/v5/reuseport"
	"github.com/pion/transport/v5/stdnet"
)

// ListenPacket binds the listener sockets at address: threadNum sockets sharing the address
// through pion's OS-dependent reuse-port control, falling back to a single plain socket when the
// shared bind fails at any point (the control cannot report whether the platform honours the
// options, so the bind is the probe). A vnet has no socket-option hook, so it always gets the
// single socket.
func ListenPacket(nw transport.Net, network, address string, threadNum int) ([]net.PacketConn, error) {
	if _, ok := nw.(*stdnet.Net); ok && threadNum > 1 {
		lc := net.ListenConfig{Control: reuseport.Control}
		conns := make([]net.PacketConn, 0, threadNum)
		for len(conns) < threadNum {
			conn, err := lc.ListenPacket(context.Background(), network, address)
			if err != nil {
				for _, c := range conns {
					_ = c.Close()
				}
				conns = nil
				break
			}
			conns = append(conns, conn)
		}
		if conns != nil {
			return conns, nil
		}
	}

	conn, err := nw.ListenPacket(network, address)
	if err != nil {
		return nil, fmt.Errorf("failed to create PacketConn at %s: %w", address, err)
	}
	return []net.PacketConn{conn}, nil
}
