package netconn

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterPacketConnAdmitsOnce(t *testing.T) {
	sock, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	var allow atomic.Bool
	checks := atomic.Int32{}
	c := FilterPacketConn(sock, func(net.Addr) bool { checks.Add(1); return allow.Load() })
	defer c.Close()

	peer := func() net.PacketConn {
		p, err := net.ListenPacket("udp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		return p
	}
	// read returns the sender of the next datagram the filter passes, nil on a timeout
	read := func() net.Addr {
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_, from, err := c.ReadFrom(make([]byte, 16))
		if err != nil {
			return nil
		}
		return from
	}
	p1, p2 := peer(), peer()

	_, _ = p1.WriteTo([]byte("x"), sock.LocalAddr())
	assert.Nil(t, read(), "a refused peer is dropped")
	allow.Store(true)
	_, _ = p1.WriteTo([]byte("x"), sock.LocalAddr())
	assert.Equal(t, p1.LocalAddr().String(), read().String(), "a refused peer is checked again")

	allow.Store(false)
	n := checks.Load()
	_, _ = p1.WriteTo([]byte("x"), sock.LocalAddr())
	assert.Equal(t, p1.LocalAddr().String(), read().String(), "an admitted peer stays admitted")
	_, err = c.WriteTo([]byte("x"), p1.LocalAddr())
	require.NoError(t, err)
	assert.Equal(t, n, checks.Load(), "an admitted peer is not checked again")
	_, _ = p2.WriteTo([]byte("x"), sock.LocalAddr())
	assert.Nil(t, read(), "a filter change applies to new peers")
}
