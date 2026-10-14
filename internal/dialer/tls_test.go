package dialer

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// selfSigned mints a certificate for 127.0.0.1 and peer.example.com, and returns it with its key
// as the base64 PEM a config holds.
func selfSigned(t *testing.T) (tls.Certificate, string, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "stunner-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"peer.example.com"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	kp, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	return kp, base64.StdEncoding.EncodeToString(certPEM), base64.StdEncoding.EncodeToString(keyPEM)
}

// streamEcho echoes on every conn ln accepts, handing them to the test.
func streamEcho(t *testing.T, ln net.Listener) chan net.Conn {
	t.Helper()
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- c
			go func() { _, _ = io.Copy(c, c) }()
		}
	}()
	return accepted
}

func TestTLSDialer(t *testing.T) {
	rt := newTestRuntime(t)
	serverCert, ca, _ := selfSigned(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{serverCert}})
	require.NoError(t, err)
	streamEcho(t, ln)

	t.Run("verified by the CA against the host", func(t *testing.T) {
		cl := startDialer(t, rt, &stnrv2.ClusterConfig{Name: "tls", Protocol: "TCP",
			TLS: &stnrv2.TLSConfig{CA: ca}})
		c, err := cl.Dial(context.Background(), nil, ln.Addr().String())
		require.NoError(t, err)
		defer c.Close() //nolint:errcheck
		echo(t, c, "hello")
		assert.Equal(t, api.Tag{Name: "tls", Proto: stnrv2.ProtocolTCP}, c.Tag())
		_, framed := c.Channel(ln.Addr())
		assert.False(t, framed, "a secured conn reports no wire")
	})

	t.Run("verified against the SNI", func(t *testing.T) {
		cl := startDialer(t, rt, &stnrv2.ClusterConfig{Name: "tls-sni", Protocol: "TCP",
			TLS: &stnrv2.TLSConfig{CA: ca, SNI: "peer.example.com"}})
		c, err := cl.Dial(context.Background(), nil, ln.Addr().String())
		require.NoError(t, err)
		_ = c.Close()

		cl = startDialer(t, rt, &stnrv2.ClusterConfig{Name: "tls-wrong-sni", Protocol: "TCP",
			TLS: &stnrv2.TLSConfig{CA: ca, SNI: "other.example.com"}})
		_, err = cl.Dial(context.Background(), nil, ln.Addr().String())
		assert.Error(t, err, "a certificate not for the SNI is refused")
	})

	t.Run("refused by the system roots", func(t *testing.T) {
		cl := startDialer(t, rt, &stnrv2.ClusterConfig{Name: "tls-roots", Protocol: "TCP",
			TLS: &stnrv2.TLSConfig{}})
		_, err := cl.Dial(context.Background(), nil, ln.Addr().String())
		assert.Error(t, err)
	})

	t.Run("mutual TLS", func(t *testing.T) {
		_, clientCert, clientKey := selfSigned(t)
		mln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
			Certificates: []tls.Certificate{serverCert}, ClientAuth: tls.RequireAnyClientCert})
		require.NoError(t, err)
		accepted := streamEcho(t, mln)
		cl := startDialer(t, rt, &stnrv2.ClusterConfig{Name: "mtls", Protocol: "TCP",
			TLS: &stnrv2.TLSConfig{CA: ca, Cert: clientCert, Key: clientKey}})
		c, err := cl.Dial(context.Background(), nil, mln.Addr().String())
		require.NoError(t, err)
		defer c.Close() //nolint:errcheck
		echo(t, c, "hello")
		assert.Len(t, (<-accepted).(*tls.Conn).ConnectionState().PeerCertificates, 1,
			"the peer sees our certificate")
	})

	t.Run("a bad CA fails the dialer", func(t *testing.T) {
		conf := &stnrv2.ClusterConfig{Name: "bad-ca", Protocol: "TCP",
			TLS: &stnrv2.TLSConfig{CA: base64.StdEncoding.EncodeToString([]byte("no pem"))}}
		require.NoError(t, conf.Validate())
		_, err := New(conf, rt, func(net.Addr) bool { return true })
		assert.Error(t, err)
	})
}

func TestDTLSDialer(t *testing.T) {
	rt := newTestRuntime(t)
	serverCert, ca, _ := selfSigned(t)
	ln, err := dtls.Listen("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")},
		&dtls.Config{Certificates: []tls.Certificate{serverCert}})
	require.NoError(t, err)
	streamEcho(t, ln)

	cl := startDialer(t, rt, &stnrv2.ClusterConfig{Name: "dtls", Protocol: "UDP",
		TLS: &stnrv2.TLSConfig{CA: ca}})
	c, err := cl.Dial(context.Background(), nil, ln.Addr().String())
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck
	echo(t, c, "hello")

	// DTLS through a TURN tunnel: the leg must not report the channel, or the offload would
	// relay past the encryption
	tunnelled := startDialer(t, rt, &stnrv2.ClusterConfig{Name: "dtls-tunnel", Protocol: "UDP",
		Tunnel: turnServer(t, rt), TLS: &stnrv2.TLSConfig{CA: ca}})
	tc, err := tunnelled.Dial(context.Background(), nil, ln.Addr().String())
	require.NoError(t, err)
	defer tc.Close() //nolint:errcheck
	echo(t, tc, "hello")
	_, framed := tc.Channel(ln.Addr())
	assert.False(t, framed, "a secured conn reports no wire")
}
