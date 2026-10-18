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
	"io"
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
				name: "connect-change-restart",
				conf: listener(func(c *stnrv2.ListenerConfig) {
					c.Connect = &stnrv2.ListenerConnectConfig{Address: "127.0.0.1:5060"}
				}),
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
			TLS:      &stnrv2.TLSConfig{Cert: certA, Key: keyA},
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
				conf: listener(func(c *stnrv2.ListenerConfig) { c.TLS.Cert = certB }),
				want: runtime.ActionReconcile,
			},
			{
				name: "key-change-reconcile",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.TLS.Key = keyB }),
				want: runtime.ActionReconcile,
			},
			{
				name: "pqc-mode-change-reconcile",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.TLS.PQCMode = stnrv2.PQCModeEnforced.String() }),
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
			Port: port, Servers: []string{"s"}, TLS: &stnrv2.TLSConfig{Cert: cert, Key: key}})
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
			Port: port, Servers: []string{"s"}, TLS: &stnrv2.TLSConfig{Cert: cert, Key: key}})
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
			Port: port, Servers: []string{"s"}, TLS: &stnrv2.TLSConfig{Cert: cert, Key: key}})
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
		TLS: &stnrv2.TLSConfig{Cert: certA, Key: keyA}}
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
	upd.TLS, upd.Labels = &stnrv2.TLSConfig{Cert: certB, Key: keyB}, map[string]string{"role": "control"}
	require.NoError(t, l.Reconcile(&upd))
	second := served()
	assert.NotEqual(t, first, second, "the next handshake serves the new certificate")
	assert.Equal(t, map[string]string{"role": "control"}, (<-accepted).Tag().Labels,
		"the next conn carries the new labels")

	bad := upd
	bad.TLS = &stnrv2.TLSConfig{Cert: base64.StdEncoding.EncodeToString([]byte("garbage")), Key: keyB}
	assert.Error(t, l.Reconcile(&bad))
	assert.Equal(t, second, served(), "a certificate that does not load keeps the previous one")
}

// TestTLSConfigPQC checks the TLS settings of each PQC mode.
func TestTLSConfigPQC(t *testing.T) {
	certB64, keyB64 := selfSignedPEM(t)
	certPEM, err := base64.StdEncoding.DecodeString(certB64)
	require.NoError(t, err)
	keyPEM, err := base64.StdEncoding.DecodeString(keyB64)
	require.NoError(t, err)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	log := logging.NewDefaultLoggerFactory().NewLogger("test")

	c := object.NewTLSConfig(stnrv2.PQCModeDefault, cert, log)
	assert.Equal(t, uint16(tls.VersionTLS12), c.MinVersion)
	assert.Zero(t, c.MaxVersion)
	assert.Nil(t, c.CurvePreferences, "the Go runtime picks the key exchange")
	assert.Len(t, c.Certificates, 1)

	c = object.NewTLSConfig(stnrv2.PQCModePreferred, cert, log)
	assert.Equal(t, uint16(tls.VersionTLS12), c.MinVersion)
	assert.Equal(t, []tls.CurveID{tls.X25519MLKEM768, tls.X25519, tls.CurveP256}, c.CurvePreferences)

	c = object.NewTLSConfig(stnrv2.PQCModeEnforced, cert, log)
	assert.Equal(t, uint16(tls.VersionTLS13), c.MinVersion)
	assert.Equal(t, uint16(tls.VersionTLS13), c.MaxVersion)
	assert.Equal(t, []tls.CurveID{tls.X25519MLKEM768}, c.CurvePreferences)
}

// TestTLSListenerPQCEnforced checks that a listener enforcing PQC serves a hybrid key exchange
// and refuses a client that cannot do one.
func TestTLSListenerPQCEnforced(t *testing.T) {
	certPEM, keyPEM := selfSignedPEM(t)
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	rt := newTestRuntime(t, nw)
	port := freePort(t, "tcp")
	startListener(t, rt, &stnrv2.ListenerConfig{Name: "tls", Protocol: "TLS", Port: port,
		Servers: []string{"s"}, TLS: &stnrv2.TLSConfig{Cert: certPEM, Key: keyPEM, PQCMode: "enforced"}})
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	c, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:noctx,gosec
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck
	roundTrip(t, c, "hello")
	assert.Equal(t, tls.X25519MLKEM768, c.ConnectionState().CurveID, "the hybrid key exchange")

	_, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, //nolint:noctx,gosec
		CurvePreferences: []tls.CurveID{tls.X25519}})
	assert.Error(t, err, "a classical client is refused")
}

// startDialer starts a dialing listener whose emitted conns are handed to the test.
func startDialer(t *testing.T, rt *runtime.Runtime, conf *stnrv2.ListenerConfig) (*object.Listener, chan api.Conn) {
	t.Helper()
	require.NoError(t, conf.Validate())
	emitted := make(chan api.Conn, 16)
	l, err := object.NewListener(conf, rt)
	require.NoError(t, err)
	l.SetSink(func(c api.Conn) { emitted <- c })
	require.NoError(t, l.Start())
	t.Cleanup(func() { _ = l.Close(false) })
	return l, emitted
}

// next returns the next emitted conn, failing the test after a few seconds.
func next(t *testing.T, emitted chan api.Conn) api.Conn {
	t.Helper()
	select {
	case c := <-emitted:
		return c
	case <-time.After(5 * time.Second):
		require.FailNow(t, "no conn emitted")
		return nil
	}
}

// echoStream serves an echo on every conn the listener accepts, handing the accepted conns to
// the test.
func echoStream(t *testing.T, ln net.Listener) chan net.Conn {
	t.Helper()
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 16)
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

// echoUDP serves an echo on a UDP socket.
func echoUDP(t *testing.T) string {
	t.Helper()
	sock, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sock.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := sock.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = sock.WriteTo(buf[:n], addr)
		}
	}()
	return sock.LocalAddr().String()
}

// keyPair decodes the base64 PEM certificate and key of a config.
func keyPair(t *testing.T, cert, key string) tls.Certificate {
	t.Helper()
	c, err := base64.StdEncoding.DecodeString(cert)
	require.NoError(t, err)
	k, err := base64.StdEncoding.DecodeString(key)
	require.NoError(t, err)
	kp, err := tls.X509KeyPair(c, k)
	require.NoError(t, err)
	return kp
}

func TestDialingListener(t *testing.T) {
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	rt := newTestRuntime(t, nw)
	cert, key := selfSignedPEM(t)
	serverCert := keyPair(t, cert, key)
	labels := map[string]string{stnrv2.RegistrationLabel: "carrier"}

	t.Run("UDP", func(t *testing.T) {
		peer := echoUDP(t)
		_, emitted := startDialer(t, rt, &stnrv2.ListenerConfig{Name: "udp", Protocol: "UDP",
			Servers: []string{"s"}, Labels: labels,
			Connect: &stnrv2.ListenerConnectConfig{Address: peer}})
		c := next(t, emitted)
		roundTrip(t, c, "hello")
		assert.Equal(t, api.Tag{Name: "udp", Proto: stnrv2.ProtocolUDP, Labels: labels}, c.Tag())
		assert.Equal(t, peer, c.RemoteAddr().String())
	})

	t.Run("TCP", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
		require.NoError(t, err)
		echoStream(t, ln)
		_, emitted := startDialer(t, rt, &stnrv2.ListenerConfig{Name: "tcp", Protocol: "TCP",
			Servers: []string{"s"}, Connect: &stnrv2.ListenerConnectConfig{Address: ln.Addr().String()}})
		c := next(t, emitted)
		roundTrip(t, c, "hello")
		assert.Equal(t, api.Tag{Name: "tcp", Proto: stnrv2.ProtocolTCP}, c.Tag())
	})

	t.Run("TLS verified by the CA", func(t *testing.T) {
		ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{serverCert}})
		require.NoError(t, err)
		echoStream(t, ln)
		_, emitted := startDialer(t, rt, &stnrv2.ListenerConfig{Name: "tls", Protocol: "TLS",
			Servers: []string{"s"}, Connect: &stnrv2.ListenerConnectConfig{Address: ln.Addr().String(),
				TLS: &stnrv2.TLSConfig{CA: cert}}})
		c := next(t, emitted)
		roundTrip(t, c, "hello")
		assert.Equal(t, api.Tag{Name: "tls", Proto: stnrv2.ProtocolTLS}, c.Tag())
	})

	t.Run("TLS refused by the system roots", func(t *testing.T) {
		ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{serverCert}})
		require.NoError(t, err)
		echoStream(t, ln)
		_, emitted := startDialer(t, rt, &stnrv2.ListenerConfig{Name: "tls", Protocol: "TLS",
			Servers: []string{"s"}, Connect: &stnrv2.ListenerConnectConfig{Address: ln.Addr().String()}})
		select {
		case <-emitted:
			assert.Fail(t, "a peer with an untrusted certificate is emitted")
		case <-time.After(200 * time.Millisecond):
		}
	})

	t.Run("mutual TLS", func(t *testing.T) {
		clientCert, clientKey := selfSignedPEM(t)
		ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{serverCert},
			ClientAuth: tls.RequireAnyClientCert})
		require.NoError(t, err)
		accepted := echoStream(t, ln)
		_, emitted := startDialer(t, rt, &stnrv2.ListenerConfig{Name: "mtls", Protocol: "TLS",
			Servers: []string{"s"}, Connect: &stnrv2.ListenerConnectConfig{Address: ln.Addr().String(),
				TLS: &stnrv2.TLSConfig{Cert: clientCert, Key: clientKey, Insecure: true}}})
		roundTrip(t, next(t, emitted), "hello")
		peerSide := (<-accepted).(*tls.Conn)
		require.Len(t, peerSide.ConnectionState().PeerCertificates, 1)
		assert.Equal(t, keyPair(t, clientCert, clientKey).Certificate[0],
			peerSide.ConnectionState().PeerCertificates[0].Raw, "the peer sees the client certificate")
	})

	t.Run("DTLS", func(t *testing.T) {
		ln, err := dtls.Listen("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")},
			&dtls.Config{Certificates: []tls.Certificate{serverCert}})
		require.NoError(t, err)
		echoStream(t, ln)
		_, emitted := startDialer(t, rt, &stnrv2.ListenerConfig{Name: "dtls", Protocol: "DTLS",
			Servers: []string{"s"}, Connect: &stnrv2.ListenerConnectConfig{Address: ln.Addr().String(),
				TLS: &stnrv2.TLSConfig{CA: cert}}})
		c := next(t, emitted)
		roundTrip(t, c, "hello")
		assert.Equal(t, api.Tag{Name: "dtls", Proto: stnrv2.ProtocolDTLS}, c.Tag())
	})

	t.Run("filters check the peer", func(t *testing.T) {
		deny, err := object.NewFilter(&stnrv2.FilterConfig{Name: "deny-loopback",
			Rules: []stnrv2.FilterRule{{Action: "DENY"}}})
		require.NoError(t, err)
		require.NoError(t, rt.Registry.Add(deny, nil))
		ln, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
		require.NoError(t, err)
		echoStream(t, ln)
		_, emitted := startDialer(t, rt, &stnrv2.ListenerConfig{Name: "filtered", Protocol: "TCP",
			Servers: []string{"s"}, Filters: []string{"deny-loopback"},
			Connect: &stnrv2.ListenerConnectConfig{Address: ln.Addr().String()}})
		select {
		case <-emitted:
			assert.Fail(t, "a peer the filters deny is emitted")
		case <-time.After(200 * time.Millisecond):
		}
	})

	t.Run("config round trip", func(t *testing.T) {
		conf := &stnrv2.ListenerConfig{Name: "rt", Protocol: "TLS", Servers: []string{"s"},
			Connect: &stnrv2.ListenerConnectConfig{Address: "sip.example.com:5061",
				TLS: &stnrv2.TLSConfig{SNI: "sip", CA: cert, Insecure: true}}}
		require.NoError(t, conf.Validate())
		l, err := object.NewListener(conf, rt)
		require.NoError(t, err)
		assert.True(t, conf.DeepEqual(l.GetConfig()), "GetConfig returns the connect block")
	})
}
