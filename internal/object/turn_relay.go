package object

// code adopted from github.com/livekit/pkg/telemetry

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v4"
	"github.com/pion/turn/v5"
	"k8s.io/utils/lru"

	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/pkg/logger"
)

const ClusterCacheSize = 512

var (
	errNilConn = errors.New("cannot allocate relay connection")
	errTodo    = errors.New("relay to Net.Conn not implemented")
)

var (
	ErrPortProhibited      = errors.New("peer port administratively prohibited")
	ErrInvalidPeerProtocol = errors.New("unknown peer transport protocol")
)

// TURNRelay can be used to only allocate connections inside a defined target port
// range. A static ip address can be set.
type TURNRelay struct {
	// Listener is the listener on behalf of which the relay address generator is created.
	*Listener

	// RelayAddress is the IP returned to the user when the relay is created.
	RelayAddress net.IP

	// Address is passed to Listen/ListenPacket when creating the Relay.
	Address string

	// ClusterCache is a cache that is used to couple relayed packets to clusters.
	ClusterCache *lru.Cache

	// PortRangeChecker is a callback to check whether a peer address is allowed by any of the
	// clusters of the listener.
	PortRangeChecker PortRangeChecker

	// Net is a pion/transport VNet, used for testing.
	Net transport.Net

	// Logger is a logger factory we can use to generate per-listener relay loggers.
	Logger logger.LoggerFactory

	telemetry *telemetry.Telemetry
}

func NewTURNRelay(l *Listener) *TURNRelay {
	r := &TURNRelay{
		Listener:     l,
		RelayAddress: l.Addr,
		// Empty string is IPADDR_ANY: on dual-stack hosts (Linux default with
		// net.ipv6.bindv6only=0) ListenPacket("udp", ":0") binds a single
		// socket reachable from both IPv4 and IPv6 peers. Earlier releases
		// hardcoded "0.0.0.0" here, which silently broke relays to IPv6-only
		// peers (e.g. IPv6-only EKS pods).
		Address:      "",
		ClusterCache: lru.New(ClusterCacheSize),
		telemetry:    l.telemetry,
		Net:          l.Net,
		Logger:       l.logger,
	}
	r.PortRangeChecker = GenPortRangeChecker(r)
	return r
}

// Validate is called on server startup and confirms the RelayAddressGenerator is properly configured.
func (r *TURNRelay) Validate() error {
	return nil
}

// AllocatePacketConn generates a new transport relay connection and returns the IP/Port to be
// returned to the client in the allocation response.
func (r *TURNRelay) AllocatePacketConn(conf turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	requestedPort := conf.RequestedPort
	if requestedPort <= 1 || requestedPort > 2<<16-1 {
		requestedPort = 0
	}

	// This will fail unless (1) r.Address is "" (IPADDR_ANY), or (2) r.Address is IPv4 and the
	// requested network is also IPv4, or (3) both are IPv6.
	conn, err := r.Net.ListenPacket(conf.Network, fmt.Sprintf("%s:%d", r.Address, requestedPort))
	if err != nil {
		return nil, nil, err
	}

	conn = NewPortRangePacketConn(conn, r.PortRangeChecker, r.telemetry,
		r.Logger.NewLogger(fmt.Sprintf("relay-%s", r.Listener.Name)))

	relayAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, nil, errNilConn
	}

	relayAddr.IP = r.RelayAddress
	return conn, relayAddr, nil
}

// AllocateConn generates a new outgoing TCP connection bound to the relay address.
func (g *TURNRelay) AllocateConn(conf turn.AllocateConnConfig) (net.Conn, error) {
	return nil, errTodo
}

// AllocateListener generates a new Listener to receive traffic on and the IP/Port to populate the
// allocation response with.
func (r *TURNRelay) AllocateListener(conf turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	return nil, nil, errTodo
}

type PortRangeChecker = func(addr net.Addr) (*Cluster, bool)

// GenPortRangeChecker finds the cluster that is responsible for routing the packet and checks
// whether the peer address is in the port range specified for the cluster. The relay's
// ClusterCache memoises recent hits; we re-resolve via the Registry on every miss, so
// reconcile-driven cluster changes propagate without a relay rebuild.
func GenPortRangeChecker(relay *TURNRelay) PortRangeChecker {
	return func(addr net.Addr) (*Cluster, bool) {
		u, ok := addr.(*net.UDPAddr)
		if !ok {
			return nil, false
		}
		ip := u.IP.String()
		var cluster *Cluster
		if c, ok := relay.ClusterCache.Get(ip); ok {
			cluster = c.(*Cluster)
		} else {
			for _, c := range relay.Listener.clustersForRoutes() {
				if c.Route(u.IP) {
					cluster = c
					relay.ClusterCache.Add(ip, c)
					break
				}
			}
		}
		if cluster != nil {
			return cluster, cluster.Match(u.IP, u.Port)
		}
		return nil, false
	}
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

// NewPortRangePacketConn decorates a PacketConn with filtering on a target port range. Errors are reported per listener name.
func NewPortRangePacketConn(c net.PacketConn, checker PortRangeChecker, t *telemetry.Telemetry, log logging.LeveledLogger) net.PacketConn {
	// cluster add/sub connection is not tracked
	// AddConnection(n, t)
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
	cluster, ok := c.checker(peerAddr)
	if !ok {
		return 0, ErrPortProhibited
	}

	n, err := c.PacketConn.WriteTo(p, peerAddr)
	if n > 0 {
		c.telemetry.IncrementBytes(cluster.Name, telemetry.ClusterType, telemetry.Outgoing, uint64(n))
		c.telemetry.IncrementPackets(cluster.Name, telemetry.ClusterType, telemetry.Outgoing, 1)
	}

	return n, err
}

// ReadFrom reads from the PortRangePacketConn. Blocks until a packet from the speciifed port range
// is received and drops all other packets.
func (c *PortRangePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		var peerAddr net.Addr

		err := c.PacketConn.SetReadDeadline(c.readDeadline)
		if err != nil {
			return 0, peerAddr, err
		}

		n, peerAddr, err := c.PacketConn.ReadFrom(p)

		// Return errors unconditionally: peerAddr will most probably not be valid anyway
		// so it is not worth checking
		if err != nil {
			return n, peerAddr, err
		}

		cluster, ok := c.checker(peerAddr)
		if !ok {
			continue
		}

		if n > 0 {
			c.telemetry.IncrementBytes(cluster.Name, telemetry.ClusterType, telemetry.Incoming, uint64(n))
			c.telemetry.IncrementPackets(cluster.Name, telemetry.ClusterType, telemetry.Incoming, 1)
		}

		return n, peerAddr, nil
	}
}

func (c *PortRangePacketConn) SetReadDeadline(t time.Time) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.readDeadline = t
	return nil
}

func (c *PortRangePacketConn) Close() error {
	// cluster add/sub connection is not tracked
	// SubConnection(c.name, c.connType)
	return c.PacketConn.Close()
}
