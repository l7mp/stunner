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
	"github.com/l7mp/stunner/v2/internal/listener"
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
	offload *fakeOffload
	ports   map[string]int
}

const realm = "stunner.test"

// newTestServer starts a TURN server behind a UDP and a TCP listener, relaying to the peers of
// 127.0.0.0/8 through the plain UDP cluster it names. The server resolves the cluster by name
// through the registry, so it is registered as a real object, and its own config as a stub.
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
	rt.QuotaHandler = quota.New(rt)

	auth := &stnrv2.AuthConfig{Type: "static", Realm: realm,
		Credentials: map[string]string{"username": "user", "password": "pass"}}
	require.NoError(t, auth.Validate())
	admin := &stnrv2.AdminConfig{}
	require.NoError(t, admin.Validate())
	require.NoError(t, rt.Registry.Add(stubObject{typ: runtime.TypeAuth, conf: auth}, nil))
	require.NoError(t, rt.Registry.Add(stubObject{typ: runtime.TypeAdmin, conf: admin}, nil))

	cl, err := object.NewCluster(&stnrv2.ClusterConfig{Name: "loopback",
		Endpoints: []string{"127.0.0.0/8"}, Protocol: "UDP", Addrs: []string{"127.0.0.1"}}, rt)
	require.NoError(t, err)
	require.NoError(t, rt.Registry.Add(cl, nil))
	require.NoError(t, cl.Start())
	conf := &stnrv2.ServerConfig{Name: "turn", Type: "turn", Clusters: []string{"loopback"}}
	require.NoError(t, conf.Validate())
	require.NoError(t, rt.Registry.Add(stubObject{typ: runtime.TypeServer, conf: conf}, nil))

	s := turn.NewServer("turn", rt)
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Close(true) })

	ts := &testServer{Server: s, rt: rt, cluster: cl.(*object.Cluster), offload: fo, ports: map[string]int{}}
	for _, proto := range []string{"UDP", "TCP"} {
		port := freePort(t, proto)
		conf := &stnrv2.ListenerConfig{Name: proto, Protocol: proto, Addr: "127.0.0.1",
			Port: port, Servers: []string{"turn"}}
		require.NoError(t, conf.Validate())
		l, err := listener.New(conf, rt, func(c api.Conn) {
			if _, _, err := s.Serve(c); err != nil {
				_ = c.Close()
			}
		})
		require.NoError(t, err)
		require.NoError(t, l.Start())
		t.Cleanup(func() { _ = l.Close() })
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

func TestTURNServerPermissions(t *testing.T) {
	ts := newTestServer(t)
	require.NoError(t, ts.cluster.Reconcile(&stnrv2.ClusterConfig{Name: "loopback",
		Endpoints: []string{"10.0.0.0/8"}, Protocol: "UDP", Addrs: []string{"127.0.0.1"}}))
	peer := udpEcho(t)

	c := ts.allocate(t, stnrv2.ProtocolTURNUDP, ts.ports["UDP"])
	_, _ = c.WriteTo([]byte("x"), peer.LocalAddr())
	require.NoError(t, c.SetReadDeadline(time.Now().Add(500*time.Millisecond)))
	buf := make([]byte, 16)
	_, _, err := c.ReadFrom(buf)
	assert.Error(t, err, "a peer outside the cluster is not reachable")

	// a cluster change applies to running allocations' new permissions
	require.NoError(t, ts.cluster.Reconcile(&stnrv2.ClusterConfig{Name: "loopback",
		Endpoints: []string{"127.0.0.1"}, Protocol: "UDP", Addrs: []string{"127.0.0.1"}}))
	c2 := ts.allocate(t, stnrv2.ProtocolTURNUDP, ts.ports["UDP"])
	relay(t, c2, peer.LocalAddr(), "admitted")
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
