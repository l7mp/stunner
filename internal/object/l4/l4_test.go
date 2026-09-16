package l4_test

import (
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/pion/transport/v5/stdnet"
	"github.com/pion/transport/v5/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/object/l4"
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

// newTestRuntime builds a runtime with one plain listener and one cluster registered.
func newTestRuntime(t *testing.T, lconf *stnrv1.ListenerConfig, cconf *stnrv1.ClusterConfig,
	quota objruntime.QuotaHandler) *objruntime.Runtime {
	t.Helper()

	log := logger.NewLoggerFactory("all:ERROR")
	tm, err := telemetry.New(telemetry.Callbacks{}, true, log.NewLogger("telemetry"))
	require.NoError(t, err, "telemetry")
	t.Cleanup(func() { _ = tm.Close() })
	n, err := stdnet.NewNet()
	require.NoError(t, err, "stdnet")
	rt := objruntime.New(objruntime.Config{
		Logger:       log,
		DryRun:       true,
		Resolver:     resolver.NewMockResolver(map[string][]string{}, log),
		Telemetry:    tm,
		QuotaHandler: quota,
		Net:          n,
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
	return startServer(t, newTestRuntime(t, lconf, cconf, nil), lconf.Name)
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
// accounting, and teardown releases it.
func TestFlowEvents(t *testing.T) {
	quota := &spyQuota{}
	peer := udpEcho(t)
	port := freePort(t, "udp")
	lconf := udpListenerConf("plain-udp-events", peer.String(), port)
	rt := newTestRuntime(t, lconf, udpClusterConf("127.0.0.1"), quota)
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

	require.NoError(t, s.Close())
	assert.Eventually(t, func() bool {
		_, decs := quota.snapshot()
		return len(decs) == 1
	}, 2*time.Second, 10*time.Millisecond, "teardown releases the quota")
	_, decs = quota.snapshot()
	assert.Equal(t, incs, decs, "decrement carries the minted identity")
}

// TestFlowQuotaRejected verifies the quota gate: a denied client gets no flow and no relay.
func TestFlowQuotaRejected(t *testing.T) {
	quota := &spyQuota{deny: true}
	peer := udpEcho(t)
	port := freePort(t, "udp")
	lconf := udpListenerConf("plain-udp-quota", peer.String(), port)
	rt := newTestRuntime(t, lconf, udpClusterConf("127.0.0.1"), quota)
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
