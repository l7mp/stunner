//go:build !linux

package util

import (
	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/pion/transport/v3"
)

// NewPacketConnPool creates a new packet connection pool which is fixed to a single connection,
// used if threadNum is zero or if we are running on top of transport.VNet (which does not support
// reuseport), or if we are on non-unix, see the fallback in socketpool.go.
func NewPacketConnPool(listenerName string, vnet transport.Net, threadNum int, t *telemetry.Telemetry) PacketConnPool {
	// default to a single socket for vnet or if udp multithreading is disabled
	return &defaultPacketConnPool{
		Net:          vnet,
		listenerName: listenerName,
		telemetry:    t,
	}
}
