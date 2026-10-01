package udp

import (
	"context"
	"net"
	"testing"

	"github.com/pion/transport/v5/reuseport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSocketPool(t *testing.T) {
	t.Run("SharedOrOne", func(t *testing.T) {
		// reserve a free port, then bind the pool on it
		probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
		require.NoError(t, err)
		addr := probe.LocalAddr().String()
		require.NoError(t, probe.Close())

		conns, reuse, err := listenReusePort("udp4", addr, 3)
		require.NoError(t, err)
		defer func() {
			for _, c := range conns {
				_ = c.Close()
			}
		}()
		// a platform sharing the port binds them all, any other falls back to one
		assert.Contains(t, []int{1, 3}, len(conns))
		assert.Equal(t, len(conns) == 3, reuse)
		for _, c := range conns {
			assert.Equal(t, addr, c.LocalAddr().String(), "all sockets share the address")
		}
	})

	t.Run("OneThreadSharesToo", func(t *testing.T) {
		conns, reuse, err := listenReusePort("udp4", "127.0.0.1:0", 0)
		require.NoError(t, err)
		defer conns[0].Close() //nolint:errcheck
		assert.Len(t, conns, 1)
		if reuse {
			// a second socket can join the address
			lc := net.ListenConfig{Control: reuseport.Control}
			other, err := lc.ListenPacket(context.Background(), "udp4", conns[0].LocalAddr().String())
			require.NoError(t, err)
			assert.NoError(t, other.Close())
		}
	})
}
