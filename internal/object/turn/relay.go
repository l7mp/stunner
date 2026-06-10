package turn

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v4"
	"github.com/pion/turn/v5"

	objruntime "github.com/l7mp/stunner/internal/runtime"
	"github.com/l7mp/stunner/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

var (
	errNilConn = errors.New("cannot allocate relay connection")
)

var (
	ErrPortProhibited = errors.New("peer port administratively prohibited")
)

// Relay can be used to only allocate connections inside a defined target port range.
type Relay struct {
	listenerName string
	runtime      *objruntime.Runtime

	// RelayAddress is the IP returned to the user when the relay is created.
	RelayAddress net.IP

	// Address is passed to Listen/ListenPacket when creating the relay.
	Address string

	// PortRangeChecker is a callback to check whether a peer address is allowed.
	PortRangeChecker PortRangeChecker

	// Net is a pion/transport VNet, used for testing.
	Net transport.Net

	// Logger is a logger factory we can use to generate per-listener relay loggers.
	Logger logger.LoggerFactory

	telemetry *telemetry.Telemetry
}

// NewRelay creates a relay address generator for a listener context.
func NewRelay(listener string, rt *objruntime.Runtime) *Relay {
	conf := rt.GetConfig(objruntime.TypeListener, listener).(*stnrv1.ListenerConfig)
	ip := net.ParseIP(conf.Addr)
	if ip == nil && conf.Addr == "localhost" {
		ip = net.ParseIP("127.0.0.1")
	}
	if ip == nil {
		panic(fmt.Sprintf("turn: invalid listener address for %q: %s", listener, conf.Addr))
	}

	r := &Relay{
		listenerName: listener,
		runtime:      rt,
		RelayAddress: ip,
		Address:      "0.0.0.0",
		telemetry:    rt.Telemetry,
		Net:          rt.Net,
		Logger:       rt.Logger,
	}
	r.PortRangeChecker = GenPortRangeChecker(r)
	return r
}

// ClusterName returns the listener-local relay name.
func (r *Relay) ClusterName() string { return r.listenerName }

// Protocol returns the downstream listener protocol family used by this relay.
func (r *Relay) Protocol() stnrv1.ClusterProtocol { return stnrv1.ClusterProtocolUDP }

// Validate is called on server startup and confirms the RelayAddressGenerator is properly configured.
func (r *Relay) Validate() error {
	return nil
}

// AllocatePacketConn generates a new transport relay connection and returns the IP/Port to be
// returned to the client in the allocation response.
func (r *Relay) AllocatePacketConn(conf turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	requestedPort := conf.RequestedPort
	if requestedPort <= 1 || requestedPort > 2<<16-1 {
		requestedPort = 0
	}

	conn, err := r.Net.ListenPacket(conf.Network, net.JoinHostPort(r.Address, strconv.Itoa(requestedPort)))
	if err != nil {
		return nil, nil, err
	}

	conn = NewPortRangePacketConn(conn, r.PortRangeChecker, r.telemetry,
		r.Logger.NewLogger(fmt.Sprintf("relay-%s", r.listenerName)))

	relayAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, nil, errNilConn
	}

	relayAddr.IP = r.RelayAddress
	return conn, relayAddr, nil
}

// AllocateConn generates a new outgoing TCP connection bound to the relay address.
func (r *Relay) AllocateConn(conf turn.AllocateConnConfig) (net.Conn, error) {
	relay, ok := objruntime.SelectConnRelay(r.runtime, r.listenerName, conf.RemoteAddr)
	if !ok {
		return nil, ErrPortProhibited
	}
	return relay.AllocateConn(conf)
}

// AllocateListener generates a new listener to receive traffic on and the IP/Port to populate the
// allocation response with.
func (r *Relay) AllocateListener(conf turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	relay, ok := objruntime.SelectListenerRelay(r.runtime, r.listenerName, clusterProtocolFromNetwork(conf.Network))
	if !ok {
		return nil, nil, ErrPortProhibited
	}

	l, addr, err := relay.AllocateListener(conf)
	if err != nil {
		return nil, nil, err
	}

	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		tcpAddr.IP = r.RelayAddress
		return l, tcpAddr, nil
	}

	if udpAddr, ok := addr.(*net.UDPAddr); ok {
		udpAddr.IP = r.RelayAddress
		return l, udpAddr, nil
	}

	return l, addr, nil
}

// PortRangeChecker validates peer addresses and returns a matched cluster name.
type PortRangeChecker = func(addr net.Addr) (string, bool)

// GenPortRangeChecker resolves peer permissions through runtime router and validates peer ports
// against the selected cluster.
func GenPortRangeChecker(relay *Relay) PortRangeChecker {
	return func(addr net.Addr) (string, bool) {
		u, ok := addr.(*net.UDPAddr)
		if !ok {
			return "", false
		}

		lconf := relay.runtime.GetConfig(objruntime.TypeListener, relay.listenerName).(*stnrv1.ListenerConfig)
		r, ok := relay.runtime.Router.Route(relay.listenerName, lconf.Routes, u.IP, u.Port)
		if !ok {
			return "", false
		}

		return r.ClusterName(), true
	}
}

// clusterProtocolFromNetwork maps a transport network string to a cluster protocol.
func clusterProtocolFromNetwork(network string) stnrv1.ClusterProtocol {
	if strings.HasPrefix(network, "udp") {
		return stnrv1.ClusterProtocolUDP
	}
	return stnrv1.ClusterProtocolTCP
}

// PortRangePacketConn is a net.PacketConn that filters on the target port range and also handles
// telemetry.
type PortRangePacketConn struct {
	net.PacketConn
	checker      PortRangeChecker
	readDeadline time.Time
	telemetry    *telemetry.Telemetry
	lock         sync.Mutex
	log          logging.LeveledLogger
}

// NewPortRangePacketConn decorates a PacketConn with filtering on a target port range.
func NewPortRangePacketConn(c net.PacketConn, checker PortRangeChecker, t *telemetry.Telemetry, log logging.LeveledLogger) net.PacketConn {
	r := PortRangePacketConn{
		PacketConn: c,
		checker:    checker,
		telemetry:  t,
		log:        log,
	}

	return &r
}

// WriteTo writes to the PacketConn.
func (c *PortRangePacketConn) WriteTo(p []byte, peerAddr net.Addr) (int, error) {
	clusterName, ok := c.checker(peerAddr)
	if !ok {
		return 0, ErrPortProhibited
	}

	n, err := c.PacketConn.WriteTo(p, peerAddr)
	if n > 0 {
		c.telemetry.IncrementBytes(clusterName, telemetry.ClusterType, telemetry.Outgoing, uint64(n))
		c.telemetry.IncrementPackets(clusterName, telemetry.ClusterType, telemetry.Outgoing, 1)
	}

	return n, err
}

// ReadFrom reads from the PortRangePacketConn.
func (c *PortRangePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		var peerAddr net.Addr

		err := c.PacketConn.SetReadDeadline(c.readDeadline)
		if err != nil {
			return 0, peerAddr, err
		}

		n, peerAddr, err := c.PacketConn.ReadFrom(p)
		if err != nil {
			return n, peerAddr, err
		}

		clusterName, ok := c.checker(peerAddr)
		if !ok {
			continue
		}

		if n > 0 {
			c.telemetry.IncrementBytes(clusterName, telemetry.ClusterType, telemetry.Incoming, uint64(n))
			c.telemetry.IncrementPackets(clusterName, telemetry.ClusterType, telemetry.Incoming, 1)
		}

		return n, peerAddr, nil
	}
}

// SetReadDeadline stores the read deadline used by ReadFrom.
func (c *PortRangePacketConn) SetReadDeadline(t time.Time) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.readDeadline = t
	return nil
}

// Close closes the wrapped packet connection.
func (c *PortRangePacketConn) Close() error {
	return c.PacketConn.Close()
}
