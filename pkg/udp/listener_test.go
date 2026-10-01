package udp

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/pion/transport/v5/stdnet"
	"github.com/pion/transport/v5/vnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serve accepts the conns of a listener and echoes each; the accepted conns are handed to the test.
func serve(t *testing.T, ln net.Listener) chan net.Conn {
	t.Helper()
	accepted := make(chan net.Conn, 256)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- c
			go func() {
				buf := make([]byte, 2048)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					if _, err := c.Write(buf[:n]); err != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return accepted
}

func roundTrip(t *testing.T, c net.Conn, msg string) {
	t.Helper()
	_, err := c.Write([]byte(msg))
	require.NoError(t, err)
	require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
	buf := make([]byte, 2048)
	n, err := c.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, msg, string(buf[:n]))
}

func TestListen(t *testing.T) {
	t.Run("Demux", func(t *testing.T) { testListen(t, false) })
	t.Run("ConnectBack", func(t *testing.T) { testListen(t, true) })
}

func testListen(t *testing.T, connectBack bool) {
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	ln, err := ListenConfig{Net: nw, ConnectBack: connectBack}.Listen("udp", "127.0.0.1:0")
	require.NoError(t, err)
	accepted := serve(t, ln)
	addr := ln.Addr().String()
	_, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)

	t.Run("OneConnPerClient", func(t *testing.T) {
		clients := []net.Conn{}
		for i := 0; i < 10; i++ {
			c, err := net.Dial("udp", addr) //nolint:noctx
			require.NoError(t, err)
			defer c.Close() //nolint:errcheck
			clients = append(clients, c)
			roundTrip(t, c, fmt.Sprintf("hello-%d", i))
		}
		for _, cl := range clients {
			roundTrip(t, cl, "again")
		}
		for range clients {
			c := <-accepted
			host, p, err := net.SplitHostPort(c.LocalAddr().String())
			require.NoError(t, err)
			assert.Equal(t, port, p)
			if connectBack {
				assert.Equal(t, "127.0.0.1", host, "a connected conn has a concrete local address")
			}
		}
		assert.Empty(t, accepted, "one conn per client")
	})

	t.Run("ConcurrentBursts", func(t *testing.T) {
		// Every client opens with a burst; each conn must see exactly its own client's
		// datagrams: racing datagrams reach the conn, foreign ones never do.
		const clients, burst = 30, 10
		var wg sync.WaitGroup
		for i := 0; i < clients; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				c, err := net.Dial("udp", addr) //nolint:noctx
				if !assert.NoError(t, err) {
					return
				}
				defer c.Close() //nolint:errcheck
				for j := 0; j < burst; j++ {
					_, err = c.Write([]byte(fmt.Sprintf("%d/%d", i, j)))
					assert.NoError(t, err)
				}
				got := []string{}
				buf := make([]byte, 64)
				_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
				for len(got) < burst {
					n, err := c.Read(buf)
					if err != nil {
						break
					}
					got = append(got, string(buf[:n]))
				}
				want := []string{}
				for j := 0; j < burst; j++ {
					want = append(want, fmt.Sprintf("%d/%d", i, j))
				}
				sort.Strings(got)
				sort.Strings(want)
				assert.Equal(t, want, got, "client %d", i)
			}(i)
		}
		wg.Wait()
		for len(accepted) > 0 {
			<-accepted
		}
	})

	t.Run("ReacceptAfterClose", func(t *testing.T) {
		c, err := net.Dial("udp", addr) //nolint:noctx
		require.NoError(t, err)
		defer c.Close() //nolint:errcheck
		roundTrip(t, c, "first")
		sc := <-accepted
		require.NoError(t, sc.Close())
		roundTrip(t, c, "second")
		again := <-accepted
		assert.Equal(t, sc.RemoteAddr().String(), again.RemoteAddr().String())
	})
}

// TestListenVNet pins that a vnet gets a single socket and a demuxing listener, connect-back or
// not.
func TestListenVNet(t *testing.T) {
	nw, err := vnet.NewNet(&vnet.NetConfig{})
	require.NoError(t, err)
	ln, err := ListenConfig{Net: nw, ConnectBack: true}.Listen("udp", "127.0.0.1:3478")
	require.NoError(t, err)
	assert.Len(t, ln.(*listener).socks, 1)
	assert.False(t, ln.(*listener).connectBack)
	accepted := serve(t, ln)

	for i := 0; i < 3; i++ {
		pc, err := nw.ListenPacket("udp", "127.0.0.1:0")
		require.NoError(t, err)
		defer pc.Close() //nolint:errcheck
		server := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 3478}
		msg := "hello-" + strconv.Itoa(i)
		_, err = pc.WriteTo([]byte(msg), server)
		require.NoError(t, err)
		require.NoError(t, pc.SetReadDeadline(time.Now().Add(5*time.Second)))
		buf := make([]byte, 64)
		n, _, err := pc.ReadFrom(buf)
		require.NoError(t, err)
		assert.Equal(t, msg, string(buf[:n]))
	}
	assert.Len(t, accepted, 3)
}
