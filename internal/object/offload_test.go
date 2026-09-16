package object_test

import (
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v5/stdnet"
	"github.com/pion/turn/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/object/l4"
	objectturn "github.com/l7mp/stunner/v2/internal/object/turn"
	"github.com/l7mp/stunner/v2/internal/offload"
	"github.com/l7mp/stunner/v2/internal/quota"
	"github.com/l7mp/stunner/v2/internal/resolver"
	"github.com/l7mp/stunner/v2/internal/router"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

func TestOffloadObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "offload",
		setup: func(t *testing.T) (runtime.Object, stnrv1.Config, *stnrv1.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewOffload(nil, env.rt)
			require.NoError(t, err)
			return obj, &object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String()}, &stnrv1.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{name: "engine-change-restart", conf: &object.OffloadConfig{Engine: stnrv1.OffloadEngineAuto.String()}, want: runtime.ActionRestart},
			{name: "interfaces-change-restart", conf: &object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String(), Interfaces: []string{"eth0"}}, want: runtime.ActionRestart},
			{name: "same-config-none", conf: &object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String()}, want: runtime.ActionNone},
		},
	})
}

func TestOffloadWithInterfacesObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "offload-with-interfaces",
		setup: func(t *testing.T) (runtime.Object, stnrv1.Config, *stnrv1.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewOffload(nil, env.rt)
			require.NoError(t, err)
			return obj, &object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String(), Interfaces: []string{"eth0"}}, &stnrv1.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{name: "same-config-none", conf: &object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String(), Interfaces: []string{"eth0"}}, want: runtime.ActionNone},
			{name: "engine-change-restart", conf: &object.OffloadConfig{Engine: stnrv1.OffloadEngineAuto.String(), Interfaces: []string{"eth0"}}, want: runtime.ActionRestart},
			{name: "interfaces-change-restart", conf: &object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String(), Interfaces: []string{"eth1"}}, want: runtime.ActionRestart},
		},
	})
}

// offloadCall is one registration or removal as the engine saw it.
type offloadCall struct {
	client, peer      offload.Connection
	listener, cluster string
}

// spyEngine records the registrations and removals a caller attempts, over the null engine.
type spyEngine struct {
	offload.Engine
	mu               sync.Mutex
	upserts, removes []offloadCall
}

func newSpyEngine() *spyEngine { return &spyEngine{Engine: offload.NewNullEngine()} }

func (e *spyEngine) Upsert(client, peer offload.Connection, listener, cluster string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.upserts = append(e.upserts, offloadCall{client, peer, listener, cluster})
	return nil
}

func (e *spyEngine) Remove(client, peer offload.Connection) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.removes = append(e.removes, offloadCall{client: client, peer: peer})
	return nil
}

func (e *spyEngine) snapshot() (upserts, removes []offloadCall) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]offloadCall{}, e.upserts...), append([]offloadCall{}, e.removes...)
}

// routeListener is a listener node carrying only the routes the Router reads. The real Listener
// object opens sockets, which this test has no use for.
type routeListener struct{ conf stnrv1.ListenerConfig }

func (l *routeListener) Name() string             { return l.conf.Name }
func (l *routeListener) Type() runtime.ObjectType { return runtime.TypeListener }
func (l *routeListener) Start() error             { return nil }
func (l *routeListener) Close(_ bool) error       { return nil }
func (l *routeListener) GetConfig() stnrv1.Config { return &l.conf }
func (l *routeListener) Status() stnrv1.Status    { return nil }
func (l *routeListener) Inspect(_, _ stnrv1.Config, _ *stnrv1.StunnerConfig) (runtime.Action, error) {
	return runtime.ActionNone, nil
}
func (l *routeListener) Reconcile(_ stnrv1.Config) error { return nil }

// offloadTestPeer is the peer every offload case routes to.
var offloadTestPeer = &net.UDPAddr{IP: net.ParseIP("9.9.9.9"), Port: 2222}

// newOffloadEnv wires a listener named "li" routing to a single cluster of the given protocol,
// admitting offloadTestPeer, over the real router, with a counting offload engine.
func newOffloadEnv(t *testing.T, cluster stnrv1.ClusterProtocol) (*runtime.Runtime, *spyEngine) {
	t.Helper()
	env := newTestEnv()
	eng := newSpyEngine()
	env.rt.OffloadEngine = eng

	cconf := &stnrv1.ClusterConfig{Name: "cl", Protocol: cluster.String()}
	if cluster.IsTURN() {
		cconf.TURNServer = &stnrv1.TURNServer{Address: "1.2.3.4", Port: 3478}
	} else {
		cconf.Endpoints = []string{offloadTestPeer.IP.String()}
	}
	c, err := object.NewCluster(cconf, env.rt)
	require.NoError(t, err)
	mustAdd(t, env, c)
	mustAdd(t, env, &routeListener{conf: stnrv1.ListenerConfig{Name: "li", Routes: []string{"cl"}}})

	return env.rt, eng
}

// TestOffloadMatrix is the single place the offload matrix is stated. The engines accelerate
// exactly one leg shape, plaintext ChannelData on one side and raw datagrams on the other, which
// leaves two mirror-image protocol pairs: a turn-udp listener relaying to a udp cluster, and a udp
// listener tunnelling to a turn-udp cluster. Nothing else offloads, in either direction.
//
// Each case asserts the rule itself, and then, for the listener protocols the TURN server engine
// serves, that the channel handlers actually obey it: an offload that is merely useless still
// costs a map entry, one installed over encrypted ChannelData would mangle the traffic, and a
// removal that was never registered makes engines log errors. Both handlers run for every case,
// because a skipped registration has to be matched by a skipped removal.
//
// TestOffloadEndToEnd below drives the plain-listener half over a live TURN server and real
// traffic, which is the only way to reach the L4 flow engine's registration path.
func TestOffloadMatrix(t *testing.T) {
	client := &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 1111}
	listener := &net.UDPAddr{IP: net.ParseIP("5.6.7.8"), Port: 3478}
	relayAddr := &net.UDPAddr{IP: net.ParseIP("5.6.7.8"), Port: 50000}

	listeners := []stnrv1.ListenerProtocol{
		stnrv1.ListenerProtocolUDP, stnrv1.ListenerProtocolTCP, stnrv1.ProtocolSTDIN,
		stnrv1.ListenerProtocolTURNUDP, stnrv1.ListenerProtocolTURNTCP,
		stnrv1.ListenerProtocolTURNTLS, stnrv1.ListenerProtocolTURNDTLS,
	}
	clusters := []stnrv1.ClusterProtocol{
		stnrv1.ClusterProtocolUDP, stnrv1.ClusterProtocolTCP,
		stnrv1.ClusterProtocolTURNUDP, stnrv1.ClusterProtocolTURNTCP,
		stnrv1.ClusterProtocolTURNTLS, stnrv1.ClusterProtocolTURNDTLS,
	}

	for _, lp := range listeners {
		for _, cp := range clusters {
			// the two accelerated shapes, and only those
			want := (lp == stnrv1.ListenerProtocolTURNUDP && cp == stnrv1.ClusterProtocolUDP) ||
				(lp == stnrv1.ListenerProtocolUDP && cp == stnrv1.ClusterProtocolTURNUDP)

			t.Run(lp.String()+"-listener/"+cp.String()+"-cluster", func(t *testing.T) {
				assert.Equal(t, want, offload.Offloadable(lp, cp), "offload rule")

				if !lp.IsTURN() {
					return // a plain listener is served by the L4 flow engine
				}

				rt, eng := newOffloadEnv(t, cp)
				h := objectturn.NewEventHandler("li", lp, rt,
					logging.NewDefaultLoggerFactory().NewLogger("test"),
					objectturn.NewQuotaHandler(rt))

				h.OnChannelCreated(client, listener, "UDP", "user", "realm", relayAddr,
					offloadTestPeer, 0x4000)
				h.OnChannelDeleted(client, listener, "UDP", "user", "realm", relayAddr,
					offloadTestPeer, 0x4000)

				upserts, removes := eng.snapshot()
				if want {
					assert.Len(t, upserts, 1, "offload registered")
					assert.Len(t, removes, 1, "offload removed")
					return
				}
				assert.Empty(t, upserts, "no offload registered")
				assert.Empty(t, removes, "no offload removal attempted")
			})
		}
	}
}

// TestOffloadChannelPeerShape covers the two channel cases the protocol matrix cannot express: a
// peer that is not a datagram endpoint, and a peer no cluster routes any more.
func TestOffloadChannelPeerShape(t *testing.T) {
	client := &net.UDPAddr{IP: net.ParseIP("1.2.3.4"), Port: 1111}
	listener := &net.UDPAddr{IP: net.ParseIP("5.6.7.8"), Port: 3478}
	relayAddr := &net.UDPAddr{IP: net.ParseIP("5.6.7.8"), Port: 50000}

	for _, c := range []struct {
		name string
		peer net.Addr
	}{
		// an RFC 6062 relayed TCP connection never carries a channel
		{name: "stream peer", peer: &net.TCPAddr{IP: offloadTestPeer.IP, Port: 2222}},
		// routing changed under a live channel: with no cluster behind the peer there is no
		// leg left to accelerate
		{name: "unrouted peer", peer: &net.UDPAddr{IP: net.ParseIP("8.8.8.8"), Port: 2222}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rt, eng := newOffloadEnv(t, stnrv1.ClusterProtocolUDP)
			h := objectturn.NewEventHandler("li", stnrv1.ListenerProtocolTURNUDP, rt,
				logging.NewDefaultLoggerFactory().NewLogger("test"),
				objectturn.NewQuotaHandler(rt))

			h.OnChannelCreated(client, listener, "UDP", "user", "realm", relayAddr, c.peer, 0x4000)
			h.OnChannelDeleted(client, listener, "UDP", "user", "realm", relayAddr, c.peer, 0x4000)

			upserts, removes := eng.snapshot()
			assert.Empty(t, upserts, "no offload registered")
			assert.Empty(t, removes, "no offload removal attempted")
		})
	}
}

// turnUpstream starts an in-process TURN server on the loopback and returns the address a turn-*
// cluster should name: the upstream leg of a tunnel-mode flow.
func turnUpstream(t *testing.T) *net.UDPAddr {
	t.Helper()
	serverConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err, "server socket")
	srv, err := turn.NewServer(turn.ServerConfig{
		Realm: "test",
		AuthHandler: func(*turn.RequestAttributes) (string, []byte, bool) {
			return "user", turn.GenerateAuthKey("user", "test", "pass"), true
		},
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: serverConn,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP("127.0.0.1"), Address: "127.0.0.1"},
		}},
	})
	require.NoError(t, err, "TURN server")
	t.Cleanup(func() { _ = srv.Close() })
	return serverConn.LocalAddr().(*net.UDPAddr)
}

// udpEcho starts a loopback echo server: the peer a flow relays to.
func udpEcho(t *testing.T) *net.UDPAddr {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := c.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = c.WriteTo(buf[:n], addr)
		}
	}()
	return c.LocalAddr().(*net.UDPAddr)
}

// freePort reserves a kernel-allocated port of the given network and releases it for reuse.
func freePort(t *testing.T, network string) int {
	t.Helper()
	if network == "udp" {
		c, err := net.ListenPacket("udp", "127.0.0.1:0")
		require.NoError(t, err)
		defer func() { _ = c.Close() }()
		return c.LocalAddr().(*net.UDPAddr).Port
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// newDataplaneEnv brings up a live plain listener routing to one cluster, with a recording offload
// engine. Unlike newOffloadEnv it opens real sockets, which is what the L4 flow engine needs.
func newDataplaneEnv(t *testing.T, lconf *stnrv1.ListenerConfig, cconf *stnrv1.ClusterConfig,
	eng offload.Engine) *l4.Server {
	t.Helper()

	log := logger.NewLoggerFactory("all:ERROR")
	tm, err := telemetry.New(telemetry.Callbacks{}, true, log.NewLogger("telemetry"))
	require.NoError(t, err, "telemetry")
	t.Cleanup(func() { _ = tm.Close() })
	n, err := stdnet.NewNet()
	require.NoError(t, err, "stdnet")

	rt := runtime.New(runtime.Config{
		Logger:        log,
		DryRun:        true,
		Resolver:      resolver.NewMockResolver(map[string][]string{}, log),
		Telemetry:     tm,
		OffloadEngine: eng,
		Net:           n,
	})
	rt.Router = router.NewRouter(rt)
	rt.QuotaHandler = quota.New(rt)

	cluster, err := object.NewCluster(cconf, rt)
	require.NoError(t, err, "cluster")
	require.NoError(t, rt.Registry.Add(cluster, nil), "register cluster")
	listener, err := object.NewListener(lconf, rt)
	require.NoError(t, err, "listener")
	require.NoError(t, rt.Registry.Add(listener, nil), "register listener")

	s, err := l4.NewServer(lconf.Name, rt)
	require.NoError(t, err, "flow engine")
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// plainListenerConf is a plain listener pinned to one peer.
func plainListenerConf(name, proto, peer string, port int) *stnrv1.ListenerConfig {
	return &stnrv1.ListenerConfig{Name: name, Protocol: proto, Addr: "127.0.0.1",
		Port: port, PeerAddr: peer, Routes: []string{"cluster"}}
}

// turnClusterConf names an upstream TURN server as a turn-udp cluster.
func turnClusterConf(server *net.UDPAddr) *stnrv1.ClusterConfig {
	return &stnrv1.ClusterConfig{
		Name:     "cluster",
		Protocol: "turn-udp",
		TURNServer: &stnrv1.TURNServer{
			Address: "127.0.0.1",
			Port:    server.Port,
			Auth: &stnrv1.AuthConfig{Type: "static", Credentials: map[string]string{
				"username": "user", "password": "pass"}},
		},
	}
}

// TestOffloadEndToEnd drives the plain-listener half of the matrix through the real L4 flow
// engine, over a live upstream TURN server and real traffic. It is the only way to reach that
// registration path: the flow engine registers a pair not when the flow is set up but when the
// upstream channel binding goes live, which no amount of stubbing produces.
//
// The three cases are the ones that can reach the channel probe at all: the one accelerated
// shape, a stream client tunnelling through the same cluster, and a direct relay.
func TestOffloadEndToEnd(t *testing.T) {
	t.Run("udp client tunnelling to a turn-udp cluster is offloaded", func(t *testing.T) {
		serverAddr := turnUpstream(t)
		eng := newSpyEngine()
		peer := udpEcho(t)
		port := freePort(t, "udp")
		lconf := plainListenerConf("plain-udp-tunnel", "UDP", peer.String(), port)
		s := newDataplaneEnv(t, lconf, turnClusterConf(serverAddr), eng)

		client, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		require.NoError(t, err, "client dial")
		defer func() { _ = client.Close() }()

		// echo through the tunnel chain, retrying while the upstream permission settles
		buf := make([]byte, 2048)
		echoed := false
		for i := 0; i < 20 && !echoed; i++ {
			_, err = client.Write([]byte("hello"))
			require.NoError(t, err, "client write")
			require.NoError(t, client.SetReadDeadline(time.Now().Add(250*time.Millisecond)))
			if _, err := client.Read(buf); err == nil {
				echoed = true
			}
		}
		require.True(t, echoed, "echo through the tunnel chain")

		assert.Eventually(t, func() bool {
			upserts, _ := eng.snapshot()
			return len(upserts) == 1
		}, 5*time.Second, 50*time.Millisecond, "upsert after the channel is bound")

		upserts, _ := eng.snapshot()
		up := upserts[0]

		// the wire takes the engine's channel-bearing position: ChannelData towards the
		// upstream server over the transport socket, with the peer implicit in the channel
		assert.Equal(t, serverAddr.String(), up.client.RemoteAddr.String(), "wire remote is the server")
		assert.NotEqual(t, peer.String(), up.client.RemoteAddr.String(), "peer rides the channel, not the wire")
		assert.Equal(t, offload.ProtocolUDP, up.client.Protocol, "wire transport")
		assert.False(t, strings.HasSuffix(up.client.LocalAddr.String(), ":"+strconv.Itoa(port)),
			"transport socket, not the listener socket")
		assert.Equal(t, uint32(0x4000), up.client.ChannelID,
			"the single binding of a per-flow session sits at the channel floor")

		// the raw side faces our client at the listener socket, with no channel
		assert.Equal(t, client.LocalAddr().String(), up.peer.RemoteAddr.String(), "client source")
		assert.True(t, strings.HasSuffix(up.peer.LocalAddr.String(), ":"+strconv.Itoa(port)),
			"listener socket")
		assert.Equal(t, offload.ProtocolUDP, up.peer.Protocol, "client transport")
		assert.Zero(t, up.peer.ChannelID, "no channel on the raw side")

		assert.Equal(t, "plain-udp-tunnel", up.listener, "listener attribution")
		assert.Equal(t, "cluster", up.cluster, "cluster attribution")

		require.NoError(t, s.Close())
		assert.Eventually(t, func() bool {
			_, removes := eng.snapshot()
			return len(removes) == 1
		}, 2*time.Second, 10*time.Millisecond, "offload removed on teardown")
		_, removes := eng.snapshot()
		assert.Equal(t, up.client, removes[0].client, "removal names the same wire connection")
		assert.Equal(t, up.peer, removes[0].peer, "removal names the same client connection")
	})

	t.Run("tcp client tunnelling to a turn-udp cluster is not", func(t *testing.T) {
		// this is where the rule is load-bearing rather than redundant: the flow does bind an
		// upstream channel, so the probe reports live framing and would register a pair, but
		// the client leg carries a stream and has no datagram 4-tuple to match
		serverAddr := turnUpstream(t)
		eng := newSpyEngine()
		peer := udpEcho(t)
		port := freePort(t, "tcp")
		lconf := plainListenerConf("plain-tcp-tunnel", "TCP", peer.String(), port)
		s := newDataplaneEnv(t, lconf, turnClusterConf(serverAddr), eng)

		client, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		require.NoError(t, err, "client dial")
		defer func() { _ = client.Close() }()

		_, err = client.Write([]byte("hello"))
		require.NoError(t, err, "client write")
		assert.Eventually(t, func() bool { return s.AllocationCount() == 1 }, 2*time.Second,
			20*time.Millisecond, "flow up")

		// the channel probe runs on a backoff for about three seconds; nothing may register
		assert.Never(t, func() bool {
			upserts, _ := eng.snapshot()
			return len(upserts) > 0
		}, 1500*time.Millisecond, 100*time.Millisecond, "stream client is not offloaded")

		require.NoError(t, s.Close())
		_, removes := eng.snapshot()
		assert.Empty(t, removes, "no offload removal attempted")
	})

	t.Run("direct relay is not", func(t *testing.T) {
		eng := newSpyEngine()
		peer := udpEcho(t)
		port := freePort(t, "udp")
		lconf := plainListenerConf("plain-udp-direct", "UDP", peer.String(), port)
		cconf := &stnrv1.ClusterConfig{Name: "cluster", Type: "STATIC", Protocol: "udp",
			Endpoints: []string{"127.0.0.1"}}
		s := newDataplaneEnv(t, lconf, cconf, eng)

		client, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		require.NoError(t, err, "client dial")
		defer func() { _ = client.Close() }()

		_, err = client.Write([]byte("hello"))
		require.NoError(t, err, "client write")
		require.NoError(t, client.SetReadDeadline(time.Now().Add(2*time.Second)))
		buf := make([]byte, 2048)
		_, err = client.Read(buf)
		require.NoError(t, err, "echo")

		// raw on both sides: there is no channel to strip, so nothing is ever registered
		assert.Never(t, func() bool {
			upserts, _ := eng.snapshot()
			return len(upserts) > 0
		}, 500*time.Millisecond, 50*time.Millisecond, "direct flow is not offloaded")

		require.NoError(t, s.Close())
		_, removes := eng.snapshot()
		assert.Empty(t, removes, "no offload removal attempted")
	})
}
