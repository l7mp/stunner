package classify_test

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/pion/transport/v5/test"
	"github.com/pion/transport/v5/vnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/netconn/classify"
	"github.com/l7mp/stunner/v2/internal/netconn/wire"
)

const testCluster = "test-cluster"

// portRange classifies UDP peers in [minPort, maxPort] into the test cluster.
func portRange(minPort, maxPort int) classify.Func {
	return func(addr net.Addr) (string, bool) {
		u, ok := addr.(*net.UDPAddr)
		if !ok {
			return "", false
		}
		return testCluster, u.Port >= minPort && u.Port <= maxPort
	}
}

func TestPacketConn(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()
	report := test.CheckRoutines(t)
	defer report()

	nw, err := vnet.NewNet(&vnet.NetConfig{})
	require.NoError(t, err, "vnet")

	t.Run("ClassifiedPeerPassesBothWays", func(t *testing.T) {
		addr := "127.0.0.1:15000"
		sock, err := nw.ListenPacket("udp4", addr)
		require.NoError(t, err)
		conn := classify.NewPacketConn(wire.Direct(sock), portRange(10000, 20000))

		udpAddr, err := net.ResolveUDPAddr("udp4", addr)
		require.NoError(t, err)
		class, ok := conn.Class(udpAddr)
		assert.True(t, ok, "classified")
		assert.Equal(t, testCluster, class, "class")

		msg := "PING!"
		n, err := conn.WriteTo([]byte(msg), udpAddr)
		require.NoError(t, err)
		assert.Equal(t, len(msg), n)

		buf := make([]byte, 1000)
		n, from, err := conn.ReadFrom(buf)
		require.NoError(t, err)
		assert.Equal(t, msg, string(buf[:n]))
		assert.Equal(t, udpAddr.String(), from.String())

		local, remote := conn.TransportAddrs()
		assert.Equal(t, udpAddr.String(), local.String(), "wire facts promote through the layer")
		assert.Nil(t, remote, "direct leg has no fixed remote")
		assert.NoError(t, conn.Close())
	})

	t.Run("UnclassifiedPeerIsRejectedAndDropped", func(t *testing.T) {
		addr := "127.0.0.1:25000"
		sock, err := nw.ListenPacket("udp4", addr)
		require.NoError(t, err)
		conn := classify.NewPacketConn(wire.Direct(sock), portRange(10000, 20000))

		udpAddr, err := net.ResolveUDPAddr("udp4", addr)
		require.NoError(t, err)
		_, ok := conn.Class(udpAddr)
		assert.False(t, ok, "unclassified")

		n, err := conn.WriteTo([]byte("PING!"), udpAddr)
		assert.ErrorIs(t, err, classify.ErrProhibited, "write rejected")
		assert.Equal(t, 0, n)

		// send around the wrapper: the datagram must be dropped on the way in, so the read
		// runs into its deadline instead of returning it
		_, err = sock.WriteTo([]byte("PING!"), udpAddr)
		require.NoError(t, err)
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(50*time.Millisecond)))
		_, _, err = conn.ReadFrom(make([]byte, 1000))
		assert.Error(t, err, "dropped, then deadline")
		assert.NoError(t, conn.Close())
	})
}

// chanListener is a fake net.Listener that returns conns fed through a channel.
type chanListener struct {
	ch   chan net.Conn
	done chan struct{}
}

func newChanListener() *chanListener {
	return &chanListener{ch: make(chan net.Conn, 8), done: make(chan struct{})}
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return c, nil
	case <-l.done:
		return nil, errors.New("listener closed")
	}
}

func (l *chanListener) Close() error   { close(l.done); return nil }
func (l *chanListener) Addr() net.Addr { return &net.TCPAddr{} }

// taggedAddr is a net.Addr whose string value is what the classifier looks at.
type taggedAddr struct{ tag string }

func (a *taggedAddr) Network() string { return "tcp" }
func (a *taggedAddr) String() string  { return a.tag }

// taggedConn is a net.Conn whose remote address has a configurable tag.
type taggedConn struct {
	net.Conn
	remote net.Addr
}

func (c *taggedConn) RemoteAddr() net.Addr { return c.remote }

func TestListener(t *testing.T) {
	lim := test.TimeOut(30 * time.Second)
	defer lim.Stop()

	tagged := func(addr net.Addr) (string, bool) {
		return testCluster, addr.String() == "classified"
	}
	base := newChanListener()
	listener := classify.NewListener(base, tagged, nil)

	t.Run("ClassifiedPeerIsAccepted", func(t *testing.T) {
		server, client := net.Pipe()
		defer client.Close()
		base.ch <- &taggedConn{Conn: server, remote: &taggedAddr{tag: "classified"}}

		accepted, err := listener.AcceptConn()
		require.NoError(t, err)
		defer accepted.Close()
		assert.Equal(t, testCluster, accepted.Class(), "accepted conn carries its class")
	})

	t.Run("UnclassifiedPeerIsClosedAndSkipped", func(t *testing.T) {
		deniedServer, deniedClient := net.Pipe()
		defer deniedClient.Close()
		okServer, okClient := net.Pipe()
		defer okClient.Close()
		base.ch <- &taggedConn{Conn: deniedServer, remote: &taggedAddr{tag: "denied"}}
		base.ch <- &taggedConn{Conn: okServer, remote: &taggedAddr{tag: "classified"}}

		accepted, err := listener.Accept()
		require.NoError(t, err)
		defer accepted.Close()

		buf := make([]byte, 1)
		deniedClient.SetReadDeadline(time.Now().Add(100 * time.Millisecond)) //nolint:errcheck
		_, err = deniedClient.Read(buf)
		assert.Error(t, err, "denied conn is closed by the listener")
	})

	require.NoError(t, listener.Close())
}
