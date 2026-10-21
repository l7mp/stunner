package l4_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/offload"
)

// recordingEngine records the flows the server offloads.
type recordingEngine struct {
	offload.NullEngine
	mu      sync.Mutex
	upserts [][2]offload.Connection
	removes int
}

func (e *recordingEngine) Upsert(a, b offload.Connection, _, _ string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.upserts = append(e.upserts, [2]offload.Connection{a, b})
	return nil
}

func (e *recordingEngine) Remove(_, _ offload.Connection) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.removes++
	return nil
}

func (e *recordingEngine) counts() (int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.upserts), e.removes
}

// TestFlowOffload pins that the server offloads a UDP flow over a direct UDP leg, raw on both
// sides, and removes it with the flow, and that a TCP client flow stays in user space.
func TestFlowOffload(t *testing.T) {
	t.Run("UDP", func(t *testing.T) {
		peer := udpEcho(t)
		eng := &recordingEngine{}
		s := newTestServer(t, setup{listener: "UDP", proto: "UDP",
			endpoints: []string{peer.String()}, engine: eng})
		echo(t, s.dial(t, "udp"), "hello")
		require.Eventually(t, func() bool { n, _ := eng.counts(); return n == 1 }, time.Second,
			10*time.Millisecond, "the flow is offloaded")
		eng.mu.Lock()
		a, b := eng.upserts[0][0], eng.upserts[0][1]
		eng.mu.Unlock()
		assert.Zero(t, a.ChannelID, "the client side is raw")
		assert.Zero(t, b.ChannelID, "a direct leg is raw")
		assert.Equal(t, peer.String(), b.RemoteAddr.String(), "the leg faces the peer")

		require.NoError(t, s.Close(false))
		assert.Eventually(t, func() bool { _, n := eng.counts(); return n == 1 }, time.Second,
			10*time.Millisecond, "closing the flow removes the offload")
	})

	t.Run("TCP client", func(t *testing.T) {
		peer := udpEcho(t)
		eng := &recordingEngine{}
		s := newTestServer(t, setup{listener: "TCP", proto: "UDP",
			endpoints: []string{peer.String()}, engine: eng})
		echo(t, s.dial(t, "tcp"), "hello")
		time.Sleep(50 * time.Millisecond)
		n, _ := eng.counts()
		assert.Zero(t, n, "a stream client stays in user space")
	})
}
