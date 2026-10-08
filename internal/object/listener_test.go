package object_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/logging"
	"github.com/pion/transport/v5"
	"github.com/pion/transport/v5/stdnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

func TestListenerObjectSemantics(t *testing.T) {
	// listener returns the base config with one change applied.
	listener := func(mod func(*stnrv2.ListenerConfig)) *stnrv2.ListenerConfig {
		c := &stnrv2.ListenerConfig{
			Name:     "listener-a",
			Protocol: stnrv2.ProtocolUDP.String(),
			Servers:  []string{"server-a"},
			Addr:     "127.0.0.1",
			Port:     3478,
		}
		mod(c)
		require.NoError(t, c.Validate())
		return c
	}

	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "listener",
		setup: func(t *testing.T) (runtime.Object, stnrv2.Config, *stnrv2.StunnerConfig) {
			env := newTestEnv()
			base := listener(func(*stnrv2.ListenerConfig) {})
			obj, err := object.NewListener(base, env.rt)
			require.NoError(t, err)
			return obj, base, &stnrv2.StunnerConfig{Auths: []stnrv2.AuthConfig{*staticAuthConfig()}}
		},
		expectations: []inspectExpectation{
			{
				name: "unchanged-none",
				conf: listener(func(*stnrv2.ListenerConfig) {}),
				want: runtime.ActionNone,
			},
			{
				name: "public-addr-change-reconcile",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.PublicAddr = "198.51.100.1" }),
				want: runtime.ActionReconcile,
			},
			{
				name: "public-port-change-reconcile",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.PublicPort = 12345 }),
				want: runtime.ActionReconcile,
			},
			{
				name: "port-change-restart",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.Port = 3479 }),
				want: runtime.ActionRestart,
			},
			{
				name: "protocol-change-restart",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.Protocol = stnrv2.ProtocolTCP.String() }),
				want: runtime.ActionRestart,
			},
			{
				name: "address-change-restart",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.Addr = "127.0.0.2" }),
				want: runtime.ActionRestart,
			},
			{
				// the server is resolved by name for every conn: no restart
				name: "server-change-reconcile",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.Servers = []string{"server-b"} }),
				want: runtime.ActionReconcile,
			},
			{
				// the realm lives on the TURN server, not on the listener feeding it
				name: "auth-realm-change-none",
				conf: listener(func(*stnrv2.ListenerConfig) {}),
				full: &stnrv2.StunnerConfig{Auths: []stnrv2.AuthConfig{{
					Type:        stnrv2.AuthTypeStatic.String(),
					Realm:       "example.org",
					Credentials: map[string]string{"username": "user", "password": "pass"},
				}}},
				want: runtime.ActionNone,
			},
		},
	})
}

func TestListenerTLSObjectSemantics(t *testing.T) {
	certA := base64.StdEncoding.EncodeToString([]byte("cert-a"))
	keyA := base64.StdEncoding.EncodeToString([]byte("key-a"))
	certB := base64.StdEncoding.EncodeToString([]byte("cert-b"))
	keyB := base64.StdEncoding.EncodeToString([]byte("key-b"))

	// listener returns the base config with one change applied.
	listener := func(mod func(*stnrv2.ListenerConfig)) *stnrv2.ListenerConfig {
		c := &stnrv2.ListenerConfig{
			Name:     "listener-tls",
			Protocol: stnrv2.ProtocolTLS.String(),
			Servers:  []string{"server-a"},
			Addr:     "127.0.0.1",
			Port:     5349,
			Cert:     certA,
			Key:      keyA,
		}
		mod(c)
		require.NoError(t, c.Validate())
		return c
	}

	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "listener-tls",
		setup: func(t *testing.T) (runtime.Object, stnrv2.Config, *stnrv2.StunnerConfig) {
			env := newTestEnv()
			base := listener(func(*stnrv2.ListenerConfig) {})
			obj, err := object.NewListener(base, env.rt)
			require.NoError(t, err)
			return obj, base, &stnrv2.StunnerConfig{Auths: []stnrv2.AuthConfig{*staticAuthConfig()}}
		},
		expectations: []inspectExpectation{
			{
				name: "public-addr-change-reconcile",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.PublicAddr = "198.51.100.1" }),
				want: runtime.ActionReconcile,
			},
			{
				name: "cert-change-reconcile",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.Cert = certB }),
				want: runtime.ActionReconcile,
			},
			{
				name: "key-change-reconcile",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.Key = keyB }),
				want: runtime.ActionReconcile,
			},
			{
				name: "pqc-mode-change-reconcile",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.PQCMode = stnrv2.PQCModeEnforced.String() }),
				want: runtime.ActionReconcile,
			},
		},
	})
}

// fakeServer records the conns handed to it and consumes them, or passes them on wrapped when
// its verdict is Next.
type fakeServer struct {
	name    string
	verdict api.Verdict
	served  chan api.Conn
}

func (s *fakeServer) Name() string             { return s.name }
func (s *fakeServer) Type() runtime.ObjectType { return runtime.TypeServer }
func (s *fakeServer) Start() error             { return nil }
func (s *fakeServer) Close(bool) error         { return nil }
func (s *fakeServer) Sessions() int            { return 0 }
func (s *fakeServer) Serve(c api.Conn) (api.Conn, api.Verdict, error) {
	s.served <- c
	if s.verdict == api.Next {
		return &passedConn{Conn: c, by: s.name}, api.Next, nil
	}
	return nil, api.Consumed, nil
}

// passedConn is a conn a server passed on down the chain.
type passedConn struct {
	api.Conn
	by string
}

// TestListenerServesByName pins that a listener hands every conn to the first server its config
// names, resolved by name when the conn arrives, that a server consuming the conn ends the chain,
// and that a change of its servers applies to the next conn without a restart.
func TestListenerServesByName(t *testing.T) {
	env := newLiveEnv(t, nil)
	a := &fakeServer{name: "server-a", served: make(chan api.Conn, 8)}
	b := &fakeServer{name: "server-b", served: make(chan api.Conn, 8)}
	require.NoError(t, env.rt.Registry.Add(a, nil))
	require.NoError(t, env.rt.Registry.Add(b, nil))

	port := freePort(t, "udp")
	conf := &stnrv2.ListenerConfig{Name: "listener-a", Protocol: stnrv2.ProtocolUDP.String(),
		Servers: []string{"server-a", "server-b"}, Addr: "127.0.0.1", Port: port}
	l, err := object.NewListener(conf, env.rt)
	require.NoError(t, err)
	require.NoError(t, env.rt.Registry.Add(l, nil))
	require.NoError(t, l.Start())
	t.Cleanup(func() { _ = l.Close(false) })

	send := func() {
		c, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) //nolint:noctx
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		_, err = c.Write([]byte("hello"))
		require.NoError(t, err)
	}

	send()
	select {
	case c := <-a.served:
		assert.Equal(t, "listener-a", c.Tag().Name, "the first server takes the conn")
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the listener handed no conn to its first server")
	}

	conf.Servers = []string{"server-b"}
	require.NoError(t, l.Reconcile(conf))
	send()
	select {
	case <-b.served:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the listener did not follow its new server")
	}
	assert.Empty(t, a.served)
}

// TestListenerServerChain pins that a server passing a conn on hands the conn it returns to the
// next server of the chain.
func TestListenerServerChain(t *testing.T) {
	env := newLiveEnv(t, nil)
	a := &fakeServer{name: "server-a", verdict: api.Next, served: make(chan api.Conn, 8)}
	b := &fakeServer{name: "server-b", served: make(chan api.Conn, 8)}
	require.NoError(t, env.rt.Registry.Add(a, nil))
	require.NoError(t, env.rt.Registry.Add(b, nil))

	port := freePort(t, "udp")
	conf := &stnrv2.ListenerConfig{Name: "listener-a", Protocol: stnrv2.ProtocolUDP.String(),
		Servers: []string{"server-a", "server-b"}, Addr: "127.0.0.1", Port: port}
	l, err := object.NewListener(conf, env.rt)
	require.NoError(t, err)
	require.NoError(t, env.rt.Registry.Add(l, nil))
	require.NoError(t, l.Start())
	t.Cleanup(func() { _ = l.Close(false) })

	c, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) //nolint:noctx
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Write([]byte("hello"))
	require.NoError(t, err)

	select {
	case <-a.served:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the first server got no conn")
	}
	select {
	case got := <-b.served:
		passed, ok := got.(*passedConn)
		require.True(t, ok, "the next server gets the conn the first one returned")
		assert.Equal(t, "server-a", passed.by)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the first server's conn did not reach the next server")
	}
}

func newTestRuntime(t *testing.T, nw transport.Net) *runtime.Runtime {
	t.Helper()
	log := logger.NewLoggerFactory("all:ERROR")
	tm, err := telemetry.New(telemetry.Callbacks{}, true, log.NewLogger("telemetry"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tm.Close() })
	return runtime.New(runtime.Config{Logger: log, DryRun: true, Telemetry: tm, Net: nw})
}

// startListener starts a listener whose sink echoes every conn it emits; the emitted conns are
// also handed to the test.
func startListener(t *testing.T, rt *runtime.Runtime, conf *stnrv2.ListenerConfig) chan api.Conn {
	t.Helper()
	require.NoError(t, conf.Validate())
	accepted := make(chan api.Conn, 256)
	l, err := object.NewListener(conf, rt)
	require.NoError(t, err)
	l.SetSink(func(c api.Conn) {
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
	require.NoError(t, l.Start())
	t.Cleanup(func() { _ = l.Close(false) })
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
			Port: port, Servers: []string{"s"}, Labels: map[string]string{"role": "control"}})
		c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port))) //nolint:noctx
		require.NoError(t, err)
		defer c.Close() //nolint:errcheck
		roundTrip(t, c, "hello")
		assert.Equal(t, api.Tag{Name: "tcp", Proto: stnrv2.ProtocolTCP,
			Labels: map[string]string{"role": "control"}}, (<-accepted).Tag(), "the conn carries the labels")
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

// selfSignedPEM mints a throwaway certificate and key in base64 PEM, as a listener config holds
// them.
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
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

// TestReconcileLive pins that Reconcile applies to the next conns of a running listener, and that
// a certificate that does not load is refused and the previous one kept.
func TestReconcileLive(t *testing.T) {
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	rt := newTestRuntime(t, nw)
	certA, keyA := selfSignedPEM(t)
	certB, keyB := selfSignedPEM(t)
	port := freePort(t, "tcp")
	conf := &stnrv2.ListenerConfig{Name: "tls", Protocol: "TLS", Port: port, Servers: []string{"s"},
		Cert: certA, Key: keyA}
	require.NoError(t, conf.Validate())
	accepted := make(chan api.Conn, 8)
	l, err := object.NewListener(conf, rt)
	require.NoError(t, err)
	l.SetSink(func(c api.Conn) { accepted <- c })
	require.NoError(t, l.Start())
	defer l.Close(false) //nolint:errcheck

	served := func() []byte {
		c, err := tls.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), //nolint:noctx
			&tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		require.NoError(t, err)
		defer c.Close() //nolint:errcheck
		return c.ConnectionState().PeerCertificates[0].Raw
	}
	first := served()
	assert.Nil(t, (<-accepted).Tag().Labels)

	upd := *conf
	upd.Cert, upd.Key, upd.Labels = certB, keyB, map[string]string{"role": "control"}
	require.NoError(t, l.Reconcile(&upd))
	second := served()
	assert.NotEqual(t, first, second, "the next handshake serves the new certificate")
	assert.Equal(t, map[string]string{"role": "control"}, (<-accepted).Tag().Labels,
		"the next conn carries the new labels")

	bad := upd
	bad.Cert = "garbage"
	assert.Error(t, l.Reconcile(&bad))
	assert.Equal(t, second, served(), "a certificate that does not load keeps the previous one")
}

// TestTLSConfigDefault pins what the open-source build serves: the default TLS settings, whatever
// PQC mode the listener asks for.
func TestTLSConfigDefault(t *testing.T) {
	certB64, keyB64 := selfSignedPEM(t)
	certPEM, err := base64.StdEncoding.DecodeString(certB64)
	require.NoError(t, err)
	keyPEM, err := base64.StdEncoding.DecodeString(keyB64)
	require.NoError(t, err)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	log := logging.NewDefaultLoggerFactory().NewLogger("test")
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	rt := newTestRuntime(t, nw)

	for _, mode := range []stnrv2.PQCMode{stnrv2.PQCModeDefault, stnrv2.PQCModePreferred, stnrv2.PQCModeEnforced} {
		c := (*object.NewTLSConfig)(rt, mode, cert, log)
		assert.Equal(t, uint16(tls.VersionTLS12), c.MinVersion, "mode %q: default minimum version", mode)
		assert.Zero(t, c.MaxVersion, "mode %q: default maximum version", mode)
		assert.Nil(t, c.CurvePreferences, "mode %q: default key exchanges", mode)
		assert.Len(t, c.Certificates, 1, "mode %q: the listener certificate", mode)
	}
}

// TestTLSConfigSeam checks that a TLS listener takes its TLS configuration from the constructor
// variable, handing it the listener's PQC mode and certificate, and serves what it returns.
func TestTLSConfigSeam(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	rt := newTestRuntime(t, nw)

	seen := stnrv2.PQCModeDefault
	defer func(f func(*runtime.Runtime, stnrv2.PQCMode, tls.Certificate, logging.LeveledLogger) *tls.Config) {
		*object.NewTLSConfig = f
	}(*object.NewTLSConfig)
	*object.NewTLSConfig = func(_ *runtime.Runtime, mode stnrv2.PQCMode, cert tls.Certificate, _ logging.LeveledLogger) *tls.Config {
		seen = mode
		// a setting the default does not have, to tell the served config apart
		return &tls.Config{MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12,
			Certificates: []tls.Certificate{cert}}
	}

	port := freePort(t, "tcp")
	startListener(t, rt, &stnrv2.ListenerConfig{Name: "tls", Protocol: "TLS", Port: port,
		Servers: []string{"s"}, Cert: certPEM, Key: keyPEM, PQCMode: "preferred"})

	c, err := tls.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), //nolint:noctx
		&tls.Config{InsecureSkipVerify: true}) //nolint:gosec
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck
	roundTrip(t, c, "hello")
	assert.Equal(t, stnrv2.PQCModePreferred, seen, "the seam sees the listener's PQC mode")
	assert.Equal(t, uint16(tls.VersionTLS12), c.ConnectionState().Version,
		"the listener serves the seam's configuration")
}
