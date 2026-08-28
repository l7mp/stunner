package l4_test

import (
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/transport/v4/stdnet"
	"github.com/pion/transport/v4/test"
	"github.com/pion/turn/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/object/l4"
	"github.com/l7mp/stunner/v2/internal/offload"
	quotapkg "github.com/l7mp/stunner/v2/internal/quota"
	"github.com/l7mp/stunner/v2/internal/resolver"
	"github.com/l7mp/stunner/v2/internal/router"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

// spyQuota records quota accounting calls; deny rejects every new flow.
type spyQuota struct {
	mu         sync.Mutex
	deny       bool
	incs, decs []string
}

func (q *spyQuota) CheckAndIncrement(user, realm string, _ int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.deny {
		return false
	}
	q.incs = append(q.incs, user+"@"+realm)
	return true
}

func (q *spyQuota) Decrement(user, realm string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.decs = append(q.decs, user+"@"+realm)
}

func (q *spyQuota) snapshot() (incs, decs []string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string{}, q.incs...), append([]string{}, q.decs...)
}

// spyOffload records offload registrations over the null engine.
type spyOffload struct {
	offload.Engine
	mu               sync.Mutex
	upserts, removes []offloadCall
}

type offloadCall struct {
	client, peer      offload.Connection
	listener, cluster string
}

func newSpyOffload() *spyOffload { return &spyOffload{Engine: offload.NewNullEngine()} }

func (e *spyOffload) Upsert(client, peer offload.Connection, listener, cluster string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.upserts = append(e.upserts, offloadCall{client, peer, listener, cluster})
	return nil
}

func (e *spyOffload) Remove(client, peer offload.Connection) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.removes = append(e.removes, offloadCall{client: client, peer: peer})
	return nil
}

func (e *spyOffload) snapshot() (upserts, removes []offloadCall) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]offloadCall{}, e.upserts...), append([]offloadCall{}, e.removes...)
}

// newTestRuntime builds a runtime with one plain listener and one cluster registered.
func newTestRuntime(t *testing.T, lconf *stnrv1.ListenerConfig, cconf *stnrv1.ClusterConfig,
	quota objruntime.QuotaHandler, eng offload.Engine) *objruntime.Runtime {
	t.Helper()

	log := logger.NewLoggerFactory("all:ERROR")
	tm, err := telemetry.New(telemetry.Callbacks{}, true, log.NewLogger("telemetry"))
	require.NoError(t, err, "telemetry")
	t.Cleanup(func() { _ = tm.Close() })
	n, err := stdnet.NewNet()
	require.NoError(t, err, "stdnet")
	rt := objruntime.New(objruntime.Config{
		Logger:        log,
		DryRun:        true,
		Resolver:      resolver.NewMockResolver(map[string][]string{}, log),
		Telemetry:     tm,
		QuotaHandler:  quota,
		OffloadEngine: eng,
		Net:           n,
	})
	rt.Router = router.NewRouter(rt)
	if rt.QuotaHandler == nil {
		rt.QuotaHandler = quotapkg.New(rt)
	}

	cluster, err := object.NewCluster(cconf, rt)
	require.NoError(t, err, "cluster")
	require.NoError(t, rt.Registry.Add(cluster, nil), "register cluster")

	listener, err := object.NewListener(lconf, rt)
	require.NoError(t, err, "listener")
	require.NoError(t, rt.Registry.Add(listener, nil), "register listener")

	return rt
}

// newTestServer starts the flow engine over a fresh test runtime.
func newTestServer(t *testing.T, lconf *stnrv1.ListenerConfig, cconf *stnrv1.ClusterConfig) *l4.Server {
	t.Helper()
	return startServer(t, newTestRuntime(t, lconf, cconf, nil, nil), lconf.Name)
}

func startServer(t *testing.T, rt *objruntime.Runtime, name string) *l4.Server {
	t.Helper()
	s, err := l4.NewServer(name, rt)
	require.NoError(t, err, "flow engine")
	t.Cleanup(func() { _ = s.Close() })
	return s
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

// udpEcho starts a UDP echo peer and returns its address.
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

func udpListenerConf(name, peer string, port int) *stnrv1.ListenerConfig {
	return &stnrv1.ListenerConfig{Name: name, Protocol: "UDP", Addr: "127.0.0.1",
		Port: port, PeerAddr: peer, Routes: []string{"cluster"}}
}

func udpClusterConf(endpoints ...string) *stnrv1.ClusterConfig {
	return &stnrv1.ClusterConfig{Name: "cluster", Type: "STATIC", Protocol: "udp",
		Endpoints: endpoints}
}

// TestFlowUDPRelay covers the datagram round trip of a plain UDP listener: client datagrams
// reach the pinned peer and the peer's responses come back on the same flow.
func TestFlowUDPRelay(t *testing.T) {
	peer := udpEcho(t)
	port := freePort(t, "udp")
	s := newTestServer(t, udpListenerConf("plain-udp", peer.String(), port),
		udpClusterConf("127.0.0.1"))

	client, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err, "client dial")
	defer func() { _ = client.Close() }()

	_, err = client.Write([]byte("hello"))
	require.NoError(t, err, "client write")

	require.NoError(t, client.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, 2048)
	n, err := client.Read(buf)
	require.NoError(t, err, "echo")
	assert.Equal(t, "hello", string(buf[:n]), "echo payload")
	assert.Equal(t, 1, s.AllocationCount(), "flow count")

	require.NoError(t, s.Close())
	assert.Eventually(t, func() bool { return s.AllocationCount() == 0 }, time.Second,
		10*time.Millisecond, "flows torn down on close")
}

// TestFlowTCPClientRelay covers the stream-to-datagram adaptation: a TCP client flow is framed
// into datagrams towards a UDP peer, chunk per read.
func TestFlowTCPClientRelay(t *testing.T) {
	peer := udpEcho(t)
	port := freePort(t, "tcp")
	lconf := &stnrv1.ListenerConfig{Name: "plain-tcp", Protocol: "TCP", Addr: "127.0.0.1",
		Port: port, PeerAddr: peer.String(), Routes: []string{"cluster"}}
	s := newTestServer(t, lconf, udpClusterConf("127.0.0.1"))

	client, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err, "client dial")

	_, err = client.Write([]byte("hello"))
	require.NoError(t, err, "client write")

	require.NoError(t, client.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, 2048)
	n, err := client.Read(buf)
	require.NoError(t, err, "echo")
	assert.Equal(t, "hello", string(buf[:n]), "echo payload")
	assert.Equal(t, 1, s.AllocationCount(), "flow count")

	// a client FIN tears the flow down
	require.NoError(t, client.Close())
	assert.Eventually(t, func() bool { return s.AllocationCount() == 0 }, time.Second,
		10*time.Millisecond, "flow torn down on client close")
}

// TestFlowTCPPeerRelay covers the stream relay leg: a listener routing to a TCP cluster dials
// the peer over TCP.
func TestFlowTCPPeerRelay(t *testing.T) {
	pl, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "peer listener")
	t.Cleanup(func() { _ = pl.Close() })
	go func() {
		for {
			c, err := pl.Accept()
			if err != nil {
				return
			}
			go func() {
				buf := make([]byte, 2048)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					_, _ = c.Write(buf[:n])
				}
			}()
		}
	}()

	port := freePort(t, "udp")
	lconf := udpListenerConf("plain-udp-tcp-peer", "tcp://"+pl.Addr().String(), port)
	cconf := &stnrv1.ClusterConfig{Name: "cluster", Type: "STATIC", Protocol: "tcp",
		Endpoints: []string{"127.0.0.1"}}
	s := newTestServer(t, lconf, cconf)

	client, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err, "client dial")
	defer func() { _ = client.Close() }()

	_, err = client.Write([]byte("hello"))
	require.NoError(t, err, "client write")

	require.NoError(t, client.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, 2048)
	n, err := client.Read(buf)
	require.NoError(t, err, "echo")
	assert.Equal(t, "hello", string(buf[:n]), "echo payload")
	assert.Equal(t, 1, s.AllocationCount(), "flow count")
}

// TestFlowAdmission verifies that a peer no routed cluster admits fails the flow early:
// nothing is relayed and no flow is registered.
func TestFlowAdmission(t *testing.T) {
	peer := udpEcho(t)
	port := freePort(t, "udp")
	s := newTestServer(t, udpListenerConf("plain-udp-unadmitted", peer.String(), port),
		udpClusterConf("9.9.9.9"))

	client, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err, "client dial")
	defer func() { _ = client.Close() }()

	_, err = client.Write([]byte("hello"))
	require.NoError(t, err, "client write")

	require.NoError(t, client.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	buf := make([]byte, 2048)
	_, err = client.Read(buf)
	assert.Error(t, err, "no echo from an unadmitted peer")
	assert.Equal(t, 0, s.AllocationCount(), "no flow registered")
}

// TestFlowPreflightProtocolAgnostic pins the admission pre-flight semantics: it follows the
// TURN permission-handler rule, so a cluster of any protocol admitting the peer IP passes the
// pre-flight, and the protocol-aware verdict falls on the relay leg (here: the per-datagram
// admission wrapper of the direct UDP leg refuses the write, tearing the flow down).
func TestFlowPreflightProtocolAgnostic(t *testing.T) {
	peer := udpEcho(t)
	port := freePort(t, "udp")
	// UDP peer, but the only routed cluster is a TCP protocol cluster
	lconf := udpListenerConf("plain-udp-proto-mismatch", peer.String(), port)
	cconf := &stnrv1.ClusterConfig{Name: "cluster", Type: "STATIC", Protocol: "tcp",
		Endpoints: []string{"127.0.0.1"}}
	s := newTestServer(t, lconf, cconf)

	client, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err, "client dial")
	defer func() { _ = client.Close() }()

	_, err = client.Write([]byte("hello"))
	require.NoError(t, err, "client write")

	require.NoError(t, client.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	buf := make([]byte, 2048)
	_, err = client.Read(buf)
	assert.Error(t, err, "no echo: the leg refuses the unadmitted transport")
	assert.Eventually(t, func() bool { return s.AllocationCount() == 0 }, time.Second,
		10*time.Millisecond, "flow torn down on the refused write")
}

// TestFlowIdleExpiry verifies the activity-driven idle teardown and that fresh traffic from the
// same client re-creates the flow.
func TestFlowIdleExpiry(t *testing.T) {
	// the flow timeout is a system constant; shorten it for the test
	defer func(d time.Duration) { l4.FlowTimeout = d }(l4.FlowTimeout)
	l4.FlowTimeout = 100 * time.Millisecond

	peer := udpEcho(t)
	port := freePort(t, "udp")
	s := newTestServer(t, udpListenerConf("plain-udp-idle", peer.String(), port),
		udpClusterConf("127.0.0.1"))

	client, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err, "client dial")
	defer func() { _ = client.Close() }()

	buf := make([]byte, 2048)
	echo := func() {
		_, err := client.Write([]byte("hello"))
		require.NoError(t, err, "client write")
		require.NoError(t, client.SetReadDeadline(time.Now().Add(2*time.Second)))
		_, err = client.Read(buf)
		require.NoError(t, err, "echo")
	}

	echo()
	assert.Equal(t, 1, s.AllocationCount(), "flow created")

	// keep the flow busy over several timer periods: activity re-arms the idle timer
	for i := 0; i < 5; i++ {
		time.Sleep(50 * time.Millisecond)
		echo()
	}
	assert.Equal(t, 1, s.AllocationCount(), "active flow survives the idle timer")

	// quiet flow expires
	assert.Eventually(t, func() bool { return s.AllocationCount() == 0 }, time.Second,
		10*time.Millisecond, "idle flow torn down")

	// fresh traffic re-creates the flow
	echo()
	assert.Equal(t, 1, s.AllocationCount(), "flow re-created on traffic")
}

// TestFlowGoroutineLeak verifies that flow and engine teardown release the pump goroutines.
// The routine check runs as the outermost cleanup, after the harness teardown.
func TestFlowGoroutineLeak(t *testing.T) {
	t.Cleanup(test.CheckRoutines(t))

	peer := udpEcho(t)
	port := freePort(t, "udp")
	s := newTestServer(t, udpListenerConf("plain-udp-leak", peer.String(), port),
		udpClusterConf("127.0.0.1"))

	for i := 0; i < 5; i++ {
		client, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		require.NoError(t, err, "client dial")
		_, err = client.Write([]byte("hello"))
		require.NoError(t, err, "client write")
		require.NoError(t, client.SetReadDeadline(time.Now().Add(2*time.Second)))
		buf := make([]byte, 2048)
		_, err = client.Read(buf)
		require.NoError(t, err, "echo")
		require.NoError(t, client.Close())
	}

	require.NoError(t, s.Close())
}

// TestFlowEvents covers the default event wiring: a flow increments the per-client-IP quota
// accounting, and teardown releases it. A direct flow is never offloaded, whatever the engine:
// the datapath only accelerates a TURN client/server leg, so a channel-less pair has no
// meaning there.
func TestFlowEvents(t *testing.T) {
	quota := &spyQuota{}
	eng := newSpyOffload()
	peer := udpEcho(t)
	port := freePort(t, "udp")
	lconf := udpListenerConf("plain-udp-events", peer.String(), port)
	rt := newTestRuntime(t, lconf, udpClusterConf("127.0.0.1"), quota, eng)
	s := startServer(t, rt, lconf.Name)

	client, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err, "client dial")
	defer func() { _ = client.Close() }()

	_, err = client.Write([]byte("hello"))
	require.NoError(t, err, "client write")
	require.NoError(t, client.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, 2048)
	_, err = client.Read(buf)
	require.NoError(t, err, "echo")

	incs, decs := quota.snapshot()
	require.Len(t, incs, 1, "quota incremented")
	assert.Equal(t, "127.0.0.1@"+stnrv1.DefaultRealm, incs[0], "client IP is the principal")
	assert.Empty(t, decs, "no decrement while the flow lives")

	upserts, removes := eng.snapshot()
	assert.Empty(t, upserts, "a direct flow is not offloadable")
	assert.Empty(t, removes, "nothing to remove")

	require.NoError(t, s.Close())
	assert.Eventually(t, func() bool {
		_, decs := quota.snapshot()
		return len(decs) == 1
	}, 2*time.Second, 10*time.Millisecond, "teardown releases the quota")
	_, removes = eng.snapshot()
	assert.Empty(t, removes, "nothing was offloaded, so nothing is removed")
	_, decs = quota.snapshot()
	assert.Equal(t, incs, decs, "decrement carries the minted identity")
}

// TestFlowQuotaRejected verifies the quota gate: a denied client gets no flow and no relay.
func TestFlowQuotaRejected(t *testing.T) {
	quota := &spyQuota{deny: true}
	peer := udpEcho(t)
	port := freePort(t, "udp")
	lconf := udpListenerConf("plain-udp-quota", peer.String(), port)
	rt := newTestRuntime(t, lconf, udpClusterConf("127.0.0.1"), quota, nil)
	s := startServer(t, rt, lconf.Name)

	client, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	require.NoError(t, err, "client dial")
	defer func() { _ = client.Close() }()

	_, err = client.Write([]byte("hello"))
	require.NoError(t, err, "client write")
	require.NoError(t, client.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	buf := make([]byte, 2048)
	_, err = client.Read(buf)
	assert.Error(t, err, "no echo for a quota-rejected client")
	assert.Equal(t, 0, s.AllocationCount(), "no flow registered")

	incs, decs := quota.snapshot()
	assert.Empty(t, incs, "nothing counted")
	assert.Empty(t, decs, "nothing released")
}

// TestTunnelFlowOffload drives a real tunnel-mode flow: the listener routes to a TURN cluster
// backed by a local pion server, so the relay leg is a real upstream allocation. No offload
// registration happens until the upstream channel binding goes live; then the upsert names the
// wire 5-tuple with the channel on the upstream side only, and teardown removes the same pair.
// The registered pair is the contract of the client-side kernel offload: ingress strips nothing
// and egress encapsulates into the channel.
func TestTunnelFlowOffload(t *testing.T) {
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
	defer srv.Close() //nolint:errcheck
	serverAddr := serverConn.LocalAddr().(*net.UDPAddr)

	eng := newSpyOffload()
	peer := udpEcho(t)
	port := freePort(t, "udp")
	lconf := udpListenerConf("plain-udp-tunnel", peer.String(), port)
	cconf := &stnrv1.ClusterConfig{
		Name:     "cluster",
		Protocol: "turn-udp",
		TURNServer: &stnrv1.TURNServer{
			Address: "127.0.0.1",
			Port:    serverAddr.Port,
			Auth: &stnrv1.AuthConfig{Type: "static", Credentials: map[string]string{
				"username": "user", "password": "pass"}},
		},
	}
	rt := newTestRuntime(t, lconf, cconf, nil, eng)
	s := startServer(t, rt, lconf.Name)

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

	// the channel-bound event upserts the wire 5-tuple with the client-bound channel
	assert.Eventually(t, func() bool {
		upserts, _ := eng.snapshot()
		return len(upserts) == 1
	}, 5*time.Second, 50*time.Millisecond, "upsert after the channel is bound")

	upserts, _ := eng.snapshot()
	up := upserts[0]

	// the wire takes the engine's channel-bearing position: ChannelData towards the upstream
	// server over the transport socket, with the peer implicit in the channel
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
}
