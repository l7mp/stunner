package listener

import (
	"crypto/tls"
	"net"
	"strconv"
	"testing"

	"github.com/pion/logging"
	"github.com/pion/transport/v5/stdnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// TestTLSConfigDefault pins what the open-source build serves: the default TLS settings, whatever
// PQC mode the listener asks for.
func TestTLSConfigDefault(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	require.NoError(t, err)
	log := logging.NewDefaultLoggerFactory().NewLogger("test")
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	rt := newTestRuntime(t, nw)

	for _, mode := range []string{"", "preferred", "enforced"} {
		conf := &stnrv2.ListenerConfig{Name: "tls", Protocol: "TLS", PQCMode: mode}
		c := newTLSConfig(rt, conf, cert, log)
		assert.Equal(t, uint16(tls.VersionTLS12), c.MinVersion, "mode %q: default minimum version", mode)
		assert.Zero(t, c.MaxVersion, "mode %q: default maximum version", mode)
		assert.Nil(t, c.CurvePreferences, "mode %q: default key exchanges", mode)
		assert.Len(t, c.Certificates, 1, "mode %q: the listener certificate", mode)
	}
}

// TestTLSConfigSeam checks that a TLS listener takes its TLS configuration from the constructor
// variable, handing it the listener's config and certificate, and serves what it returns.
func TestTLSConfigSeam(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	rt := newTestRuntime(t, nw)

	var seen *stnrv2.ListenerConfig
	defer func(f func(*runtime.Runtime, *stnrv2.ListenerConfig, tls.Certificate, logging.LeveledLogger) *tls.Config) {
		newTLSConfig = f
	}(newTLSConfig)
	newTLSConfig = func(_ *runtime.Runtime, conf *stnrv2.ListenerConfig, cert tls.Certificate, _ logging.LeveledLogger) *tls.Config {
		seen = conf
		// a setting the default does not have, to tell the served config apart
		return &tls.Config{MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12,
			Certificates: []tls.Certificate{cert}}
	}

	port := freePort(t, "tcp")
	startListener(t, rt, &stnrv2.ListenerConfig{Name: "tls", Protocol: "TLS", Port: port,
		Servers: []string{"s"}, Cert: certPEM, Key: keyPEM, PQCMode: "preferred"})
	require.NotNil(t, seen, "the listener built its TLS configuration through the seam")
	assert.Equal(t, "tls", seen.Name)
	assert.Equal(t, "preferred", seen.PQCMode, "the seam sees the listener's PQC mode")

	c, err := tls.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), //nolint:noctx
		&tls.Config{InsecureSkipVerify: true}) //nolint:gosec
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck
	roundTrip(t, c, "hello")
	assert.Equal(t, uint16(tls.VersionTLS12), c.ConnectionState().Version,
		"the listener serves the seam's configuration")
}
