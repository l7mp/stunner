package turnclient

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/pion/turn/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

func TestNewConfig(t *testing.T) {
	t.Run("static auth", func(t *testing.T) {
		s := &stnrv1.TURNServer{Address: "1.2.3.4", Port: 3478,
			Auth: &stnrv1.AuthConfig{Type: "static", Realm: "example.org",
				Credentials: map[string]string{"username": "user", "password": "pass"}}}
		c, err := NewConfig(s, stnrv1.ProtocolTURNUDP)
		require.NoError(t, err)
		assert.Equal(t, stnrv1.ProtocolTURNUDP, c.Protocol)
		assert.Equal(t, "1.2.3.4:3478", c.ServerAddr)
		assert.Equal(t, "user", c.Username)
		assert.Equal(t, "pass", c.Password)
		assert.Equal(t, "example.org", c.Realm, "realm seeded from the auth config")
		assert.Equal(t, "1.2.3.4", c.ServerName)
	})

	t.Run("no auth block dials anonymously", func(t *testing.T) {
		s := &stnrv1.TURNServer{Address: "1.2.3.4", Port: 3478}
		c, err := NewConfig(s, stnrv1.ProtocolTURNTCP)
		require.NoError(t, err)
		assert.Empty(t, c.Username)
		assert.Empty(t, c.Password)
		assert.Empty(t, c.Realm)
	})

	t.Run("explicit none dials anonymously too", func(t *testing.T) {
		// a nil auth block and an explicit "none" must behave identically on the wire:
		// no credentials either way (the realm seed is inert without credentials, no
		// message integrity is ever computed from it)
		s := &stnrv1.TURNServer{Address: "1.2.3.4", Port: 3478,
			Auth: &stnrv1.AuthConfig{Type: "none"}}
		c, err := NewConfig(s, stnrv1.ProtocolTURNUDP)
		require.NoError(t, err)
		assert.Empty(t, c.Username)
		assert.Empty(t, c.Password)
	})

	t.Run("ephemeral auth generates per-call credentials", func(t *testing.T) {
		s := &stnrv1.TURNServer{Address: "2001:db8::1", Port: 3478,
			Auth: &stnrv1.AuthConfig{Type: "ephemeral", Lifetime: "10m",
				Credentials: map[string]string{"secret": "my-secret"}}}
		c, err := NewConfig(s, stnrv1.ProtocolTURNTLS)
		require.NoError(t, err)
		assert.Equal(t, "[2001:db8::1]:3478", c.ServerAddr, "IPv6 host is bracketed")
		_, err = strconv.ParseInt(c.Username, 10, 64)
		assert.NoError(t, err, "time-windowed username")
		assert.NotEmpty(t, c.Password)
	})

	t.Run("SNI overrides the server name", func(t *testing.T) {
		s := &stnrv1.TURNServer{Address: "1.2.3.4", Port: 5349, SNI: "turn.example.com"}
		c, err := NewConfig(s, stnrv1.ProtocolTURNTLS)
		require.NoError(t, err)
		assert.Equal(t, "turn.example.com", c.ServerName, "SNI wins")
		assert.Equal(t, "1.2.3.4:5349", c.ServerAddr, "dial address unchanged")
	})

	t.Run("broken auth config fails the dial early", func(t *testing.T) {
		s := &stnrv1.TURNServer{Address: "1.2.3.4", Port: 3478,
			Auth: &stnrv1.AuthConfig{Type: "ephemeral"}}
		_, err := NewConfig(s, stnrv1.ProtocolTURNUDP)
		assert.Error(t, err)
	})
}

// TestUpstreamSessionChannel drives a real TURN session against a local pion server: the
// client assigns a channel for the peer at the first write, the probe finds it at the channel
// floor, and TransportAddrs reports the wire 5-tuple.
func TestUpstreamSessionChannel(t *testing.T) {
	serverConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err, "server socket")
	srv, err := turn.NewServer(turn.ServerConfig{
		Realm: "test",
		AuthHandler: func(*turn.RequestAttributes) (string, []byte, bool) {
			return "user", turn.GenerateAuthKey("user", "test", "pass"), true
		},
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: serverConn,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP("127.0.0.1"), Address: "127.0.0.1"},
		}},
	})
	require.NoError(t, err, "TURN server")
	defer srv.Close() //nolint:errcheck

	pc, err := Dialer{Config: Config{
		Protocol:   stnrv1.ProtocolTURNUDP,
		ServerAddr: serverConn.LocalAddr().String(),
		Username:   "user",
		Password:   "pass",
		Realm:      "test",
	}}.ListenPacket(context.Background(), "udp", "")
	require.NoError(t, err, "upstream session")
	defer pc.Close() //nolint:errcheck

	local, remote := pc.TransportAddrs()
	require.NotNil(t, local, "transport local address")
	assert.Equal(t, serverConn.LocalAddr().String(), remote.String(), "server address")

	peer := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9000}
	_, err = pc.WriteTo([]byte("hello"), peer)
	require.NoError(t, err, "relayed write")

	// the channel is assigned at the first write, but Channel stays silent until the
	// ChannelBind settles a round trip later: until then the client sends the payload as a
	// Send indication and the wire carries no ChannelData
	assert.Eventually(t, func() bool {
		_, ok := pc.Channel(peer)
		return ok
	}, 3*time.Second, 50*time.Millisecond, "the binding goes live")

	// per-session allocations bind a single peer, so its channel lands at the floor (0x4000)
	num, ok := pc.Channel(peer)
	require.True(t, ok, "channel live")
	assert.Equal(t, uint16(0x4000), num, "channel at the floor")

	// an address the session does not carry has no framing
	other := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9001}
	_, ok = pc.Channel(other)
	assert.False(t, ok, "no framing for a peer the session does not carry")
}
