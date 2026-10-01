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

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/listener"
	"github.com/l7mp/stunner/v2/internal/object"
	quotapkg "github.com/l7mp/stunner/v2/internal/quota"
	"github.com/l7mp/stunner/v2/internal/resolver"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server/l4"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
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

// setup describes a test L4 server: its listener protocol, the protocol and the endpoints of the
// cluster it names (no protocol: the cluster does not exist), and an optional quota handler.
type setup struct {
	listener  string
	proto     string
	endpoints []string
	quota     objruntime.QuotaHandler
}

type testServer struct {
	*object.Server
	cluster *object.Cluster
	port    int
}

// newTestServer starts an L4 server behind one listener on loopback.
func newTestServer(t *testing.T, c setup) *testServer {
	t.Helper()
	log := logger.NewLoggerFactory("all:ERROR")
	tm, err := telemetry.New(telemetry.Callbacks{}, true, log.NewLogger("telemetry"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tm.Close() })
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	rt := objruntime.New(objruntime.Config{Logger: log, Telemetry: tm, Net: nw,
		QuotaHandler: c.quota, Resolver: resolver.NewMockResolver(map[string][]string{}, log)})
	if rt.QuotaHandler == nil {
		rt.QuotaHandler = quotapkg.New(rt)
	}

	// the server and its cluster, found by name through the registry
	start := func(o objruntime.Object) {
		require.NoError(t, rt.Registry.Add(o, nil))
		require.NoError(t, o.Start())
		t.Cleanup(func() { _ = o.Close(true) })
	}
	ts := &testServer{}
	if c.proto != "" {
		cl, err := object.NewCluster(&stnrv2.ClusterConfig{Name: "peers", Endpoints: c.endpoints,
			Protocol: c.proto, RoutingPolicy: stnrv2.RoutingPolicyRoundRobin.String()}, rt)
		require.NoError(t, err)
		start(cl)
		ts.cluster = cl.(*object.Cluster)
	}
	srv, err := object.NewServer(&stnrv2.ServerConfig{Name: "l4", Type: "l4",
		Clusters: []string{"peers"}}, rt)
	require.NoError(t, err)
	start(srv)
	s := srv.(*object.Server)

	port := freePort(t, c.listener)
	conf := &stnrv2.ListenerConfig{Name: "plain", Protocol: c.listener, Addr: "127.0.0.1",
		Port: port, Servers: []string{"l4"}}
	require.NoError(t, conf.Validate())
	l, err := listener.New(conf, rt, func(conn api.Conn) {
		if _, _, err := s.Serve(conn); err != nil {
			_ = conn.Close()
		}
	})
	require.NoError(t, err)
	require.NoError(t, l.Start())
	t.Cleanup(func() { _ = l.Close() })
	ts.Server, ts.port = s, port
	return ts
}

// setEndpoints reconciles the cluster with new endpoints.
func (s *testServer) setEndpoints(t *testing.T, endpoints []string) {
	t.Helper()
	require.NoError(t, s.cluster.Reconcile(&stnrv2.ClusterConfig{Name: "peers",
		Endpoints: endpoints, Protocol: s.cluster.Protocol().String(),
		RoutingPolicy: stnrv2.RoutingPolicyRoundRobin.String()}))
}

func (s *testServer) dial(t *testing.T, network string) net.Conn {
	t.Helper()
	c, err := net.Dial(network, net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port))) //nolint:noctx
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// freePort reserves a kernel-allocated port of the given listener protocol and releases it.
func freePort(t *testing.T, proto string) int {
	t.Helper()
	if proto == "UDP" {
		c, err := net.ListenPacket("udp", "127.0.0.1:0") //nolint:noctx
		require.NoError(t, err)
		defer func() { _ = c.Close() }()
		return c.LocalAddr().(*net.UDPAddr).Port
	}
	l, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// udpEcho starts a UDP echo peer and returns its address.
func udpEcho(t *testing.T) *net.UDPAddr {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0") //nolint:noctx
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

// tcpEcho starts a TCP echo peer and returns its address.
func tcpEcho(t *testing.T) *net.TCPAddr {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
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
	return l.Addr().(*net.TCPAddr)
}

// echo writes msg and expects it back.
func echo(t *testing.T, c net.Conn, msg string) {
	t.Helper()
	_, err := c.Write([]byte(msg))
	require.NoError(t, err)
	require.NoError(t, c.SetReadDeadline(time.Now().Add(2*time.Second)))
	buf := make([]byte, 2048)
	n, err := c.Read(buf)
	require.NoError(t, err, "echo")
	assert.Equal(t, msg, string(buf[:n]))
}

// noEcho writes msg and expects nothing back.
func noEcho(t *testing.T, c net.Conn, msg string) {
	t.Helper()
	_, err := c.Write([]byte(msg))
	require.NoError(t, err)
	require.NoError(t, c.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	_, err = c.Read(make([]byte, 2048))
	assert.Error(t, err, "no echo")
}

// TestFlowUDPRelay covers the datagram round trip: client datagrams reach the endpoint and its
// responses come back on the same flow.
func TestFlowUDPRelay(t *testing.T) {
	peer := udpEcho(t)
	s := newTestServer(t, setup{listener: "UDP", proto: "UDP",
		endpoints: []string{peer.String()}})

	client := s.dial(t, "udp")
	echo(t, client, "hello")
	echo(t, client, "again")
	assert.Equal(t, 1, s.Sessions(), "one flow per client")

	require.NoError(t, s.Close(false))
	assert.Eventually(t, func() bool { return s.Sessions() == 0 }, time.Second,
		10*time.Millisecond, "flows torn down on close")
}

// TestFlowTCPClientRelay covers the stream-to-datagram adaptation: a TCP client flow reaches a
// UDP endpoint through a UDP cluster, chunk per read.
func TestFlowTCPClientRelay(t *testing.T) {
	peer := udpEcho(t)
	s := newTestServer(t, setup{listener: "TCP", proto: "UDP",
		endpoints: []string{peer.String()}})

	client := s.dial(t, "tcp")
	echo(t, client, "hello")
	assert.Equal(t, 1, s.Sessions())

	// a client FIN tears the flow down
	require.NoError(t, client.Close())
	assert.Eventually(t, func() bool { return s.Sessions() == 0 }, time.Second,
		10*time.Millisecond, "flow torn down on client close")
}

// TestFlowTCPPeerRelay covers a stream leg for a datagram client.
func TestFlowTCPPeerRelay(t *testing.T) {
	peer := tcpEcho(t)
	s := newTestServer(t, setup{listener: "UDP", proto: "TCP",
		endpoints: []string{peer.String()}})

	echo(t, s.dial(t, "udp"), "hello")
	assert.Equal(t, 1, s.Sessions())
}

// TestFlowNoTarget verifies that without a dialable endpoint no flow is created: a cluster of
// prefixes is a TURN filter, not a set of L4 targets.
func TestFlowNoTarget(t *testing.T) {
	s := newTestServer(t, setup{listener: "UDP", proto: "UDP",
		endpoints: []string{"127.0.0.0/8"}})
	noEcho(t, s.dial(t, "udp"), "hello")
	assert.Equal(t, 0, s.Sessions(), "no flow registered")
}

// TestFlowNoCluster verifies that a flow fails when the cluster the server names does not exist.
func TestFlowNoCluster(t *testing.T) {
	peer := udpEcho(t)
	s := newTestServer(t, setup{listener: "UDP", endpoints: []string{peer.String()}})
	noEcho(t, s.dial(t, "udp"), "hello")
	assert.Equal(t, 0, s.Sessions(), "no flow registered")
}

// TestFlowLoadBalancing verifies round robin over the dialable endpoints, and that a cluster
// change leaves live flows on their leg.
func TestFlowLoadBalancing(t *testing.T) {
	peer1, peer2 := udpEcho(t), udpEcho(t)
	s := newTestServer(t, setup{listener: "UDP", proto: "UDP",
		endpoints: []string{peer1.String(), peer2.String()}})

	c1, c2 := s.dial(t, "udp"), s.dial(t, "udp")
	echo(t, c1, "one")
	echo(t, c2, "two")
	assert.Equal(t, 2, s.Sessions())

	// both peers got a flow: the echo peers answer from their own address, and the flows are
	// pinned to theirs, so both echoes arriving means both endpoints were used
	s.setEndpoints(t, []string{"127.0.0.0/8"})
	echo(t, c1, "still one")
	echo(t, c2, "still two")
	noEcho(t, s.dial(t, "udp"), "new flows find no target")
}

// TestFlowIdleExpiry verifies the activity-driven idle teardown and that fresh traffic from the
// same client re-creates the flow.
func TestFlowIdleExpiry(t *testing.T) {
	defer func(d time.Duration) { l4.FlowTimeout = d }(l4.FlowTimeout)
	l4.FlowTimeout = 100 * time.Millisecond

	peer := udpEcho(t)
	s := newTestServer(t, setup{listener: "UDP", proto: "UDP",
		endpoints: []string{peer.String()}})
	client := s.dial(t, "udp")

	echo(t, client, "hello")
	assert.Equal(t, 1, s.Sessions(), "flow created")

	// keep the flow busy over several timer periods: activity re-arms the idle timer
	for i := 0; i < 5; i++ {
		time.Sleep(50 * time.Millisecond)
		echo(t, client, "hello")
	}
	assert.Equal(t, 1, s.Sessions(), "active flow survives the idle timer")

	// quiet flow expires
	assert.Eventually(t, func() bool { return s.Sessions() == 0 }, time.Second,
		10*time.Millisecond, "idle flow torn down")

	// fresh traffic re-creates the flow
	echo(t, client, "hello")
	assert.Equal(t, 1, s.Sessions(), "flow re-created on traffic")
}

// TestFlowGoroutineLeak verifies that flow and server teardown release the pump goroutines.
func TestFlowGoroutineLeak(t *testing.T) {
	t.Cleanup(test.CheckRoutines(t))

	peer := udpEcho(t)
	s := newTestServer(t, setup{listener: "UDP", proto: "UDP",
		endpoints: []string{peer.String()}})
	for i := 0; i < 5; i++ {
		c, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port))) //nolint:noctx
		require.NoError(t, err)
		echo(t, c, "hello")
		require.NoError(t, c.Close())
	}
	require.NoError(t, s.Close(true))
}

// TestFlowEvents covers the default event wiring: a flow increments the per-client-IP quota
// accounting, and teardown releases it.
func TestFlowEvents(t *testing.T) {
	quota := &spyQuota{}
	peer := udpEcho(t)
	s := newTestServer(t, setup{listener: "UDP", proto: "UDP",
		endpoints: []string{peer.String()}, quota: quota})
	echo(t, s.dial(t, "udp"), "hello")

	incs, decs := quota.snapshot()
	require.Len(t, incs, 1, "quota incremented")
	assert.Equal(t, "127.0.0.1@"+stnrv2.DefaultRealm, incs[0], "client IP is the principal")
	assert.Empty(t, decs, "no decrement while the flow lives")

	require.NoError(t, s.Close(false))
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
	s := newTestServer(t, setup{listener: "UDP", proto: "UDP",
		endpoints: []string{peer.String()}, quota: quota})
	noEcho(t, s.dial(t, "udp"), "hello")
	assert.Equal(t, 0, s.Sessions(), "no flow registered")

	incs, decs := quota.snapshot()
	assert.Empty(t, incs, "nothing counted")
	assert.Empty(t, decs, "nothing released")
}
