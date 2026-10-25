package turn

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/netconn"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

var errNoCluster = errors.New("no cluster of the server can serve the allocation")

// relayAddressGenerator makes relay transports through the first of the server's clusters that can
// make one, admitting the peers the filters of the server admit.
type relayAddressGenerator struct{ s *Server }

var _ turn.RelayAddressGenerator = relayAddressGenerator{}

func (relayAddressGenerator) Validate() error { return nil }

func (g relayAddressGenerator) AllocatePacketConn(conf turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	for _, d := range g.s.dialers() {
		c, addr, err := d.ListenPacket(conf.Network, conf.RequestedPort)
		if errors.Is(err, api.ErrNotSupported) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		return netconn.FilterPacketConn(c, g.s.Admits), addr, nil
	}
	return nil, nil, errNoCluster
}

// AllocateConn dials a Connect's peer, already permitted, through the cluster that made the
// relayed listener: the first TCP cluster.
func (g relayAddressGenerator) AllocateConn(conf turn.AllocateConnConfig) (net.Conn, error) {
	if !g.s.Admits(conf.RemoteAddr) {
		return nil, fmt.Errorf("peer %s refused by the filters of server %s", conf.RemoteAddr,
			g.s.Name())
	}
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
		if err != nil {
			return nil, nil, err
		}
		return netconn.FilterListener(l, g.s.Admits), addr, nil
	}
	return nil, nil, errNoCluster
}
