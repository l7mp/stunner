package stunner

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

	"github.com/l7mp/stunner/internal/object"
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

type PortRangeChecker = func(addr net.Addr) (*object.Cluster, bool)

// RelayGen can be used to only allocate connections inside a defined target port
// range. A static ip address can be set.
type RelayGen struct {
	// Listener is the listener on behalf of which the relay address generator is created.
	Listener *object.Listener

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

func NewRelayGen(l *object.Listener, t *telemetry.Telemetry, logger logger.LoggerFactory) *RelayGen {
	return &RelayGen{
		Listener:     l,
		RelayAddress: l.Addr,
		// Empty string is IPADDR_ANY: on dual-stack hosts (Linux default with
		// net.ipv6.bindv6only=0) ListenPacket("udp", ":0") binds a single
		// socket reachable from both IPv4 and IPv6 peers. Earlier releases
		// hardcoded "0.0.0.0" here, which silently broke relays to IPv6-only
		// peers (e.g. IPv6-only EKS pods).
		Address:      "",
		ClusterCache: lru.New(ClusterCacheSize),
		Net:          l.Net,
		Logger:       logger,
		telemetry:    t,
	}
}

// Validate is called on server startup and confirms the RelayAddressGenerator is properly configured.
func (r *RelayGen) Validate() error {
	return nil
}

// AllocatePacketConn generates a new transport relay connection and returns the IP/Port to be
// returned to the client in the allocation response.
func (r *RelayGen) AllocatePacketConn(conf turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	requestedPort := conf.RequestedPort
	if requestedPort <= 1 || requestedPort > 2<<16-1 {
		requestedPort = 0
	}

	// pion/turn passes a family-pinned Network ("udp4" or "udp6") derived
	// from the allocation's RFC 6156 family. On Linux dual-stack hosts that
	// hint forces ListenPacket into a single-family socket regardless of how
	// the listener itself was bound -- so a relay allocated from an IPv6-only
	// Kubernetes cluster (where pion/turn defaults to the RFC 6156 IPv4
	// family because the Allocate request omits REQUESTED-ADDRESS-FAMILY) is
	// unreachable for the cluster's IPv6 backend pods.
	//
	// When the listener Address is the wildcard, override the network with
	// the family-neutral "udp" so a single relay socket binds [::] and
	// reaches both IPv4 and IPv6 peers. Pinned Address listeners are
	// untouched and keep RFC 6156 strict family behavior.
	//
	// End-to-end cross-family forwarding additionally requires pion/turn's
	// permission family check to accept v6 peers on a v4 allocation; see the
	// pion/turn upstream issue tracking that.
	network := conf.Network
	if r.Address == "" && (network == "udp4" || network == "udp6") {
		network = "udp"
	}
	conn, err := r.Net.ListenPacket(network, fmt.Sprintf("%s:%d", r.Address, requestedPort))
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
func (g *RelayGen) AllocateConn(conf turn.AllocateConnConfig) (net.Conn, error) {
	return nil, errTodo
}

// AllocateListener generates a new Listener to receive traffic on and the IP/Port to populate the
// allocation response with.
func (r *RelayGen) AllocateListener(conf turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	return nil, nil, errTodo
}

// GenPortRangeChecker finds the cluster that is responsible for routing the packet and checks
// whether the peer address is in the port range specified for the cluster. The RelayGen caches
// recent hits for simplicity.
func (s *Stunner) GenPortRangeChecker(g *RelayGen) PortRangeChecker {
	return func(addr net.Addr) (*object.Cluster, bool) {
		u, ok := addr.(*net.UDPAddr)
		if !ok {
			return nil, false
		}

		ip := u.IP.String()
		c, ok := g.ClusterCache.Get(ip)
		var cluster *object.Cluster
		if ok {
			// cache hit
			cluster = c.(*object.Cluster)
		} else {
			// route
			for _, r := range g.Listener.Routes {
				c := s.GetCluster(r)
				if c != nil && c.Route(u.IP) {
					cluster = c
					g.ClusterCache.Add(ip, c)
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
