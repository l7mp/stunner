// Package turn holds the TURN dialer of mediactl: conns to peers relayed through a TURN server,
// over a UDP allocation or an RFC 6062 TCP connection.
package turn

import (
	"context"
	"fmt"
	"net"

	"github.com/pion/logging"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/utils/turnclient"
)

// Dialer opens conns to peers relayed through a TURN server.
type Dialer struct {
	// URI is the TURN server, turn:<host>:<port>?transport=udp|tcp, or turns: for TLS and DTLS.
	URI string
	// Username and Password are the long-term credentials, none if empty.
	Username, Password string
	// Insecure skips the verification of the TURN server's certificate.
	Insecure bool
	// LoggerFactory makes the TURN client's logger; nil logs nothing.
	LoggerFactory logging.LoggerFactory
}

// DialContext opens a conn to the peer at address through the TURN server: what it reads comes
// from the peer, what it writes goes to the peer, its local address is the relayed address. The
// network "tcp" relays over an RFC 6062 TCP connection, which needs a TCP or TLS transport, any
// other network over a UDP allocation. Closing the conn ends the TURN session.
func (d Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	u, err := stnrv2.ParseURI(d.URI)
	if err != nil {
		return nil, err
	}
	if !u.Protocol.IsTURN() {
		return nil, fmt.Errorf("not a TURN URI: %q", d.URI)
	}
	td := turnclient.Dialer{Config: turnclient.Config{
		Protocol:      u.Protocol,
		ServerAddr:    u.HostPort(),
		Username:      d.Username,
		Password:      d.Password,
		ServerName:    u.Host,
		Insecure:      d.Insecure,
		LoggerFactory: d.LoggerFactory,
	}}
	if network == "tcp" {
		return td.DialContext(ctx, "tcp", address)
	}
	peer, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, err
	}
	pc, err := td.ListenPacket(ctx, "udp", "")
	if err != nil {
		return nil, err
	}
	return &peerConn{PacketConn: pc, peer: peer}, nil
}

// peerConn is the conn to one peer through a relay.
type peerConn struct {
	net.PacketConn
	peer net.Addr
}

func (c *peerConn) Read(p []byte) (int, error) {
	for {
		n, from, err := c.ReadFrom(p)
		if err != nil || from.String() == c.peer.String() {
			return n, err
		}
	}
}

func (c *peerConn) Write(p []byte) (int, error) { return c.WriteTo(p, c.peer) }

func (c *peerConn) RemoteAddr() net.Addr { return c.peer }
