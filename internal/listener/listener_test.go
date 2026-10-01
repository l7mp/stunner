package listener

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/transport/v5"
	"github.com/pion/transport/v5/stdnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

func newTestRuntime(t *testing.T, nw transport.Net) *runtime.Runtime {
	t.Helper()
	log := logger.NewLoggerFactory("all:ERROR")
	tm, err := telemetry.New(telemetry.Callbacks{}, true, log.NewLogger("telemetry"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tm.Close() })
	return runtime.New(runtime.Config{Logger: log, DryRun: true, Telemetry: tm, Net: nw})
}

func freePort(t *testing.T, network string) int {
	t.Helper()
	var addr net.Addr
	if network == "tcp" {
		l, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
		require.NoError(t, err)
		addr = l.Addr()
		require.NoError(t, l.Close())
	} else {
		c, err := net.ListenPacket("udp", "127.0.0.1:0") //nolint:noctx
		require.NoError(t, err)
		addr = c.LocalAddr()
		require.NoError(t, c.Close())
	}
	_, port, err := net.SplitHostPort(addr.String())
	require.NoError(t, err)
	p, err := strconv.Atoi(port)
	require.NoError(t, err)
	return p
}

// startListener starts a listener whose sink echoes every conn it emits; the emitted conns are
// also handed to the test.
func startListener(t *testing.T, rt *runtime.Runtime, conf *stnrv2.ListenerConfig) chan api.Conn {
	t.Helper()
	require.NoError(t, conf.Validate())
	accepted := make(chan api.Conn, 256)
	l, err := New(conf, rt, func(c api.Conn) {
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
	})
	require.NoError(t, err)
	require.NoError(t, l.Start())
	t.Cleanup(func() { _ = l.Close() })
	return accepted
}

// roundTrip writes msg and expects it echoed back.
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

func TestListeners(t *testing.T) {
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	rt := newTestRuntime(t, nw)
	cert, key := selfSignedPEM(t)

	t.Run("UDP", func(t *testing.T) {
		port := freePort(t, "udp")
		accepted := startListener(t, rt, &stnrv2.ListenerConfig{Name: "udp", Protocol: "UDP",
			Port: port, Servers: []string{"s"}})
		c, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) //nolint:noctx
		require.NoError(t, err)
		defer c.Close() //nolint:errcheck
		roundTrip(t, c, "hello")
		roundTrip(t, c, "again")
		assert.Equal(t, api.Tag{Name: "udp", Proto: stnrv2.ProtocolUDP}, (<-accepted).Tag())
		assert.Empty(t, accepted, "one conn per client")
	})

	t.Run("TCP", func(t *testing.T) {
		port := freePort(t, "tcp")
		accepted := startListener(t, rt, &stnrv2.ListenerConfig{Name: "tcp", Protocol: "TCP",
			Port: port, Servers: []string{"s"}})
		c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) //nolint:noctx
		require.NoError(t, err)
		defer c.Close() //nolint:errcheck
		roundTrip(t, c, "hello")
		assert.Equal(t, api.Tag{Name: "tcp", Proto: stnrv2.ProtocolTCP}, (<-accepted).Tag())
	})

	t.Run("TLS", func(t *testing.T) {
		port := freePort(t, "tcp")
		accepted := startListener(t, rt, &stnrv2.ListenerConfig{Name: "tls", Protocol: "TLS",
			Port: port, Servers: []string{"s"}, Cert: cert, Key: key})
		c, err := tls.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), //nolint:noctx
			&tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		require.NoError(t, err)
		defer c.Close() //nolint:errcheck
		roundTrip(t, c, "hello")
		assert.Equal(t, api.Tag{Name: "tls", Proto: stnrv2.ProtocolTLS}, (<-accepted).Tag())
	})

	t.Run("TLSHandshakeBeforeAccept", func(t *testing.T) {
		port := freePort(t, "tcp")
		accepted := startListener(t, rt, &stnrv2.ListenerConfig{Name: "tls", Protocol: "TLS",
			Port: port, Servers: []string{"s"}, Cert: cert, Key: key})
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

		// a client that never handshakes is not emitted, and does not hold up the next one
		stalled, err := net.Dial("tcp", addr) //nolint:noctx
		require.NoError(t, err)
		defer stalled.Close()                                                  //nolint:errcheck
		c, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:noctx,gosec
		require.NoError(t, err)
		defer c.Close() //nolint:errcheck
		roundTrip(t, c, "hello")

		assert.Equal(t, c.LocalAddr().String(), (<-accepted).RemoteAddr().String(),
			"the handshaken client")
		assert.Empty(t, accepted, "the stalled client is not emitted")
	})

	t.Run("DTLS", func(t *testing.T) {
		port := freePort(t, "udp")
		accepted := startListener(t, rt, &stnrv2.ListenerConfig{Name: "dtls", Protocol: "DTLS",
			Port: port, Servers: []string{"s"}, Cert: cert, Key: key})
		sock, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
		require.NoError(t, err)
		c, err := dtls.ClientWithOptions(sock, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port},
			dtls.WithInsecureSkipVerify(true))
		require.NoError(t, err)
		defer c.Close() //nolint:errcheck
		roundTrip(t, c, "hello")
		assert.Equal(t, api.Tag{Name: "dtls", Proto: stnrv2.ProtocolDTLS}, (<-accepted).Tag())
	})
}

// TestListenerBindAddr pins that a listener binds its address, and every address without one.
func TestListenerBindAddr(t *testing.T) {
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	rt := newTestRuntime(t, nw)

	port := freePort(t, "tcp")
	accepted := startListener(t, rt, &stnrv2.ListenerConfig{Name: "tcp", Protocol: "TCP",
		Addr: "127.0.0.1", Port: port, Servers: []string{"s"}})
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) //nolint:noctx
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck
	roundTrip(t, c, "hello")
	assert.Equal(t, "127.0.0.1:"+strconv.Itoa(port), (<-accepted).LocalAddr().String(),
		"bound to the listener's address")

	port = freePort(t, "tcp")
	accepted = startListener(t, rt, &stnrv2.ListenerConfig{Name: "any", Protocol: "TCP",
		Port: port, Servers: []string{"s"}})
	c, err = net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) //nolint:noctx
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck
	roundTrip(t, c, "hello")
	<-accepted
}

// selfSignedPEM mints a throwaway certificate and key in PEM, as a listener config holds them.
func selfSignedPEM(t *testing.T) (cert, key string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "stunner-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}
