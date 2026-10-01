package turn

import (
	"context"
	"errors"
	"net"

	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/v2/internal/api"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

var errNoCluster = errors.New("no cluster of the server can serve the allocation")

// relayAddressGenerator makes relay transports through the first of the server's clusters that can
// make one.
type relayAddressGenerator struct{ s *Server }

var _ turn.RelayAddressGenerator = relayAddressGenerator{}

func (relayAddressGenerator) Validate() error { return nil }

func (g relayAddressGenerator) AllocatePacketConn(conf turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	for _, d := range g.s.dialers() {
		c, addr, err := d.ListenPacket(conf.Network, conf.RequestedPort)
		if errors.Is(err, api.ErrNotSupported) {
			continue
		}
		return c, addr, err
	}
	return nil, nil, errNoCluster
}

// AllocateConn dials a Connect's peer, already permitted, through the cluster that made the
// relayed listener: the first TCP cluster.
func (g relayAddressGenerator) AllocateConn(conf turn.AllocateConnConfig) (net.Conn, error) {
	for _, d := range g.s.dialers() {
		if d.Protocol() == stnrv2.ProtocolTCP {
			return d.Dial(context.Background(), conf.LocalAddr, conf.RemoteAddr.String())
		}
	}
	return nil, errNoCluster
}

func (g relayAddressGenerator) AllocateListener(conf turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	for _, d := range g.s.dialers() {
		l, addr, err := d.Listen(conf.Network, conf.RequestedPort)
		if errors.Is(err, api.ErrNotSupported) {
			continue
		}
		return l, addr, err
	}
	return nil, nil, errNoCluster
}
