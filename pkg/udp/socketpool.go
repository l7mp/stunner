package udp

import (
	"context"
	"net"

	"github.com/pion/transport/v5/reuseport"
)

// listenReusePort binds n sockets (at least one) of the OS network sharing address through the
// reuse-port options, falling back to a single plain socket when the shared bind fails at any
// point: the bind is the probe of whether the platform honours the options. The returned flag
// reports whether the sockets share the address, in which case more sockets can join it.
func listenReusePort(network, address string, n int) ([]net.PacketConn, bool, error) {
	lc := net.ListenConfig{Control: reuseport.Control}
	socks := make([]net.PacketConn, 0, max(n, 1))
	for len(socks) < max(n, 1) {
		sock, err := lc.ListenPacket(context.Background(), network, address)
		if err != nil {
			for _, s := range socks {
				_ = s.Close()
			}
			sock, err := net.ListenPacket(network, address) //nolint:noctx
			if err != nil {
				return nil, false, err
			}
			return []net.PacketConn{sock}, false, nil
		}
		socks = append(socks, sock)
	}
	return socks, true, nil
}
