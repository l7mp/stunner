package object_test

import (
	"encoding/base64"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
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
			return obj, base, &stnrv2.StunnerConfig{Auth: *staticAuthConfig()}
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
				full: &stnrv2.StunnerConfig{Auth: stnrv2.AuthConfig{
					Type:        stnrv2.AuthTypeStatic.String(),
					Realm:       "example.org",
					Credentials: map[string]string{"username": "user", "password": "pass"},
				}},
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
			return obj, base, &stnrv2.StunnerConfig{Auth: *staticAuthConfig()}
		},
		expectations: []inspectExpectation{
			{
				name: "public-addr-change-reconcile",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.PublicAddr = "198.51.100.1" }),
				want: runtime.ActionReconcile,
			},
			{
				name: "cert-change-restart",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.Cert = certB }),
				want: runtime.ActionRestart,
			},
			{
				name: "key-change-restart",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.Key = keyB }),
				want: runtime.ActionRestart,
			},
			{
				name: "pqc-mode-change-restart",
				conf: listener(func(c *stnrv2.ListenerConfig) { c.PQCMode = stnrv2.PQCModeEnforced.String() }),
				want: runtime.ActionRestart,
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
