package telemetry

// code adopted from github.com/livekit/pkg/telemetry

import (
	"errors"
	"fmt"
	"net"

	"github.com/pion/randutil"
	"github.com/pion/transport/v3"
	"github.com/pion/transport/v3/stdnet"
)

var (
	errInvalidName             = errors.New("RelayAddressGenerator: Name must be set")
	errRelayAddressInvalid     = errors.New("RelayAddressGenerator: invalid RelayAddress")
	errMinPortNotZero          = errors.New("RelayAddressGenerator: MinPort must be not 0")
	errMaxPortNotZero          = errors.New("RelayAddressGenerator: MaxPort must be not 0")
	errListeningAddressInvalid = errors.New("RelayAddressGenerator: invalid ListeningAddress")
	errNilConn                 = errors.New("cannot allocate relay connection")
	errMaxRetriesExceeded      = errors.New("max retries exceeded when trying to generate new relay connection: MinPort:MaxPort range too small?")
	errTodo                    = errors.New("relay to Net.Conn  not implemented")
)

// RelayAddressGenerator can be used to only allocate connections inside a defined port range. A
// static ip address can be set.
type RelayAddressGenerator struct {
	// Name is the name of the listener this relay address generator belongs to. Note that
	// packets sent to/received from upstream cluster are reported with the name of the
	// *listener* that the packet belongs to, and not the cluster.
	Name string

	// RelayAddress is the IP returned to the user when the relay is created.
	RelayAddress net.IP

	// MinPort the minimum port to allocate.
	MinPort uint16
	// MaxPort the maximum (inclusive) port to allocate.
	MaxPort uint16

	// MaxRetries the amount of tries to allocate a random port in the defined range.
	MaxRetries int

	// Rand the random source of numbers.
	Rand randutil.MathRandomGenerator

	// Address is passed to Listen/ListenPacket when creating the Relay.
	Address string

	// Net is a pion/transport VNet, used for testing.
	Net transport.Net
}

// Validate is called on server startup and confirms the RelayAddressGenerator is properly configured
func (r *RelayAddressGenerator) Validate() error {
	if r.Name == "" {
		return errInvalidName
	}

	if r.Net == nil {
		r.Net, _ = stdnet.NewNet()
	}

	if r.Rand == nil {
		r.Rand = randutil.NewMathRandomGenerator()
	}

	if r.MaxRetries == 0 {
		r.MaxRetries = 10
	}

	switch {
	case r.MinPort == 0:
		return errMinPortNotZero
	case r.MaxPort == 0:
		return errMaxPortNotZero
	case r.RelayAddress == nil:
		return errRelayAddressInvalid
	case r.Address == "":
		return errListeningAddressInvalid
	default:
		return nil
	}
}

// AllocatePacketConn generates a new PacketConn to receive traffic on and the IP/Port to populate
// the allocation response with
func (r *RelayAddressGenerator) AllocatePacketConn(network string, requestedPort int) (net.PacketConn, net.Addr, error) {
	if requestedPort != 0 {
		conn, err := r.Net.ListenPacket(network, fmt.Sprintf("%s:%d", r.Address, requestedPort))
		if err != nil {
			return nil, nil, err
		}

		conn = NewPacketConn(conn, r.Name, ClusterType)

		relayAddr, ok := conn.LocalAddr().(*net.UDPAddr)
		if !ok {
			return nil, nil, errNilConn
		}

		relayAddr.IP = r.RelayAddress
		return conn, relayAddr, nil
	}

	for try := 0; try < r.MaxRetries; try++ {
		port := r.MinPort + uint16(r.Rand.Intn(int((r.MaxPort+1)-r.MinPort)))
		conn, err := r.Net.ListenPacket(network, fmt.Sprintf("%s:%d", r.Address, port))
		if err != nil {
			continue
		}

		conn = NewPacketConn(conn, r.Name, ClusterType)

		relayAddr, ok := conn.LocalAddr().(*net.UDPAddr)
		if !ok {
			return nil, nil, errNilConn
		}

		relayAddr.IP = r.RelayAddress
		return conn, relayAddr, nil
	}

	return nil, nil, errMaxRetriesExceeded
}

// AllocateConn generates a new Conn to receive traffic on and the IP/Port to populate the
// allocation response with
func (g *RelayAddressGenerator) AllocateConn(network string, requestedPort int) (net.Conn, net.Addr, error) {
	return nil, nil, errTodo
}
