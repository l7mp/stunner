package util

// code adopted from github.com/livekit/pkg/telemetry

import (
	"errors"
	"fmt"
	"net"

	"github.com/pion/transport/v3"
	"github.com/pion/transport/v3/stdnet"

	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/pkg/logger"
)

var (
	errInvalidName             = errors.New("RelayAddressGenerator: Name must be set")
	errRelayAddressInvalid     = errors.New("RelayAddressGenerator: invalid RelayAddress")
	errInvalidRelayPortRange   = errors.New("RelayAddressGenerator: invalid target port range [MinRelayPort:MaxRelayPort]")
	errListeningAddressInvalid = errors.New("RelayAddressGenerator: invalid ListeningAddress")
	errNilConn                 = errors.New("cannot allocate relay connection")
	errTodo                    = errors.New("relay to Net.Conn not implemented")
)

// RelayAddressGenerator can be used to only allocate connections inside a defined target port
// range. A static ip address can be set.
type RelayAddressGenerator struct {
	// ListenerName is the name of the listener this relay address generator belongs to. Note that
	// packets sent to/received from upstream cluster are reported with the name of the
	// *listener* that the packet belongs to, and not the cluster.
	ListenerName string

	// RelayAddress is the IP returned to the user when the relay is created.
	RelayAddress net.IP

	// MinRelayPort the minimum target port (inclusive).
	MinRelayPort int

	// MaxRelayPort the maximum target port (inclusive).
	MaxRelayPort int

	// Address is passed to Listen/ListenPacket when creating the Relay.
	Address string

	// Net is a pion/transport VNet, used for testing.
	Net transport.Net

	// Logger is a logger factory we can use to generate per-listener relay loggers.
	Logger *logger.LeveledLoggerFactory
}

// Validate is called on server startup and confirms the RelayAddressGenerator is properly configured.
func (r *RelayAddressGenerator) Validate() error {
	if r.ListenerName == "" {
		return errInvalidName
	}

	if r.Net == nil {
		r.Net, _ = stdnet.NewNet()
	}

	switch {
	case r.MinRelayPort == 0:
		return errInvalidRelayPortRange
	case r.MaxRelayPort == 0:
		return errInvalidRelayPortRange
	case r.MinRelayPort > r.MaxRelayPort:
		return errInvalidRelayPortRange
	case r.RelayAddress == nil:
		return errRelayAddressInvalid
	case r.Address == "":
		return errListeningAddressInvalid
	default:
		return nil
	}
}

// AllocatePacketConn generates a new transport relay connection and returns the IP/Port to be
// returned to the client in the allocation response.
func (r *RelayAddressGenerator) AllocatePacketConn(network string, requestedPort int) (net.PacketConn, net.Addr, error) {
	if requestedPort <= 1 || requestedPort > 2<<16-1 {
		// let the OS automatically assign a port
		requestedPort = 0
	}

	conn, err := r.Net.ListenPacket(network, fmt.Sprintf("%s:%d", r.Address, requestedPort))
	if err != nil {
		return nil, nil, err
	}

	conn = NewPortRangePacketConn(conn, r.ListenerName, r.MinRelayPort, r.MaxRelayPort,
		r.Logger.NewLogger(fmt.Sprintf("relay-%s", r.ListenerName)))

	// Decorate with a telemetry reporter.
	conn = telemetry.NewPacketConn(conn, r.ListenerName, telemetry.ClusterType)

	relayAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, nil, errNilConn
	}

	relayAddr.IP = r.RelayAddress
	return conn, relayAddr, nil
}

// AllocateConn generates a new Conn to receive traffic on and the IP/Port to populate the
// allocation response with
func (g *RelayAddressGenerator) AllocateConn(network string, requestedPort int) (net.Conn, net.Addr, error) {
	return nil, nil, errTodo
}
