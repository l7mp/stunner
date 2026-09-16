package stunner

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/pion/transport/v5/test"
	"github.com/pion/turn/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

// TestStunnerTURNRelayChain exercises the TURN-client relay cluster end-to-end: a client
// allocates on a downstream stunnerd whose only route is a TURN-* protocol cluster pointing at an
// upstream stunnerd, sends to a UDP echo server, and the datagrams must traverse the upstream
// TURN relay in both directions. The downstream local relay socket is fail-closed for
// TURN-cluster peers, so a successful echo proves the upstream traversal.
func TestStunnerTURNRelayChain(t *testing.T) {
	lim := test.TimeOut(time.Second * 120)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, upstreamProto := range []string{"turn-udp", "turn-tcp"} {
		t.Run("TURNRelayChain_upstream:"+upstreamProto, func(t *testing.T) {
			log.Debugf("-------------- Running test: TURN relay chain over %s -------------", upstreamProto)

			upstream, downstream := setupTURNRelayChain(t, upstreamProto)
			defer closeStunner(upstream)
			defer closeStunner(downstream)

			relayChainEchoTest(t, log, true)

			// the upstream leg carries the flow: exactly one upstream allocation
			l := upstream.GetListener("upstream")
			require.NotNil(t, l, "upstream listener")
			assert.Equal(t, 1, l.AllocationCount(), "upstream allocation count")
		})
	}
}

// setupTURNRelayChain stands up the upstream stunnerd (TURN listener on 127.0.0.1:23479, admits
// any peer) and the downstream stunnerd (TURN-UDP listener on 127.0.0.1:23478 routing
// to a single TURN-* protocol cluster that passes traffic through the upstream).
func setupTURNRelayChain(t *testing.T, upstreamProto string) (*Stunner, *Stunner) {
	t.Helper()

	// disable the health-check server: the two in-process instances would collide on the
	// default port
	noHealthCheck := ""

	upstream := NewStunner(Options{
		Name:             "upstream",
		LogOptions:       LogOptions{Level: stunnerTestLoglevel},
		SuppressRollback: true,
	})
	require.NoError(t, upstream.Reconcile(&stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
		Admin: stnrv1.AdminConfig{
			LogLevel:            stunnerTestLoglevel,
			HealthCheckEndpoint: &noHealthCheck,
		},
		Auth: stnrv1.AuthConfig{
			Type:        "static",
			Credentials: map[string]string{"username": "user2", "password": "passwd2"},
		},
		Listeners: []stnrv1.ListenerConfig{{
			Name:     "upstream",
			Protocol: upstreamProto,
			Addr:     "127.0.0.1",
			Port:     23479,
			Routes:   []string{"allow-any"},
		}},
		Clusters: []stnrv1.ClusterConfig{{
			Name:      "allow-any",
			Endpoints: []string{"0.0.0.0/0"},
		}},
	}), "upstream server started")

	downstream := NewStunner(Options{
		Name:             "downstream",
		LogOptions:       LogOptions{Level: stunnerTestLoglevel},
		SuppressRollback: true,
	})
	require.NoError(t, downstream.Reconcile(&stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
		Admin: stnrv1.AdminConfig{
			LogLevel:            stunnerTestLoglevel,
			HealthCheckEndpoint: &noHealthCheck,
		},
		Auth: stnrv1.AuthConfig{
			Type:        "static",
			Credentials: map[string]string{"username": "user1", "password": "passwd1"},
		},
		Listeners: []stnrv1.ListenerConfig{{
			Name:     "downstream",
			Protocol: "turn-udp",
			Addr:     "127.0.0.1",
			Port:     23478,
			Routes:   []string{"turn-relay"},
		}},
		Clusters: []stnrv1.ClusterConfig{{
			Name:     "turn-relay",
			Protocol: upstreamProto,
			TURNServer: &stnrv1.TURNServer{
				Address: "127.0.0.1",
				Port:    23479,
				Auth: &stnrv1.AuthConfig{Type: "static", Credentials: map[string]string{
					"username": "user2", "password": "passwd2"}},
			},
		}},
	}), "downstream server started")

	return upstream, downstream
}

// TestStunnerTURNRelayChainTCP exercises downstream-TCP relaying: a client makes an RFC 6062
// TCP allocation on a downstream stunnerd whose TURN-* cluster points at an upstream
// stunnerd over a TCP control connection; the upstream relays (RFC 6062) to a TCP echo peer. A
// successful echo proves the two-hop TCP relay chain.
func TestStunnerTURNRelayChainTCP(t *testing.T) {
	lim := test.TimeOut(time.Second * 120)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")
	log.Debug("-------------- Running test: TCP relay chain --------------")

	noHealthCheck := ""

	// upstream: TURN-TCP listener with a TCP (RFC 6062) cluster that dials the peer directly
	upstream := NewStunner(Options{Name: "upstream", LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
	require.NoError(t, upstream.Reconcile(&stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
		Admin:      stnrv1.AdminConfig{LogLevel: stunnerTestLoglevel, HealthCheckEndpoint: &noHealthCheck},
		Auth:       stnrv1.AuthConfig{Type: "static", Credentials: map[string]string{"username": "user2", "password": "passwd2"}},
		Listeners: []stnrv1.ListenerConfig{{
			Name: "upstream", Protocol: "turn-tcp", Addr: "127.0.0.1", Port: 23479, Routes: []string{"tcp-echo"},
		}},
		Clusters: []stnrv1.ClusterConfig{{
			Name: "tcp-echo", Protocol: "tcp", Endpoints: []string{"127.0.0.1"},
		}},
	}), "upstream server started")
	defer closeStunner(upstream)

	// downstream: TURN-TCP listener pointing at the upstream over TCP
	downstream := NewStunner(Options{Name: "downstream", LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
	require.NoError(t, downstream.Reconcile(&stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
		Admin:      stnrv1.AdminConfig{LogLevel: stunnerTestLoglevel, HealthCheckEndpoint: &noHealthCheck},
		Auth:       stnrv1.AuthConfig{Type: "static", Credentials: map[string]string{"username": "user1", "password": "passwd1"}},
		Listeners: []stnrv1.ListenerConfig{{
			Name: "downstream", Protocol: "turn-tcp", Addr: "127.0.0.1", Port: 23478, Routes: []string{"turn-relay"},
		}},
		Clusters: []stnrv1.ClusterConfig{{
			Name: "turn-relay", Protocol: "turn-tcp",
			TURNServer: &stnrv1.TURNServer{Address: "127.0.0.1", Port: 23479, Auth: &stnrv1.AuthConfig{Type: "static", Credentials: map[string]string{"username": "user2", "password": "passwd2"}}},
		}},
	}), "downstream server started")
	defer closeStunner(downstream)

	// TCP echo peer
	echoLn, err := net.Listen("tcp", "127.0.0.1:25679")
	require.NoError(t, err, "echo listener")
	defer echoLn.Close() //nolint:errcheck
	go func() {
		for {
			ec, aerr := echoLn.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close() //nolint:errcheck
				buf := make([]byte, 1600)
				for {
					n, rerr := c.Read(buf)
					if n > 0 {
						_, _ = c.Write(buf[:n])
					}
					if rerr != nil {
						return
					}
				}
			}(ec)
		}
	}()

	// client: TCP control connection to the downstream, TCP allocation, Connect+Dial the peer
	ctrlConn, err := net.Dial("tcp", "127.0.0.1:23478")
	require.NoError(t, err, "client control connection")
	client, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: "127.0.0.1:23478",
		TURNServerAddr: "127.0.0.1:23478",
		Username:       "user1",
		Password:       "passwd1",
		Conn:           turn.NewSTUNConn(ctrlConn),
	})
	require.NoError(t, err, "TURN client")
	require.NoError(t, client.Listen(), "TURN client listen")
	defer func() { client.Close(); ctrlConn.Close() }() //nolint:errcheck

	alloc, err := client.AllocateTCP()
	require.NoError(t, err, "downstream TCP allocation")
	defer alloc.Close() //nolint:errcheck

	peer := "127.0.0.1:25679"
	peerAddr, err := net.ResolveTCPAddr("tcp", peer)
	require.NoError(t, err, "resolve peer")
	require.NoError(t, client.CreatePermission(peerAddr), "create permission")

	dataConn, err := alloc.Dial("tcp", peer)
	require.NoError(t, err, "TCP connect through the relay chain")
	defer dataConn.Close() //nolint:errcheck

	buf := make([]byte, 1600)
	for i := 0; i < 4; i++ {
		msg := fmt.Sprintf("Hello-%d", i)
		_, err = dataConn.Write([]byte(msg))
		require.NoError(t, err, "write to relay")
		require.NoError(t, dataConn.SetReadDeadline(time.Now().Add(5*time.Second)))
		n, rerr := dataConn.Read(buf)
		require.NoError(t, rerr, "read from relay")
		assert.Equal(t, msg, string(buf[:n]), "echoed message")
	}
	log.Debug("TCP relay-chain echo round-trips done")
}

func closeStunner(s *Stunner) {
	s.Shutdown()
	s.Close()
}

// relayChainEchoTest allocates on the downstream stunnerd and runs an echo round-trip against a
// local UDP echo server. The first datagrams may be dropped while the downstream dials the
// upstream TURN session, so sends are retried until the first reply arrives.
func relayChainEchoTest(t *testing.T, log interface{ Debugf(string, ...interface{}) }, echoSuccess bool) {
	t.Helper()

	lconn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	require.NoError(t, err, "client socket")
	defer lconn.Close() //nolint:errcheck

	client, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: "127.0.0.1:23478",
		TURNServerAddr: "127.0.0.1:23478",
		Username:       "user1",
		Password:       "passwd1",
		Conn:           lconn,
	})
	require.NoError(t, err, "TURN client")
	require.NoError(t, client.Listen(), "TURN client listen")
	defer client.Close()

	conn, err := client.Allocate()
	require.NoError(t, err, "downstream allocation")
	defer conn.Close() //nolint:errcheck

	echoConn, err := net.ListenPacket("udp4", "127.0.0.1:25678")
	require.NoError(t, err, "echo server socket")
	defer echoConn.Close() //nolint:errcheck

	go func() {
		buf := make([]byte, 1600)
		for {
			n, from, err2 := echoConn.ReadFrom(buf)
			if err2 != nil {
				return
			}
			// echo back to whoever sent (the sender is the serving relay address:
			// the upstream relay for TURN clusters, the downstream one for direct)
			_, _ = echoConn.WriteTo(buf[:n], from)
		}
	}()

	// retry the first round-trip: datagrams are dropped while the upstream session dials
	buf := make([]byte, 1600)
	echoed := false
	for i := 0; i < 20 && !echoed; i++ {
		_, err = conn.WriteTo([]byte("Hello"), echoConn.LocalAddr())
		require.NoError(t, err, "relayed send")

		require.NoError(t, conn.SetReadDeadline(time.Now().Add(250*time.Millisecond)))
		n, from, err2 := conn.ReadFrom(buf)
		if err2 != nil {
			continue
		}
		assert.Equal(t, "Hello", string(buf[:n]), "echo payload")
		// the peer's real source address is preserved end-to-end
		assert.Equal(t, echoConn.LocalAddr().String(), from.String(), "echo peer address")
		echoed = true
	}
	if echoSuccess {
		require.True(t, echoed, "echo round-trip succeeded")
		log.Debugf("echo round-trip done")

		// a couple more round-trips over the established session
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		for i := 0; i < 4; i++ {
			_, err = conn.WriteTo([]byte(fmt.Sprintf("Hello-%d", i)), echoConn.LocalAddr())
			require.NoError(t, err, "relayed send")
			n, _, err2 := conn.ReadFrom(buf)
			require.NoError(t, err2, "relayed receive")
			assert.Equal(t, fmt.Sprintf("Hello-%d", i), string(buf[:n]), "echo payload")
		}
	} else {
		require.False(t, echoed, "echo round-trip failed as expected")
	}
}
