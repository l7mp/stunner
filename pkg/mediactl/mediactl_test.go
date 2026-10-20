package mediactl

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/pkg/udp"
)

// TestMain runs the tests on short timers.
func TestMain(m *testing.M) {
	EchoDrain, FlowIdle = 50*time.Millisecond, 100*time.Millisecond
	os.Exit(m.Run())
}

// TestStats pins the loss of a receiver: the gaps in the sequence numbers of the headers.
func TestStats(t *testing.T) {
	s := NewStats()
	for _, seq := range []uint32{0, 1, 3} {
		p := make([]byte, HeaderLen)
		binary.BigEndian.PutUint32(p, seq)
		binary.BigEndian.PutUint64(p[4:], uint64(time.Now().UnixNano()))
		s.Received(p, true)
	}
	s.Received([]byte{1}, true)
	r := s.Result()
	assert.Equal(t, int64(4), r.Received, "a short packet counts too")
	assert.Equal(t, int64(4), r.Expected, "up to the highest sequence number")
	assert.Equal(t, int64(0), r.Lost, "4 received of 4 expected, the short one included")
	assert.Zero(t, r.RTTMax, "a receiver alone measures no round trip")
}

// lossy drops every other datagram it writes.
type lossy struct {
	net.Conn
	n int
}

func (c *lossy) Write(p []byte) (int, error) {
	if c.n++; c.n%2 == 0 {
		return len(p), nil
	}
	return c.Conn.Write(p)
}

// TestStreamEcho streams through a datagram echo on a UDP listener of pkg/udp, once clean and once
// with half the packets lost on the way, and checks what the stream measured.
func TestStreamEcho(t *testing.T) {
	l, err := udp.DefaultListenConfig.Listen("udp", "127.0.0.1:0")
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Go(func() { _ = Serve(l, true, nil) })
	defer func() { _ = l.Close(); wg.Wait() }()

	for _, c := range []struct {
		name string
		lose bool
	}{{"clean", false}, {"lossy", true}} {
		t.Run(c.name, func(t *testing.T) {
			raw, err := net.Dial("udp", l.Addr().String())
			require.NoError(t, err)
			var conn net.Conn = raw
			if c.lose {
				conn = &lossy{Conn: raw}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			r := Stream(ctx, conn, Config{Length: 100, Rate: 200, Echo: true}).Result()
			require.NotZero(t, r.Sent)
			assert.Equal(t, r.Sent, r.Expected, "an echoing stream expects what it sent")
			if c.lose {
				assert.InDelta(t, r.Sent/2, r.Lost, 1, "half of it lost")
			} else {
				assert.Zero(t, r.Lost)
			}
			assert.NotZero(t, r.RTTMax, "the round trip of the echo")
			assert.LessOrEqual(t, r.RTTMin, r.RTTAvg)
		})
	}
}

// TestEcho echoes a byte stream: everything comes back, nothing is judged lost.
func TestEcho(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Go(func() { _ = Serve(l, false, nil) })
	defer func() { _ = l.Close(); wg.Wait() }()

	conn, err := net.Dial("tcp", l.Addr().String())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	r := Stream(ctx, conn, Config{Length: 100, Rate: 200, Echo: true, ByteStream: true}).Result()
	assert.Equal(t, r.SentBytes, r.ReceivedBytes, "every byte came back")
	assert.Zero(t, r.Expected, "a byte stream judges no loss")
}
