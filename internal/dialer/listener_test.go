package dialer

import (
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// dialingListener returns the socket of a TCP listener dialing addr, closed with the test.
func dialingListener(t *testing.T, addr string) net.Listener {
	t.Helper()
	conf := &stnrv2.ListenerConfig{Name: "dialing", Protocol: "TCP", Servers: []string{"s"},
		Connect: &stnrv2.ListenerConnectConfig{Address: addr}}
	require.NoError(t, conf.Validate())
	ln, err := NewListener(conf, newTestRuntime(t), func(net.Addr) bool { return true })
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// accept runs Accept in the background: the conns it returns arrive on the channel.
func accept(ln net.Listener) chan net.Conn {
	accepted := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()
	return accepted
}

func next(t *testing.T, accepted chan net.Conn) net.Conn {
	t.Helper()
	select {
	case c := <-accepted:
		return c
	case <-time.After(5 * time.Second):
		require.FailNow(t, "no conn dialed")
		return nil
	}
}

func TestDialingListener(t *testing.T) {
	defer func(d time.Duration) { dialBackoffInitial = d }(dialBackoffInitial)
	dialBackoffInitial = 10 * time.Millisecond

	t.Run("redial after the conn closes", func(t *testing.T) {
		peer, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
		require.NoError(t, err)
		peerSide := streamEcho(t, peer)
		dialed := accept(dialingListener(t, peer.Addr().String()))
		first := next(t, dialed)
		echo(t, first, "hello")
		assert.Empty(t, dialed, "one conn at a time")

		// the peer drops the flow: the server reading the conn sees EOF and closes it
		require.NoError(t, (<-peerSide).Close())
		_, err = first.Read(make([]byte, 16))
		require.Error(t, err)
		require.NoError(t, first.Close())
		echo(t, next(t, dialed), "again")
	})

	t.Run("redial while the peer is down", func(t *testing.T) {
		probe, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
		require.NoError(t, err)
		addr := probe.Addr().String()
		require.NoError(t, probe.Close())
		dialed := accept(dialingListener(t, addr))
		time.Sleep(50 * time.Millisecond)
		peer, err := net.Listen("tcp", addr) //nolint:noctx
		require.NoError(t, err)
		streamEcho(t, peer)
		echo(t, next(t, dialed), "hello")
	})

	t.Run("close drops the conn and stops dialing", func(t *testing.T) {
		peer, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
		require.NoError(t, err)
		peerSide := streamEcho(t, peer)
		ln := dialingListener(t, peer.Addr().String())
		c := next(t, accept(ln))
		remote := <-peerSide
		require.NoError(t, ln.Close())
		_, err = c.Read(make([]byte, 16))
		require.Error(t, err, "the conn is closed")
		require.NoError(t, remote.SetReadDeadline(time.Now().Add(5*time.Second)))
		_, err = remote.Read(make([]byte, 16))
		require.ErrorIs(t, err, io.EOF, "the peer sees the close")
		select {
		case <-peerSide:
			assert.Fail(t, "a closed listener redials")
		case <-time.After(100 * time.Millisecond):
		}
	})

	t.Run("the peer is the address", func(t *testing.T) {
		ln := dialingListener(t, "127.0.0.1:"+strconv.Itoa(5060))
		assert.Equal(t, "127.0.0.1:5060", ln.Addr().String())
		assert.Equal(t, "tcp", ln.Addr().Network())
	})

	t.Run("STDIN and FIFO do not dial", func(t *testing.T) {
		for _, proto := range []string{"STDIN", "FIFO"} {
			conf := &stnrv2.ListenerConfig{Name: "nodial", Protocol: proto, Addr: "fifo",
				Connect: &stnrv2.ListenerConnectConfig{Address: "127.0.0.1:5060"}}
			require.NoError(t, conf.Validate())
			_, err := NewListener(conf, newTestRuntime(t), func(net.Addr) bool { return true })
			assert.ErrorContains(t, err, "connect mode is not supported", proto)
		}
	})
}
