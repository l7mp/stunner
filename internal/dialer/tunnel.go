package dialer

import (
	"context"
	"fmt"
	"net"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/utils/turnclient"
)

// client returns a TURN client of the tunnel, with fresh per-allocation credentials of the live
// authentication policy the tunnel names.
func (b *base) client() (turnclient.Dialer, error) {
	auth := b.rt.AuthConfig(b.tunnel.Auth)
	if b.tunnel.Auth != "" && auth == nil {
		return turnclient.Dialer{}, fmt.Errorf("tunnel of cluster %q: %w %q", b.name,
			stnrv2.ErrNoSuchAuth, b.tunnel.Auth)
	}
	conf, err := turnclient.NewConfig(b.tunnel, auth)
	if err != nil {
		return turnclient.Dialer{}, err
	}
	conf.LoggerFactory, conf.Net = b.rt.Logger, b.rt.Net
	return turnclient.Dialer{Config: conf}, nil
}

// allocate makes a UDP allocation on the upstream server of the tunnel.
func (b *base) allocate(ctx context.Context) (*turnclient.PacketConn, error) {
	td, err := b.client()
	if err != nil {
		return nil, err
	}
	session, err := td.ListenPacket(ctx, "udp", "")
	if err != nil {
		return nil, fmt.Errorf("failed to dial the tunnel of cluster %q: %w", b.name, err)
	}
	return session, nil
}

// rejectListener closes every connection it accepts.
type rejectListener struct {
	net.Listener
}

func (l rejectListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		_ = c.Close()
	}
}

// peerConn is a UDP allocation connected to one peer.
type peerConn struct {
	*turnclient.PacketConn
	peer net.Addr
}

func (c *peerConn) Read(p []byte) (int, error) {
	for {
		n, from, err := c.ReadFrom(p)
		if err != nil {
			return n, err
		}
		if from.String() == c.peer.String() {
			return n, nil
		}
	}
}

func (c *peerConn) Write(p []byte) (int, error) { return c.WriteTo(p, c.peer) }
func (c *peerConn) RemoteAddr() net.Addr        { return c.peer }
