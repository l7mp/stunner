package turn

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
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

// stubObject registers a config in the runtime without the object behind it.
type stubObject struct {
	typ  objruntime.ObjectType
	conf stnrv1.Config
}

func (o stubObject) Name() string                { return o.conf.ConfigName() }
func (o stubObject) Type() objruntime.ObjectType { return o.typ }
func (stubObject) Start() error                  { return nil }
func (stubObject) Close(bool) error              { return nil }
func (o stubObject) GetConfig() stnrv1.Config    { return o.conf }
func (stubObject) Inspect(_, _ stnrv1.Config, _ *stnrv1.StunnerConfig) (objruntime.Action, error) {
	return objruntime.ActionNone, nil
}
func (stubObject) Reconcile(stnrv1.Config) error { return nil }
func (stubObject) Status() stnrv1.Status         { return nil }

// selfSignedPEM mints a throwaway certificate and key in PEM, as the listener holds them.
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

// TestTLSConfigDefault pins what the open-source build serves: the default TLS settings, whatever
// PQC mode the listener asks for.
func TestTLSConfigDefault(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	require.NoError(t, err)
	log := logging.NewDefaultLoggerFactory().NewLogger("test")

	for _, mode := range []string{"", "preferred", "enforced"} {
		conf := &stnrv1.ListenerConfig{Name: "tls", Protocol: "turn-tls", PQCMode: mode}
		c := newTLSConfig(nil, conf, cert, log)
		assert.Equal(t, uint16(tls.VersionTLS12), c.MinVersion, "mode %q: default minimum version", mode)
		assert.Len(t, c.Certificates, 1, "mode %q: the listener certificate", mode)
	}
}

// TestTLSConfigSeam checks that a TURN-TLS server takes its TLS configuration from the
// constructor variable, handing it the listener's config.
func TestTLSConfigSeam(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	logf := logger.NewLoggerFactory("all:ERROR")
	tm, err := telemetry.New(telemetry.Callbacks{}, false, logf.NewLogger("metric"))
	require.NoError(t, err)
	defer tm.Close() //nolint:errcheck

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := probe.Addr().(*net.TCPAddr).Port
	require.NoError(t, probe.Close())

	rt := objruntime.New(objruntime.Config{Logger: logf, Telemetry: tm})
	require.NoError(t, rt.Registry.Add(stubObject{typ: objruntime.TypeAuth, conf: &stnrv1.AuthConfig{
		Type: "static", Realm: "test", Credentials: map[string]string{"username": "u", "password": "p"},
	}}, nil))
	require.NoError(t, rt.Registry.Add(stubObject{typ: objruntime.TypeListener, conf: &stnrv1.ListenerConfig{
		Name: "tls", Protocol: "turn-tls", Addr: "127.0.0.1", Port: port, Cert: certPEM, Key: keyPEM,
		PQCMode: "preferred",
	}}, nil))

	var seen *stnrv1.ListenerConfig
	defer func(f func(*objruntime.Runtime, *stnrv1.ListenerConfig, tls.Certificate, logging.LeveledLogger) *tls.Config) {
		newTLSConfig = f
	}(newTLSConfig)
	newTLSConfig = func(_ *objruntime.Runtime, conf *stnrv1.ListenerConfig, cert tls.Certificate, _ logging.LeveledLogger) *tls.Config {
		seen = conf
		return &tls.Config{Certificates: []tls.Certificate{cert}}
	}

	s, err := NewServer("tls", stnrv1.ProtocolTURNTLS, rt)
	require.NoError(t, err, "TURN-TLS server")
	defer s.Close() //nolint:errcheck

	require.NotNil(t, seen, "the server built its TLS configuration through the seam")
	assert.Equal(t, "preferred", seen.PQCMode, "the seam sees the listener's PQC mode")
}
