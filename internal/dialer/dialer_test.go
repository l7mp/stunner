package dialer

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/pion/transport/v5/stdnet"
	"github.com/pion/turn/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

func newTestRuntime(t *testing.T) *runtime.Runtime {
	t.Helper()
	log := logger.NewLoggerFactory("all:ERROR")
	tm, err := telemetry.New(telemetry.Callbacks{}, true, log.NewLogger("telemetry"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tm.Close() })
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	return runtime.New(runtime.Config{Logger: log, DryRun: true, Telemetry: tm, Net: nw})
}

// udpEcho runs a UDP echo peer on loopback.
func udpEcho(t *testing.T) net.PacketConn {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0") //nolint:noctx
	require.NoError(t, err)
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(buf[:n], from)
		}
	}()
	return pc
}

// turnServer runs an upstream pion TURN server on loopback with static credentials user/pass and
// returns the tunnel to it over UDP.
func turnServer(t *testing.T) *stnrv2.TunnelConfig {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0") //nolint:noctx
	require.NoError(t, err)
	server, err := turn.NewServer(turn.ServerConfig{
		Realm: "example.org",
		AuthHandler: func(ra *turn.RequestAttributes) (string, []byte, bool) {
			return ra.Username, turn.GenerateAuthKey(ra.Username, ra.Realm, "pass"), true
		},
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: pc,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP("127.0.0.1"),
				Address:      "127.0.0.1",
			},
		}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = server.Close() })
	tunnel := &stnrv2.TunnelConfig{URL: "turn:" + pc.LocalAddr().String() + "?transport=udp",
		Auth: &stnrv2.AuthConfig{Type: "static", Realm: "example.org",
			Credentials: map[string]string{"username": "user", "password": "pass"}}}
	require.NoError(t, tunnel.Validate())
	return tunnel
}

func echo(t *testing.T, c net.Conn, msg string) {
	t.Helper()
	_, err := c.Write([]byte(msg))
	require.NoError(t, err)
	require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
	buf := make([]byte, 1500)
	n, err := c.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, msg, string(buf[:n]))
}

// stubObject stands in for the cluster object in the registry: the dialer reads its config
// from there.
type stubObject struct{ conf *stnrv2.ClusterConfig }

func (o stubObject) Name() string                { return o.conf.Name }
func (stubObject) Type() runtime.ObjectType      { return runtime.TypeCluster }
func (stubObject) Start() error                  { return nil }
func (stubObject) Close(bool) error              { return nil }
func (o stubObject) GetConfig() stnrv2.Config    { return o.conf }
func (stubObject) Reconcile(stnrv2.Config) error { return nil }
func (stubObject) Status() stnrv2.Status         { return nil }
func (stubObject) Inspect(_, _ stnrv2.Config, _ *stnrv2.StunnerConfig) (runtime.Action, error) {
	return runtime.ActionNone, nil
}

func newDialer(t *testing.T, rt *runtime.Runtime, conf *stnrv2.ClusterConfig) api.Dialer {
	t.Helper()
	require.NoError(t, conf.Validate())
	require.NoError(t, rt.Registry.Add(stubObject{conf: conf}, nil))
	cl, err := New(conf, rt)
	require.NoError(t, err)
	require.NoError(t, cl.Start())
	return cl
}

func TestUDPDialer(t *testing.T) {
	rt := newTestRuntime(t)
	cl := newDialer(t, rt, &stnrv2.ClusterConfig{Name: "udp", Protocol: "UDP",
		Addrs: []string{"10.0.0.1", "fd00::1"}})
	peer := udpEcho(t)
	assert.Equal(t, stnrv2.ProtocolUDP, cl.Protocol())

	c, err := cl.Dial(context.Background(), nil, peer.LocalAddr().String())
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck
	echo(t, c, "hello")
	assert.Equal(t, api.Tag{Name: "udp", Proto: stnrv2.ProtocolUDP}, c.Tag())
	_, remote := c.TransportAddrs()
	assert.Equal(t, peer.LocalAddr().String(), remote.String())
	_, framed := c.Channel(peer.LocalAddr())
	assert.False(t, framed)

	// the relay socket of a TURN allocation: advertised at the cluster's address of the family,
	// serving every peer
	relay, advertised, err := cl.ListenPacket("udp4", 0)
	require.NoError(t, err)
	defer relay.Close() //nolint:errcheck
	assert.Equal(t, "10.0.0.1", advertised.(*net.UDPAddr).IP.String())
	_, err = relay.WriteTo([]byte("x"), peer.LocalAddr())
	require.NoError(t, err)
	require.NoError(t, relay.SetReadDeadline(time.Now().Add(5*time.Second)))
	buf := make([]byte, 16)
	n, _, err := relay.ReadFrom(buf)
	require.NoError(t, err)
	assert.Equal(t, "x", string(buf[:n]))
	_, err = relay.WriteTo([]byte("x"), &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9})
	assert.NoError(t, err, "a relay socket checks no peers")

	_, _, err = cl.Listen("tcp4", 0)
	assert.ErrorIs(t, err, api.ErrNotSupported, "a UDP dialer makes no relayed listeners")

	// without an address of the family, the relay socket's own address is advertised
	own, ownAdvertised, err := newDialer(t, rt, &stnrv2.ClusterConfig{Name: "v6only",
		Protocol: "UDP", Addrs: []string{"fd00::1"}}).ListenPacket("udp4", 0)
	require.NoError(t, err)
	defer own.Close() //nolint:errcheck
	assert.Equal(t, own.LocalAddr().String(), ownAdvertised.String())
}

func TestTCPDialer(t *testing.T) {
	rt := newTestRuntime(t)
	cl := newDialer(t, rt, &stnrv2.ClusterConfig{Name: "tcp", Protocol: "TCP",
		Addrs: []string{"10.0.0.1"}})
	assert.Equal(t, stnrv2.ProtocolTCP, cl.Protocol())

	ln, err := net.Listen("tcp4", "127.0.0.1:0") //nolint:noctx
	require.NoError(t, err)
	defer ln.Close() //nolint:errcheck
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 1500)
		n, _ := c.Read(buf)
		_, _ = c.Write(buf[:n])
	}()

	c, err := cl.Dial(context.Background(), nil, ln.Addr().String())
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck
	echo(t, c, "hello")

	l, advertised, err := cl.Listen("tcp4", 0)
	require.NoError(t, err)
	defer l.Close() //nolint:errcheck
	assert.Equal(t, "10.0.0.1", advertised.(*net.TCPAddr).IP.String())

	// pion dials the peers of an allocation from its relayed address: the dial shares the port
	// of the relayed listener
	peer, err := net.Listen("tcp4", "127.0.0.1:0") //nolint:noctx
	require.NoError(t, err)
	defer peer.Close() //nolint:errcheck
	go func() {
		c, err := peer.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 1500)
		n, _ := c.Read(buf)
		_, _ = c.Write(buf[:n])
	}()
	port := l.Addr().(*net.TCPAddr).Port
	relayed, err := cl.Dial(context.Background(), &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port},
		peer.Addr().String())
	require.NoError(t, err, "a dial shares the relayed listener's port")
	defer relayed.Close() //nolint:errcheck
	assert.Equal(t, port, relayed.LocalAddr().(*net.TCPAddr).Port)
	echo(t, relayed, "relayed")

	_, _, err = cl.ListenPacket("udp4", 0)
	assert.ErrorIs(t, err, api.ErrNotSupported, "a TCP dialer makes no relay sockets")
}

func TestTunnelDialer(t *testing.T) {
	rt := newTestRuntime(t)
	tunnel := turnServer(t)
	cl := newDialer(t, rt, &stnrv2.ClusterConfig{Name: "upstream", Protocol: "UDP", Tunnel: tunnel})
	peer := udpEcho(t)

	// a dial is an allocation connected to the peer, its wire the session's
	c, err := cl.Dial(context.Background(), nil, peer.LocalAddr().String())
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck
	echo(t, c, "hello")
	assert.Equal(t, api.Tag{Name: "upstream", Proto: stnrv2.ProtocolUDP}, c.Tag())
	assert.Equal(t, peer.LocalAddr().String(), c.RemoteAddr().String())
	u, err := stnrv2.ParseURI(tunnel.URL)
	require.NoError(t, err)
	_, remote := c.TransportAddrs()
	assert.Equal(t, u.HostPort(), remote.String(), "the wire runs to the upstream server")

	// a relay socket is an allocation too, advertised at the upstream relayed address
	relay, advertised, err := cl.ListenPacket("udp4", 0)
	require.NoError(t, err)
	defer relay.Close() //nolint:errcheck
	assert.Equal(t, "127.0.0.1", advertised.(*net.UDPAddr).IP.String())
	_, err = relay.WriteTo([]byte("x"), peer.LocalAddr())
	require.NoError(t, err)
	require.NoError(t, relay.SetReadDeadline(time.Now().Add(5*time.Second)))
	buf := make([]byte, 16)
	n, _, err := relay.ReadFrom(buf)
	require.NoError(t, err)
	assert.Equal(t, "x", string(buf[:n]))

	_, _, err = cl.Listen("tcp4", 0)
	assert.ErrorIs(t, err, api.ErrNotSupported)

	// a TCP peer needs a stream transport towards the upstream server: over UDP, dialing fails
	tcp := newDialer(t, rt, &stnrv2.ClusterConfig{Name: "upstream-tcp", Protocol: "TCP",
		Tunnel: tunnel})
	_, err = tcp.Dial(context.Background(), nil, "127.0.0.1:22")
	assert.ErrorContains(t, err, "stream transport")
	_, _, err = tcp.ListenPacket("udp4", 0)
	assert.ErrorIs(t, err, api.ErrNotSupported)
}
