package turn

import (
	"net"

	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/v2/internal/relay"
)

// relayAddressGenerator hands the listener's relay to the pion TURN server. The relay returns
// classified transports the dataplane can ask about themselves; Go has no covariant returns, so
// this narrows them to the bare net.* types pion takes, and maps pion's allocation configs onto
// the relay's own parameters.
type relayAddressGenerator struct{ *relay.Relay }

var _ turn.RelayAddressGenerator = relayAddressGenerator{}

// Validate is called on server startup and confirms the generator is configured.
func (relayAddressGenerator) Validate() error { return nil }

func (g relayAddressGenerator) AllocatePacketConn(conf turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	c, addr, err := g.PacketConn(conf.Network, conf.RequestedPort)
	if err != nil {
		return nil, nil, err
	}
	return c, addr, nil
}

func (g relayAddressGenerator) AllocateConn(conf turn.AllocateConnConfig) (net.Conn, error) {
	c, err := g.Conn(conf.LocalAddr, conf.RemoteAddr)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (g relayAddressGenerator) AllocateListener(conf turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	l, addr, err := g.Listener(conf.Network, conf.RequestedPort)
	if err != nil {
		return nil, nil, err
	}
	return l, addr, nil
}
