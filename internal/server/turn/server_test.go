package turn_test

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/pion/transport/v5/stdnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/netconn"
	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/offload"
	"github.com/l7mp/stunner/v2/internal/quota"
	"github.com/l7mp/stunner/v2/internal/resolver"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server/turn"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
	"github.com/l7mp/stunner/v2/pkg/utils/turnclient"
)

// stubObject registers a config in the runtime without the object behind it.
type stubObject struct {
	typ  runtime.ObjectType
	conf stnrv2.Config
}

func (o stubObject) Name() string                { return o.conf.ConfigName() }
func (o stubObject) Type() runtime.ObjectType    { return o.typ }
func (stubObject) Start() error                  { return nil }
func (stubObject) Close(bool) error              { return nil }
func (o stubObject) GetConfig() stnrv2.Config    { return o.conf }
func (stubObject) Reconcile(stnrv2.Config) error { return nil }
func (stubObject) Status() stnrv2.Status         { return nil }
func (stubObject) Inspect(_, _ stnrv2.Config, _ *stnrv2.StunnerConfig) (runtime.Action, error) {
	return runtime.ActionNone, nil
}

// fakeOffload records the connection pairs registered with it.
type fakeOffload struct {
	offload.NullEngine
	mu       sync.Mutex
	upserted []string
	removed  []string
}

func (f *fakeOffload) Upsert(client, peer offload.Connection, listener, cluster string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upserted = append(f.upserted, listener+"/"+cluster)
	return nil
}

func (f *fakeOffload) Remove(client, peer offload.Connection) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, client.String())
	return nil
}

func (f *fakeOffload) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.upserted), len(f.removed)
}

type testServer struct {
	*turn.Server
	rt      *runtime.Runtime
	cluster *object.Cluster
	conf    *stnrv2.ServerConfig
	offload *fakeOffload
	ports   map[string]int
}

const realm = "stunner.test"

// newTestServer starts a TURN server behind a UDP and a TCP listener, relaying to the peers of
// 127.0.0.1 through the plain UDP cluster it names. The listeners and the server resolve each
// other by name through the registry.
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	log := logger.NewLoggerFactory("all:ERROR")
	tm, err := telemetry.New(telemetry.Callbacks{}, true, log.NewLogger("telemetry"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tm.Close() })
	nw, err := stdnet.NewNet()
	require.NoError(t, err)
	fo := &fakeOffload{}
	rt := runtime.New(runtime.Config{Logger: log, Telemetry: tm, Net: nw, OffloadEngine: fo,
		Resolver: resolver.NewMockResolver(map[string][]string{}, log)})
	rt.QuotaHandler = quota.New()

	auth := &stnrv2.AuthConfig{Name: stnrv2.DefaultAuthName, Type: "static", Realm: realm,
		Credentials: map[string]string{"username": "user", "password": "pass"}}
	require.NoError(t, auth.Validate())
	admin := &stnrv2.AdminConfig{}
	require.NoError(t, admin.Validate())
	require.NoError(t, rt.Registry.Add(stubObject{typ: runtime.TypeAuth, conf: auth}, nil))
	require.NoError(t, rt.Registry.Add(stubObject{typ: runtime.TypeAdmin, conf: admin}, nil))

	cl, err := object.NewCluster(&stnrv2.ClusterConfig{Name: "loopback",
		Endpoints: []string{"127.0.0.1"}, Protocol: "UDP", Addrs: []string{"127.0.0.1"}}, rt)
	require.NoError(t, err)
	require.NoError(t, rt.Registry.Add(cl, nil))
	require.NoError(t, cl.Start())
	// a permission is granted only by a matching ALLOW rule
	all, err := object.NewFilter(&stnrv2.FilterConfig{Name: "all", Rules: []stnrv2.FilterRule{
		{Action: "ALLOW"}}})
	require.NoError(t, err)
	require.NoError(t, rt.Registry.Add(all, nil))
	conf := &stnrv2.ServerConfig{ServerSpec: stnrv2.ServerSpec{Name: "turn", Type: "turn", Clusters: []string{"loopback"},
		Filters: []string{"all"}}}
	require.NoError(t, conf.Validate())
	s, err := turn.New(conf, rt)
	require.NoError(t, err)
	require.NoError(t, rt.Registry.Add(s, nil))
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Close(true) })

	ts := &testServer{Server: s, rt: rt, cluster: cl.(*object.Cluster), conf: conf, offload: fo,
		ports: map[string]int{}}
	for _, proto := range []string{"UDP", "TCP"} {
		port := freePort(t, proto)
		conf := &stnrv2.ListenerConfig{Name: proto, Protocol: proto, Addr: "127.0.0.1",
			Port: port, Servers: []string{"turn"}}
		require.NoError(t, conf.Validate())
		l, err := object.NewListener(conf, rt)
		require.NoError(t, err)
		require.NoError(t, l.Start())
		t.Cleanup(func() { _ = l.Close(false) })
		ts.ports[proto] = port
	}
	return ts
}

func freePort(t *testing.T, proto string) int {
	t.Helper()
	var addr string
	if proto == "TCP" {
		l, err := net.Listen("tcp", "127.0.0.1:0") //nolint:noctx
		require.NoError(t, err)
		addr = l.Addr().String()
		require.NoError(t, l.Close())
	} else {
		c, err := net.ListenPacket("udp", "127.0.0.1:0") //nolint:noctx
		require.NoError(t, err)
		addr = c.LocalAddr().String()
		require.NoError(t, c.Close())
	}
	_, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	p, err := strconv.Atoi(port)
	require.NoError(t, err)
	return p
}

func udpEcho(t *testing.T) net.PacketConn {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0") //nolint:noctx
	require.NoError(t, err)
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(buf[:n], from)
		}
	}()
	return pc
}

// allocate makes an allocation through the test server over the given transport.
func (ts *testServer) allocate(t *testing.T, proto stnrv2.Protocol, port int) *turnclient.PacketConn {
	t.Helper()
	d := turnclient.Dialer{Config: turnclient.Config{
		Protocol:   proto,
		ServerAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		Username:   "user",
		Password:   "pass",
		Realm:      realm,
	}}
	c, err := d.ListenPacket(context.Background(), "udp", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func relay(t *testing.T, c net.PacketConn, peer net.Addr, msg string) {
	t.Helper()
	require.Eventually(t, func() bool {
		if _, err := c.WriteTo([]byte(msg), peer); err != nil {
			return false
		}
		_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		buf := make([]byte, 1500)
		n, _, err := c.ReadFrom(buf)
		return err == nil && string(buf[:n]) == msg
	}, 5*time.Second, 10*time.Millisecond)
}

func TestTURNServerRelays(t *testing.T) {
	ts := newTestServer(t)
	peer := udpEcho(t)

	udp := ts.allocate(t, stnrv2.ProtocolTURNUDP, ts.ports["UDP"])
	relay(t, udp, peer.LocalAddr(), "over udp")
	tcp := ts.allocate(t, stnrv2.ProtocolTURNTCP, ts.ports["TCP"])
	relay(t, tcp, peer.LocalAddr(), "over tcp")
	assert.Equal(t, 2, ts.Sessions())

	// the relay socket is advertised at the listeners' address
	assert.Equal(t, "127.0.0.1", udp.LocalAddr().(*net.UDPAddr).IP.String())

	// closing the allocation deletes it
	require.NoError(t, udp.Close())
	require.Eventually(t, func() bool { return ts.Sessions() == 1 }, 5*time.Second, 20*time.Millisecond)
}

// TestTURNServerRefusesStreamlessConns pins that a TURN server refuses a conn of a protocol it
// cannot serve, from a STDIN listener here, with an error.
func TestTURNServerRefusesStreamlessConns(t *testing.T) {
	ts := newTestServer(t)
	c1, c2 := net.Pipe()
	t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })
	_, _, err := ts.Serve(netconn.NewConn(c1, api.Tag{Name: "stdin", Proto: stnrv2.ProtocolSTDIN}, nil))
	assert.ErrorContains(t, err, "cannot serve a STDIN conn")
	assert.Equal(t, 0, ts.Sessions(), "nothing tracked")
}

func TestTURNServerPermissions(t *testing.T) {
	ts := newTestServer(t)
	rules := func(allow string) []stnrv2.FilterRule {
		return []stnrv2.FilterRule{{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: allow}},
			{Action: "DENY", IPBlock: &stnrv2.IPBlock{CIDR: "0.0.0.0/0"}}}
	}
	filter, err := object.NewFilter(&stnrv2.FilterConfig{Name: "peers", Rules: rules("10.0.0.1/32")})
	require.NoError(t, err)
	require.NoError(t, ts.rt.Registry.Add(filter, nil))
	ts.conf.Filters = []string{"peers"}
	require.NoError(t, ts.Reconcile(ts.conf))
	peer := udpEcho(t)

	c := ts.allocate(t, stnrv2.ProtocolTURNUDP, ts.ports["UDP"])
	_, _ = c.WriteTo([]byte("x"), peer.LocalAddr())
	require.NoError(t, c.SetReadDeadline(time.Now().Add(500*time.Millisecond)))
	buf := make([]byte, 16)
	_, _, err = c.ReadFrom(buf)
	assert.Error(t, err, "a peer the server's filters refuse is not reachable")

	// a filter change applies to the new permissions of running allocations
	require.NoError(t, filter.Reconcile(&stnrv2.FilterConfig{Name: "peers", Rules: rules("127.0.0.1/32")}))
	c2 := ts.allocate(t, stnrv2.ProtocolTURNUDP, ts.ports["UDP"])
	relay(t, c2, peer.LocalAddr(), "admitted")
}

// TestTURNServerFilterPorts pins that the server's filters check the ports of the peers on the
// traffic, whatever cluster made the relay socket: the permission is granted by IP, the datagrams of
// a port the filters refuse are dropped, and only an admitted peer is offloaded.
func TestTURNServerFilterPorts(t *testing.T) {
	ts := newTestServer(t)
	in, out := udpEcho(t), udpEcho(t)
	port := uint16(in.LocalAddr().(*net.UDPAddr).Port)
	filter, err := object.NewFilter(&stnrv2.FilterConfig{Name: "peers", Rules: []stnrv2.FilterRule{
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "127.0.0.1/32"},
			Ports: []stnrv2.FilterPort{{Port: int(port), EndPort: int(port)}}},
		{Action: "DENY"}}})
	require.NoError(t, err)
	require.NoError(t, ts.rt.Registry.Add(filter, nil))
	ts.conf.Filters = []string{"peers"}
	require.NoError(t, ts.Reconcile(ts.conf))

	c := ts.allocate(t, stnrv2.ProtocolTURNUDP, ts.ports["UDP"])
	relay(t, c, in.LocalAddr(), "admitted port")
	require.Eventually(t, func() bool {
		up, _ := ts.offload.counts()
		return up == 1
	}, 5*time.Second, 20*time.Millisecond, "the admitted peer is offloaded")

	for range 3 {
		_, _ = c.WriteTo([]byte("x"), out.LocalAddr())
	}
	require.NoError(t, c.SetReadDeadline(time.Now().Add(500*time.Millisecond)))
	buf := make([]byte, 16)
	for {
		n, from, err := c.ReadFrom(buf)
		if err != nil {
			break
		}
		assert.NotEqual(t, out.LocalAddr().String(), from.String(), "a refused port is unreachable: %q", buf[:n])
	}
	time.Sleep(200 * time.Millisecond)
	up, _ := ts.offload.counts()
	assert.Equal(t, 1, up, "a refused peer is not offloaded")
}

func TestTURNServerPreAllocationTimeout(t *testing.T) {
	saved := turn.PreAllocationTimeout
	turn.PreAllocationTimeout = 200 * time.Millisecond
	t.Cleanup(func() { turn.PreAllocationTimeout = saved })
	ts := newTestServer(t)

	// a client that never allocates
	c, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ts.ports["UDP"]))) //nolint:noctx
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck
	_, err = c.Write([]byte("not a TURN message, still a new client"))
	require.NoError(t, err)
	// the default UDP listener demuxes over sockets bound to the listener's address
	local := &net.UDPAddr{IP: net.ParseIP("127.0.0.1").To4(), Port: ts.ports["UDP"]}
	require.Eventually(t, func() bool {
		_, ok := ts.Conn(c.LocalAddr(), local)
		return ok
	}, 5*time.Second, 10*time.Millisecond, "the listener emitted a conn")
	require.Eventually(t, func() bool {
		_, ok := ts.Conn(c.LocalAddr(), local)
		return !ok
	}, 5*time.Second, 20*time.Millisecond, "the conn is closed without an allocation")

	// an allocated conn survives the timeout
	a := ts.allocate(t, stnrv2.ProtocolTURNUDP, ts.ports["UDP"])
	time.Sleep(3 * turn.PreAllocationTimeout)
	relay(t, a, udpEcho(t).LocalAddr(), "still here")
}

// TestTURNServerUntracksClosedStreams pins that a stream conn leaves the conntrack when it closes,
// whether it allocated or not.
func TestTURNServerUntracksClosedStreams(t *testing.T) {
	ts := newTestServer(t)

	// a client that never allocates
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(ts.ports["TCP"]))) //nolint:noctx
	require.NoError(t, err)
	require.Eventually(t, func() bool { return ts.Tracked() == 1 }, 5*time.Second, 10*time.Millisecond,
		"the listener emitted a conn")
	require.NoError(t, c.Close())
	require.Eventually(t, func() bool { return ts.Tracked() == 0 }, 5*time.Second, 20*time.Millisecond,
		"a conn closed without an allocation is untracked")

	// a client that allocates
	a := ts.allocate(t, stnrv2.ProtocolTURNTCP, ts.ports["TCP"])
	relay(t, a, udpEcho(t).LocalAddr(), "allocated")
	require.Equal(t, 1, ts.Tracked())
	require.NoError(t, a.Close())
	require.Eventually(t, func() bool { return ts.Tracked() == 0 }, 5*time.Second, 20*time.Millisecond,
		"a conn closed after an allocation is untracked")
}

func TestTURNServerRealmRebuild(t *testing.T) {
	ts := newTestServer(t)
	peer := udpEcho(t)
	relay(t, ts.allocate(t, stnrv2.ProtocolTURNUDP, ts.ports["UDP"]), peer.LocalAddr(), "before")

	require.NoError(t, ts.Close(false))
	assert.Equal(t, 0, ts.Sessions())
	require.NoError(t, ts.Start())
	// the listeners survived the rebuild
	relay(t, ts.allocate(t, stnrv2.ProtocolTURNUDP, ts.ports["UDP"]), peer.LocalAddr(), "after")
}

func TestTURNServerOffload(t *testing.T) {
	ts := newTestServer(t)
	peer := udpEcho(t)

	// a TCP client is never offloaded, even through a plain UDP relay
	tcp := ts.allocate(t, stnrv2.ProtocolTURNTCP, ts.ports["TCP"])
	relay(t, tcp, peer.LocalAddr(), "tcp")
	time.Sleep(200 * time.Millisecond)
	up, _ := ts.offload.counts()
	assert.Equal(t, 0, up)

	// a UDP client through a plain UDP relay is, once its channel is bound
	udp := ts.allocate(t, stnrv2.ProtocolTURNUDP, ts.ports["UDP"])
	relay(t, udp, peer.LocalAddr(), "udp")
	require.Eventually(t, func() bool {
		up, _ := ts.offload.counts()
		return up == 1
	}, 5*time.Second, 20*time.Millisecond)
	ts.offload.mu.Lock()
	assert.Equal(t, "UDP/loopback", ts.offload.upserted[0], "listener and cluster")
	ts.offload.mu.Unlock()

	// deleting the allocation removes what was registered
	require.NoError(t, udp.Close())
	require.Eventually(t, func() bool {
		_, rm := ts.offload.counts()
		return rm == 1
	}, 5*time.Second, 20*time.Millisecond)
}
