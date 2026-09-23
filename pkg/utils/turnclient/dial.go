// Package turnclient opens client sessions on a TURN server over any of the TURN transports
// (TURN-UDP, TURN-TCP, TURN-TLS, TURN-DTLS). It is the TURN-client machinery under the TURN-*
// protocol relay clusters, and the client the tests and tools use to drive traffic through a
// STUNner gateway.
package turnclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"

	"github.com/pion/dtls/v3"
	"github.com/pion/logging"
	"github.com/pion/turn/v5"

	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	a12n "github.com/l7mp/stunner/v2/pkg/authentication"
)

// Config specifies the upstream TURN server to dial.
type Config struct {
	// Protocol is the transport to reach the TURN server over: TURN-UDP, TURN-TCP, TURN-TLS
	// or TURN-DTLS.
	Protocol stnrv1.Protocol
	// ServerAddr is the TURN server address in host:port form. The host may be an IP address
	// of either family or a DNS name.
	ServerAddr string
	// Username and Password are the optional long-term credentials.
	Username, Password string
	// Realm is the STUN/TURN realm.
	Realm string
	// ServerName is the SNI server name for the TLS/DTLS transports (unless it is an IP
	// address).
	ServerName string
	// Insecure allows TLS certificate verification to be skipped for the TLS/DTLS transports.
	Insecure bool
	// LoggerFactory is an optional external logger.
	LoggerFactory logging.LoggerFactory
}

// NewConfig builds a dialer config for the upstream TURN server named in a cluster config, reached
// over the given transport. Credentials are generated fresh on every call, so with "ephemeral"
// auth each dial (that is, each allocation) authenticates with its own time-windowed credential.
// Callers add their own LoggerFactory.
//
// Realm is a seed only: the server states its realm in the 401 challenge and the client overwrites
// the configured value with it before computing the message integrity.
func NewConfig(s *stnrv1.TURNServer, proto stnrv1.Protocol) (Config, error) {
	user, pass, err := a12n.GenerateCredentials(s.Auth)
	if err != nil {
		return Config{}, fmt.Errorf("failed to generate credentials for TURN server %q: %w",
			s.HostPort(), err)
	}

	// the SNI override wins; otherwise the address doubles as the server name (no SNI for
	// an IP literal: the TLS stack ignores ServerName in that case)
	serverName := s.Address
	if s.SNI != "" {
		serverName = s.SNI
	}

	c := Config{
		Protocol:   proto,
		ServerAddr: s.HostPort(),
		Username:   user,
		Password:   pass,
		ServerName: serverName,
		Insecure:   s.Insecure,
	}
	if s.Auth != nil {
		c.Realm = s.Auth.Realm
	}
	return c, nil
}

// Dialer opens client sessions on the TURN server named in its Config, with net-shaped entry
// points: ListenPacket mirrors net.ListenConfig.ListenPacket and returns the session's UDP
// allocation, DialContext mirrors net.Dialer.DialContext and returns an RFC 6062 relayed TCP
// connection to a peer. Either conn owns the whole TURN session: closing it tears down the
// allocation, the TURN client, and its transport. The context is checked between the dial steps;
// the underlying TURN library does not support mid-step cancellation.
type Dialer struct {
	Config
}

// ListenPacket makes a UDP allocation on the TURN server and returns it as the session's packet
// conn: ReadFrom/WriteTo relay through the server (the client auto-creates a server-side
// permission for each new peer written to), and LocalAddr is the server-relayed transport
// address. The network and local address of the allocation are the server's business, so both
// parameters are ignored; they exist to mirror net.ListenConfig.ListenPacket.
func (d Dialer) ListenPacket(ctx context.Context, _, _ string) (*PacketConn, error) {
	client, transport, err := dial(d.Config)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		client.Close()
		transport.Close() //nolint:errcheck
		return nil, err
	}

	relay, err := client.Allocate()
	if err != nil {
		client.Close()
		transport.Close() //nolint:errcheck
		return nil, fmt.Errorf("upstream allocation failed: %w", err)
	}

	// the wire runs over UDP for the turn-udp/turn-dtls transports and over TCP otherwise;
	// resolution succeeded inside dial, so it cannot fail here
	var server net.Addr
	switch d.Protocol {
	case stnrv1.ProtocolTURNUDP, stnrv1.ProtocolTURNDTLS:
		server, _ = net.ResolveUDPAddr("udp", d.ServerAddr)
	default:
		server, _ = net.ResolveTCPAddr("tcp", d.ServerAddr)
	}

	return &PacketConn{PacketConn: relay, client: client, transport: transport, server: server}, nil
}

// BindingRequest sends a STUN binding request over a fresh session and returns the reflexive
// address the server saw, closing the session afterwards. It needs no credentials: a STUN
// server answers it without authentication.
func (d Dialer) BindingRequest(ctx context.Context) (net.Addr, error) {
	client, transport, err := dial(d.Config)
	if err != nil {
		return nil, err
	}
	defer transport.Close() //nolint:errcheck
	defer client.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	addr, err := client.SendBindingRequest()
	if err != nil {
		return nil, fmt.Errorf("binding request failed: %w", err)
	}
	return addr, nil
}

// DialContext makes a TCP (RFC 6062) allocation on the TURN server and opens a relayed connection
// to the peer at address (host:port). Only "tcp" is a valid network, and only over TURN-TCP and
// TURN-TLS: RFC 6062 requires the control connection to be TCP or TLS.
func (d Dialer) DialContext(ctx context.Context, network, address string) (*Conn, error) {
	if network != "tcp" {
		return nil, fmt.Errorf("TURN relayed connections are TCP only, got network %q", network)
	}
	switch d.Protocol {
	case stnrv1.ProtocolTURNTCP, stnrv1.ProtocolTURNTLS:
	default:
		return nil, fmt.Errorf("TCP relaying requires a TURN-TCP or TURN-TLS transport, got %q",
			d.Protocol.String())
	}

	client, transport, err := dial(d.Config)
	if err != nil {
		return nil, err
	}
	closeSession := func() {
		client.Close()
		transport.Close() //nolint:errcheck
	}
	if err := ctx.Err(); err != nil {
		closeSession()
		return nil, err
	}

	alloc, err := client.AllocateTCP()
	if err != nil {
		closeSession()
		return nil, fmt.Errorf("upstream TCP allocation failed: %w", err)
	}
	// AllocateTCP's allocation is not torn down by client.Close(); close it explicitly.
	closeSession = func() {
		_ = alloc.Close()
		client.Close()
		transport.Close() //nolint:errcheck
	}

	var dataConn net.Conn
	switch d.Protocol {
	case stnrv1.ProtocolTURNTCP:
		dataConn, err = alloc.Dial("tcp", address)
	case stnrv1.ProtocolTURNTLS:
		// the RFC 6062 data connection follows the control transport: dial it over TLS
		// ourselves and hand it to the allocation.
		var serverConn net.Conn
		serverConn, err = tls.Dial("tcp", d.ServerAddr, &tls.Config{
			MinVersion:         tls.VersionTLS12,
			ServerName:         d.ServerName,
			InsecureSkipVerify: d.Insecure, //nolint:gosec
		})
		if err == nil {
			dataConn, err = alloc.DialWithConn(serverConn, "tcp", address)
		}
	}
	if err != nil {
		closeSession()
		return nil, fmt.Errorf("upstream relayed connection to peer %s failed: %w", address, err)
	}

	return &Conn{Conn: dataConn, closeSession: closeSession}, nil
}

// dial connects the transport to the TURN server, builds the TURN client, and starts its read
// loop. The returned packet connection is the transport conn owned by the client: close the
// client first and the conn after. The network family is chosen from the resolved server
// address, so both IPv4 and IPv6 (and DNS-named) servers work.
func dial(c Config) (*turn.Client, net.PacketConn, error) {
	var turnConn net.PacketConn

	switch c.Protocol {
	case stnrv1.ProtocolTURNUDP:
		udpAddr, err := net.ResolveUDPAddr("udp", c.ServerAddr)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to resolve TURN server address %q: %w",
				c.ServerAddr, err)
		}
		t, err := net.ListenPacket(networkFamily("udp", udpAddr.IP), ":0")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to allocate TURN listening packet socket: %w", err)
		}
		turnConn = t
	case stnrv1.ProtocolTURNTCP:
		conn, err := net.Dial("tcp", c.ServerAddr)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to allocate TURN socket: %w", err)
		}
		turnConn = turn.NewSTUNConn(conn)
	case stnrv1.ProtocolTURNTLS:
		conn, err := tls.Dial("tcp", c.ServerAddr, &tls.Config{
			MinVersion:         tls.VersionTLS12,
			ServerName:         c.ServerName,
			InsecureSkipVerify: c.Insecure, //nolint:gosec
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to allocate TURN/TLS socket: %w", err)
		}
		turnConn = turn.NewSTUNConn(conn)
	case stnrv1.ProtocolTURNDTLS:
		udpAddr, err := net.ResolveUDPAddr("udp", c.ServerAddr)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to resolve TURN server address %q: %w",
				c.ServerAddr, err)
		}
		conn, err := dtls.DialWithOptions(networkFamily("udp", udpAddr.IP), udpAddr,
			dtls.WithInsecureSkipVerify(c.Insecure),
			dtls.WithServerName(c.ServerName),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to allocate TURN/DTLS socket: %w", err)
		}
		turnConn = turn.NewSTUNConn(conn)
	default:
		return nil, nil, fmt.Errorf("unknown TURN server protocol %q", c.Protocol.String())
	}

	client, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: c.ServerAddr,
		TURNServerAddr: c.ServerAddr,
		Conn:           turnConn,
		Username:       c.Username,
		Password:       c.Password,
		Realm:          c.Realm,
		LoggerFactory:  c.LoggerFactory,
	})
	if err != nil {
		turnConn.Close() //nolint:errcheck
		return nil, nil, fmt.Errorf("failed to allocate TURN client: %w", err)
	}

	// Start the client read loop (also drives allocation/permission refreshes).
	if err := client.Listen(); err != nil {
		client.Close()
		turnConn.Close() //nolint:errcheck
		return nil, nil, fmt.Errorf("failed to listen on TURN client: %w", err)
	}

	return client, turnConn, nil
}

// PacketConn is a UDP allocation that owns its TURN session: Close closes the allocation, then
// the TURN client and its transport. TransportAddrs and Channel describe the session's wire, so
// the dataplane can stack its relay leg on the session as it is.
type PacketConn struct {
	net.PacketConn // the upstream allocation
	client         *turn.Client
	transport      net.PacketConn
	server         net.Addr
	once           sync.Once
}

func (c *PacketConn) Close() error {
	err := c.PacketConn.Close()
	c.once.Do(func() {
		c.client.Close()
		c.transport.Close() //nolint:errcheck
	})
	return err
}

// TransportAddrs returns the wire addresses of the TURN session: the transport socket's local
// address and the server's address. This is the 5-tuple a kernel offload must match on; the
// allocation's own LocalAddr is the server-side relayed address, useless for local offload.
func (c *PacketConn) TransportAddrs() (local, remote net.Addr) {
	return c.transport.LocalAddr(), c.server
}

// channelFinder is the channel accessor set of the pion client relay conn. The concrete type
// lives in the library's internal packages, so the only way to reach these is structurally.
type channelFinder interface {
	FindChannelNumberByAddr(addr net.Addr) (uint16, bool)
	IsChannelActive(chNum uint16) bool
}

// Channel reports the channel the session frames traffic towards peer with, and whether that
// framing is live. The client assigns a number at the first write towards a peer and completes
// the ChannelBind a round trip later, sending Send indications until it does, so a number alone
// does not mean the wire carries ChannelData yet.
func (c *PacketConn) Channel(peer net.Addr) (uint16, bool) {
	f, ok := c.PacketConn.(channelFinder)
	if !ok {
		return 0, false
	}
	num, ok := f.FindChannelNumberByAddr(peer)
	if !ok || !f.IsChannelActive(num) {
		return 0, false
	}
	return num, true
}

// Conn is an RFC 6062 relayed TCP connection that owns its TURN session: Close closes the data
// connection, then the allocation, the TURN client, and its transport.
type Conn struct {
	net.Conn
	closeSession func()
	once         sync.Once
}

func (c *Conn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.closeSession)
	return err
}

// TransportAddrs returns the wire addresses of the relayed connection: the RFC 6062 data
// connection runs to the TURN server, so the remote is the server, not the peer.
func (c *Conn) TransportAddrs() (local, remote net.Addr) {
	return c.LocalAddr(), c.RemoteAddr()
}

// Channel reports no framing: RFC 6062 relays peer traffic over a dedicated data connection,
// there are no channels on a stream allocation.
func (c *Conn) Channel(net.Addr) (uint16, bool) { return 0, false }

// networkFamily narrows a base network ("udp") to the family of the server IP, so the local
// socket family matches the server: a dual-stack wildcard socket would source packets to an IPv4
// server from an IPv4-mapped IPv6 address, which some transports refuse.
func networkFamily(base string, ip net.IP) string {
	if ip == nil {
		return base
	}
	if ip.To4() != nil {
		return base + "4"
	}
	return base + "6"
}
