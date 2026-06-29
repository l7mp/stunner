package netutil_test

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/pion/transport/v4/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/netutil"
	iTelemetry "github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/pkg/logger"
)

const testConnLogLevel = "all:ERROR"

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

func (l *chanListener) push(c net.Conn) { l.ch <- c }

// taggedAddr is a net.Addr whose string value is used for admission decisions.
type taggedAddr struct{ tag string }

func (a *taggedAddr) Network() string { return "tcp" }
func (a *taggedAddr) String() string  { return a.tag }

// taggedConn is a net.Conn whose remote address has a configurable tag.
type taggedConn struct {
	net.Conn
	remote net.Addr
}

func (c *taggedConn) RemoteAddr() net.Addr { return c.remote }

func TestAdmittingListener(t *testing.T) {
	lim := test.TimeOut(30 * time.Second)
	defer lim.Stop()

	log := logger.NewLoggerFactory(testConnLogLevel)
	tm, err := iTelemetry.New(iTelemetry.Callbacks{}, false, log.NewLogger("metric"))
	require.NoError(t, err)
	defer tm.Close() //nolint:errcheck

	const cluster = "test-cluster"

	// admit only conns tagged "admitted".
	admit := func(addr net.Addr) (string, bool) {
		return cluster, addr.String() == "admitted"
	}

	base := newChanListener()
	listener := netutil.NewListener(base, "unused", iTelemetry.ClusterType, tm, admit,
		log.NewLogger("prl"))

	t.Run("AdmittedPeerIsAccepted", func(t *testing.T) {
		server, client := net.Pipe()
		defer client.Close()

		base.push(&taggedConn{Conn: server, remote: &taggedAddr{tag: "admitted"}})

		accepted, err := listener.Accept()
		require.NoError(t, err)
		defer accepted.Close()

		// Accepted conn is telemetry wrapped: write through it to exercise telemetry.
		msg := []byte("PING")
		readCh := make(chan []byte, 1)
		go func() {
			buf := make([]byte, len(msg))
			client.Read(buf) //nolint:errcheck
			readCh <- buf
		}()
		_, err = accepted.Write(msg)
		require.NoError(t, err)
		assert.Equal(t, msg, <-readCh)
	})

	t.Run("UnadmittedPeerIsDropped", func(t *testing.T) {
		// Push a denied conn followed immediately by an admitted conn.
		deniedServer, deniedClient := net.Pipe()
		defer deniedClient.Close()
		admittedServer, admittedClient := net.Pipe()
		defer admittedClient.Close()

		base.push(&taggedConn{Conn: deniedServer, remote: &taggedAddr{tag: "denied"}})
		base.push(&taggedConn{Conn: admittedServer, remote: &taggedAddr{tag: "admitted"}})

		// Accept skips the denied conn and returns the admitted one.
		accepted, err := listener.Accept()
		require.NoError(t, err)
		defer accepted.Close()

		// The denied conn should be closed by the listener.
		buf := make([]byte, 1)
		deniedClient.SetReadDeadline(time.Now().Add(100 * time.Millisecond)) //nolint:errcheck
		_, err = deniedClient.Read(buf)
		assert.Error(t, err, "denied conn should be closed by the listener")
	})

	require.NoError(t, listener.Close())
}
