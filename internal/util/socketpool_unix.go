//go:build unix

package util

import (
	"context"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/pion/transport/v2"
	"github.com/pion/transport/v2/stdnet"
)

// unixPacketConPool implements socketpools for unix with full support for SO_REUSEPORT
type unixPacketConnPool struct {
	net.ListenConfig
	size int
}

// NewPacketConnPool creates a new packet connection pool. Pooling is disabled if threadNum is zero
// or if we are running on top of transport.VNet (which does not support reuseport), or if we are
// on non-unix, see the fallback in socketpool.go.
func NewPacketConnPool(vnet transport.Net, threadNum int) PacketConnPool {
	// default to a single socket for vnet or if udp multithreading is disabled
	_, ok := vnet.(*stdnet.Net)
	if ok && threadNum > 0 {
		return &unixPacketConnPool{
			size: threadNum,
			ListenConfig: net.ListenConfig{
				Control: func(network, address string, conn syscall.RawConn) error {
					var operr error
					if err := conn.Control(func(fd uintptr) {
						operr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET,
							unix.SO_REUSEPORT, 1)
					}); err != nil {
						return err
					}

					return operr
				},
			},
		}
	} else {
		return &defaultPacketConnPool{Net: vnet}
	}
}

// Make creates a PacketConnPool, caller must make sure to close the sockets.
func (p *unixPacketConnPool) Make(network, address string) ([]net.PacketConn, error) {
	conns := []net.PacketConn{}
	for i := 0; i < p.size; i++ {
		conn, err := p.ListenPacket(context.Background(), network, address)
		// this will have to be converted to errors.Join once we bump Go dependency to
		// 1.20, for now we return on the first error that poccurred.
		if err != nil {
			return []net.PacketConn{}, fmt.Errorf("failed to create PacketConn "+
				"%d at %s (REUSEPORT: %t): %s", i, address, (p.size > 0), err)
		}
		conns = append(conns, conn)
	}

	return conns, nil
}

func (p *unixPacketConnPool) Size() int { return p.size }
