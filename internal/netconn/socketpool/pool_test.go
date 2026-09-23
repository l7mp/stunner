package socketpool

import (
	"net"
	"testing"

	"github.com/pion/transport/v5/stdnet"
	"github.com/pion/transport/v5/vnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListenPacket(t *testing.T) {
	t.Run("VNetGetsOneSocket", func(t *testing.T) {
		nw, err := vnet.NewNet(&vnet.NetConfig{})
		require.NoError(t, err)
		conns, err := ListenPacket(nw, "udp4", "127.0.0.1:0", 4)
		require.NoError(t, err)
		assert.Len(t, conns, 1)
		assert.NoError(t, conns[0].Close())
	})

	t.Run("SharedOrOne", func(t *testing.T) {
		nw, err := stdnet.NewNet()
		require.NoError(t, err)
		// reserve a free port, then bind the pool on it
		probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
		require.NoError(t, err)
		addr := probe.LocalAddr().String()
		require.NoError(t, probe.Close())

		conns, err := ListenPacket(nw, "udp4", addr, 3)
		require.NoError(t, err)
		defer func() {
			for _, c := range conns {
				_ = c.Close()
			}
		}()
		// a platform sharing the port binds them all, any other falls back to one
		assert.Contains(t, []int{1, 3}, len(conns))
		for _, c := range conns {
			assert.Equal(t, addr, c.LocalAddr().String(), "all sockets share the address")
		}
	})
}
