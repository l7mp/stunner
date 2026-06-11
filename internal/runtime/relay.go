package runtime

import (
	"errors"
	"net"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/pion/turn/v5"
)

// Relay is a protocol-specific upstream relay implementation.
type Relay interface {
	Validate() error
	AllocatePacketConn(turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error)
	AllocateListener(turn.AllocateListenerConfig) (net.Listener, net.Addr, error)
	AllocateConn(turn.AllocateConnConfig) (net.Conn, error)
	ClusterName() string
	Protocol() stnrv1.ClusterProtocol
	Match(peer net.IP, port int) bool
}

// ManagedRelay is a relay that has an explicit lifecycle.
type ManagedRelay interface {
	Relay
	Start() error
	Close() error
}

// NewUnsupportedNetOpError returns a net.OpError wrapping errors.ErrUnsupported.
func NewUnsupportedNetOpError(op, network string) error {
	return &net.OpError{Op: op, Net: network, Err: errors.ErrUnsupported}
}
