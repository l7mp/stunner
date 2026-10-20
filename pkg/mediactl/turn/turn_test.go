package turn

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/pion/turn/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/pkg/mediactl"
	"github.com/l7mp/stunner/v2/pkg/udp"
)

// TestDialer streams through a local TURN server to a UDP echo peer.
func TestDialer(t *testing.T) {
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	srv, err := turn.NewServer(turn.ServerConfig{
		Realm: "test",
		AuthHandler: func(*turn.RequestAttributes) (string, []byte, bool) {
			return "user", turn.GenerateAuthKey("user", "test", "pass"), true
		},
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: conn,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP("127.0.0.1"), Address: "127.0.0.1"},
		}},
	})
	require.NoError(t, err)
	defer srv.Close() //nolint:errcheck

	peer, err := udp.DefaultListenConfig.Listen("udp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = mediactl.Serve(peer, true, nil) }()
	defer peer.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	d := Dialer{URI: "turn:" + conn.LocalAddr().String() + "?transport=udp", Username: "user", Password: "pass"}
	relayed, err := d.DialContext(ctx, "udp", peer.Addr().String())
	require.NoError(t, err)
	assert.NotNil(t, relayed.LocalAddr(), "the relayed address")
	assert.Equal(t, peer.Addr().String(), relayed.RemoteAddr().String())
	r := mediactl.Stream(ctx, relayed, mediactl.Config{Length: 100, Rate: 100, Echo: true}).Result()
	assert.NotZero(t, r.Received, "the echo came back through the relay")
	assert.Zero(t, r.Lost)

	_, err = Dialer{URI: "udp://" + conn.LocalAddr().String()}.DialContext(ctx, "udp", peer.Addr().String())
	assert.Error(t, err, "not a TURN URI")
}
