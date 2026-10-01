package stunner

import (
	"net"
	"testing"
	"time"

	"github.com/pion/transport/v5"
	"github.com/pion/transport/v5/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/server/l4"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

// plainListenerConfig builds a single-listener stunnerd config with a listener of the given
// protocol at host:port feeding an l4 server that relays each client flow to an endpoint of its
// UDP cluster.
func plainListenerConfig(proto, host string, port int, endpoints ...string) *stnrv2.StunnerConfig {
	noHealthCheck := ""
	return &stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin: stnrv2.AdminConfig{
			LogLevel:            stunnerTestLoglevel,
			HealthCheckEndpoint: &noHealthCheck,
		},
		Auth: stnrv2.AuthConfig{
			Type:        "static",
			Credentials: map[string]string{"username": "user1", "password": "passwd1"},
		},
		Listeners: []stnrv2.ListenerConfig{{
			Name:     "plain",
			Protocol: proto,
			Servers:  []string{"plain"},
			Addr:     host,
			Port:     port,
		}},
		Servers: []stnrv2.ServerConfig{{
			Name:     "plain",
			Type:     "l4",
			Clusters: []string{"peer"},
		}},
		Clusters: []stnrv2.ClusterConfig{{
			Name:          "peer",
			Endpoints:     endpoints,
			RoutingPolicy: "ROUND_ROBIN",
			Protocol:      "UDP",
		}},
	}
}

// startUDPEcho starts a UDP echo server at addr of nw.
func startUDPEcho(t *testing.T, nw transport.Net, addr string) net.PacketConn {
	t.Helper()
	c, err := nw.ListenPacket("udp4", addr)
	require.NoError(t, err, "echo server socket")
	t.Cleanup(func() { _ = c.Close() })
	go func() {
		buf := make([]byte, 1600)
		for {
			n, from, err := c.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = c.WriteTo(buf[:n], from)
		}
	}()
	return c
}

// TestStunnerPlainListener exercises the l4 flow engine end to end through the full reconcile
// machinery: server dispatch, endpoint selection, chunking, idle expiry.
func TestStunnerPlainListener(t *testing.T) {
	lim := test.TimeOut(time.Second * 120)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	t.Run("udp-round-trip", func(t *testing.T) {
		tn := vnetTestNet(t)
		defer tn.close()
		log.Debug("-------------- Running test: plain UDP round trip --------------")
		startUDPEcho(t, tn.pod, net.JoinHostPort(tn.peerHost, "25680"))
		s := NewStunner(Options{Name: "plain-udp", Net: tn.pod, LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
		defer closeStunner(s)
		require.NoError(t, s.Reconcile(plainListenerConfig("UDP", tn.host, 23478,
			net.JoinHostPort(tn.peerHost, "25680"))), "server started")

		client, err := tn.client.Dial("udp", net.JoinHostPort(tn.host, "23478"))
		require.NoError(t, err, "client dial")
		defer client.Close() //nolint:errcheck

		buf := make([]byte, 1600)
		_, err = client.Write([]byte("Hello"))
		require.NoError(t, err, "client write")
		require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
		n, err := client.Read(buf)
		require.NoError(t, err, "echo")
		assert.Equal(t, "Hello", string(buf[:n]), "echo payload")

		srv := s.GetServer("plain")
		require.NotNil(t, srv, "server")
		assert.Equal(t, 1, srv.Sessions(), "flow counted as allocation")
	})

	t.Run("tcp-client-chunking", func(t *testing.T) {
		tn := osTestNet(t)
		log.Debug("-------------- Running test: plain TCP client chunking --------------")
		startUDPEcho(t, tn.pod, net.JoinHostPort(tn.peerHost, "25680"))
		s := NewStunner(Options{Name: "plain-tcp", LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
		defer closeStunner(s)
		require.NoError(t, s.Reconcile(plainListenerConfig("TCP", tn.host, 23478,
			net.JoinHostPort(tn.peerHost, "25680"))), "server started")

		client, err := net.Dial("tcp", "127.0.0.1:23478")
		require.NoError(t, err, "client dial")
		defer client.Close() //nolint:errcheck

		// each stream write chunk makes one datagram towards the peer and each echoed
		// datagram makes one chunk back
		buf := make([]byte, 1600)
		require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
		for _, msg := range []string{"Hello-0", "Hello-1", "Hello-2"} {
			_, err = client.Write([]byte(msg))
			require.NoError(t, err, "client write")
			n, err := client.Read(buf)
			require.NoError(t, err, "echo")
			assert.Equal(t, msg, string(buf[:n]), "echo payload")
		}

		srv := s.GetServer("plain")
		require.NotNil(t, srv, "server")
		assert.Equal(t, 1, srv.Sessions(), "flow counted as allocation")

		// a client FIN tears the flow down
		require.NoError(t, client.Close())
		assert.Eventually(t, func() bool { return srv.Sessions() == 0 },
			5*time.Second, 10*time.Millisecond, "flow torn down on client close")
	})

	t.Run("no-dialable-endpoint", func(t *testing.T) {
		tn := vnetTestNet(t)
		defer tn.close()
		log.Debug("-------------- Running test: l4 server with no dialable endpoint --------------")
		startUDPEcho(t, tn.pod, net.JoinHostPort(tn.peerHost, "25680"))
		s := NewStunner(Options{Name: "plain-undialable", Net: tn.pod, LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
		defer closeStunner(s)
		// an endpoint without a port admits the peer but names no target to dial
		require.NoError(t, s.Reconcile(plainListenerConfig("UDP", tn.host, 23478,
			tn.peerHost)), "server started")

		client, err := tn.client.Dial("udp", net.JoinHostPort(tn.host, "23478"))
		require.NoError(t, err, "client dial")
		defer client.Close() //nolint:errcheck

		buf := make([]byte, 1600)
		_, err = client.Write([]byte("Hello"))
		require.NoError(t, err, "client write")
		require.NoError(t, client.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
		_, err = client.Read(buf)
		assert.Error(t, err, "no echo without a dialable endpoint")

		srv := s.GetServer("plain")
		require.NotNil(t, srv, "server")
		assert.Equal(t, 0, srv.Sessions(), "no flow registered")
	})

	t.Run("peer-change-reconciles-in-place", func(t *testing.T) {
		tn := vnetTestNet(t)
		defer tn.close()
		log.Debug("-------------- Running test: plain listener peer change --------------")
		startUDPEcho(t, tn.pod, net.JoinHostPort(tn.peerHost, "25680"))
		// the second peer tags its replies so the serving peer is observable
		tagged, err := tn.pod.ListenPacket("udp4", net.JoinHostPort(tn.peerHost, "25681"))
		require.NoError(t, err, "tagged echo server socket")
		defer tagged.Close() //nolint:errcheck
		go func() {
			buf := make([]byte, 1600)
			for {
				n, from, err := tagged.ReadFrom(buf)
				if err != nil {
					return
				}
				_, _ = tagged.WriteTo(append([]byte("B:"), buf[:n]...), from)
			}
		}()

		s := NewStunner(Options{Name: "plain-peer-change", Net: tn.pod, LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
		defer closeStunner(s)
		require.NoError(t, s.Reconcile(plainListenerConfig("UDP", tn.host, 23478,
			net.JoinHostPort(tn.peerHost, "25680"))), "server started")

		buf := make([]byte, 1600)
		echo := func(c net.Conn, want string) {
			t.Helper()
			_, err := c.Write([]byte("Hello"))
			require.NoError(t, err, "client write")
			require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
			n, err := c.Read(buf)
			require.NoError(t, err, "echo")
			assert.Equal(t, want, string(buf[:n]), "echo payload")
		}

		client1, err := tn.client.Dial("udp", net.JoinHostPort(tn.host, "23478"))
		require.NoError(t, err, "client 1 dial")
		defer client1.Close() //nolint:errcheck
		echo(client1, "Hello")

		// a cluster change reconciles in place: the existing flow stays pinned to the old
		// peer, a new flow goes to the new one
		require.NoError(t, s.Reconcile(plainListenerConfig("UDP", tn.host, 23478,
			net.JoinHostPort(tn.peerHost, "25681"))), "peer change reconciled")

		echo(client1, "Hello")

		client2, err := tn.client.Dial("udp", net.JoinHostPort(tn.host, "23478"))
		require.NoError(t, err, "client 2 dial")
		defer client2.Close() //nolint:errcheck
		echo(client2, "B:Hello")

		srv := s.GetServer("plain")
		require.NotNil(t, srv, "server")
		assert.Equal(t, 2, srv.Sessions(), "both flows live")
	})

	t.Run("idle-expiry", func(t *testing.T) {
		tn := vnetTestNet(t)
		defer tn.close()
		log.Debug("-------------- Running test: plain listener idle expiry --------------")
		// the flow timeout is a system constant; shorten it for the test
		defer func(d time.Duration) { l4.FlowTimeout = d }(l4.FlowTimeout)
		l4.FlowTimeout = 100 * time.Millisecond

		startUDPEcho(t, tn.pod, net.JoinHostPort(tn.peerHost, "25680"))
		s := NewStunner(Options{Name: "plain-idle", Net: tn.pod, LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
		defer closeStunner(s)
		require.NoError(t, s.Reconcile(plainListenerConfig("UDP", tn.host, 23478,
			net.JoinHostPort(tn.peerHost, "25680"))), "server started")

		client, err := tn.client.Dial("udp", net.JoinHostPort(tn.host, "23478"))
		require.NoError(t, err, "client dial")
		defer client.Close() //nolint:errcheck

		buf := make([]byte, 1600)
		echo := func() {
			_, err := client.Write([]byte("Hello"))
			require.NoError(t, err, "client write")
			require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
			_, err = client.Read(buf)
			require.NoError(t, err, "echo")
		}

		srv := s.GetServer("plain")
		require.NotNil(t, srv, "server")

		echo()
		assert.Equal(t, 1, srv.Sessions(), "flow created")

		// quiet flow expires
		assert.Eventually(t, func() bool { return srv.Sessions() == 0 },
			5*time.Second, 10*time.Millisecond, "idle flow torn down")

		// fresh traffic from the same client re-creates the flow
		echo()
		assert.Equal(t, 1, srv.Sessions(), "flow re-created on traffic")
	})
}

// TestStunnerPlainTunnelChain is the tunnel-mode chain matrix, covering the retired
// turncat's test matrix (client transport x upstream TURN transport x static/ephemeral
// upstream auth, with UDP peers) plus the tcp-tcp tunnel that turncat never had: a TCP
// client relayed to a TCP peer over an RFC 6062 upstream allocation. Each case runs a raw
// client against an l4 server whose only cluster holds the peer and tunnels through an upstream
// stunnerd; a successful raw echo plus exactly one upstream allocation proves the traversal.
func TestStunnerPlainTunnelChain(t *testing.T) {
	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	staticAuth := stnrv2.AuthConfig{Type: "static",
		Credentials: map[string]string{"username": "user2", "password": "passwd2"}}
	ephemeralAuth := stnrv2.AuthConfig{Type: "ephemeral",
		Credentials: map[string]string{"secret": "my-secret"}}

	testCases := []struct {
		name          string
		clientProto   string // downstream listener protocol
		upstreamProto string // transport towards the upstream TURN server
		auth          stnrv2.AuthConfig
		tcpPeer       bool
	}{
		{"udp-client:turn-udp:static", "UDP", "UDP", staticAuth, false},
		{"udp-client:turn-tcp:static", "UDP", "TCP", staticAuth, false},
		{"tcp-client:turn-udp:static", "TCP", "UDP", staticAuth, false},
		{"tcp-client:turn-tcp:static", "TCP", "TCP", staticAuth, false},
		{"udp-client:turn-udp:ephemeral", "UDP", "UDP", ephemeralAuth, false},
		{"udp-client:turn-tcp:ephemeral", "UDP", "TCP", ephemeralAuth, false},
		{"tcp-client:turn-udp:ephemeral", "TCP", "UDP", ephemeralAuth, false},
		{"tcp-client:turn-tcp:ephemeral", "TCP", "TCP", ephemeralAuth, false},
		{"tcp-client:turn-tcp:static:tcp-peer", "TCP", "TCP", staticAuth, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			log.Debugf("-------------- Running test: tunnel chain %s --------------", tc.name)
			lim := test.TimeOut(time.Second * 60)
			defer lim.Stop()

			// the routine check also serializes the fixed-port sockets between cases:
			// the UDP listener socket of a plain listener closes asynchronously
			report := test.CheckRoutines(t)
			defer report()

			// a chain with no TCP anywhere runs on the default vnet
			tn := osTestNet(t)
			if tc.clientProto == "UDP" && tc.upstreamProto == "UDP" && !tc.tcpPeer {
				tn = vnetTestNet(t)
			}
			defer tn.close()

			// the peer: a UDP echo server, or a TCP one for the tcp-tcp tunnel
			peerAddr, peerProto := net.JoinHostPort(tn.peerHost, "25680"), "UDP"
			upstreamCluster := stnrv2.ClusterConfig{Name: "allow-any", Protocol: "UDP",
				Endpoints: []string{"0.0.0.0/0"}}
			if tc.tcpPeer {
				peerAddr, peerProto = "127.0.0.1:25681", "TCP"
				upstreamCluster = stnrv2.ClusterConfig{Name: "tcp-echo", Protocol: "TCP",
					Endpoints: []string{"127.0.0.1"}}
				echoLn, err := net.Listen("tcp", "127.0.0.1:25681")
				require.NoError(t, err, "tcp echo listener")
				defer echoLn.Close() //nolint:errcheck
				go func() {
					for {
						ec, err := echoLn.Accept()
						if err != nil {
							return
						}
						go func(c net.Conn) {
							defer c.Close() //nolint:errcheck
							buf := make([]byte, 1600)
							for {
								n, err := c.Read(buf)
								if n > 0 {
									_, _ = c.Write(buf[:n])
								}
								if err != nil {
									return
								}
							}
						}(ec)
					}
				}()
			} else {
				echo := startUDPEcho(t, tn.pod, peerAddr)
				defer echo.Close() //nolint:errcheck
			}

			// each side gets its own copy of the auth config: reconciliation normalizes in place
			upstreamAuth, serverAuth := stnrv2.AuthConfig{}, stnrv2.AuthConfig{}
			tc.auth.DeepCopyInto(&upstreamAuth)
			tc.auth.DeepCopyInto(&serverAuth)

			tunnelProto, err := stnrv2.NewProtocol("TURN-" + tc.upstreamProto)
			require.NoError(t, err, "tunnel protocol")
			tunnelURL := (&stnrv2.URI{Protocol: tunnelProto, Host: tn.host, Port: 23479}).String()

			noHealthCheck := ""
			upstream := NewStunner(Options{Name: "upstream", Net: tn.pod, LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
			defer closeStunner(upstream)
			require.NoError(t, upstream.Reconcile(&stnrv2.StunnerConfig{
				ApiVersion: stnrv2.ApiVersion,
				Admin:      stnrv2.AdminConfig{LogLevel: stunnerTestLoglevel, HealthCheckEndpoint: &noHealthCheck},
				Auth:       upstreamAuth,
				Listeners: []stnrv2.ListenerConfig{{
					Name: "upstream", Protocol: tc.upstreamProto, Servers: []string{"upstream"},
					Addr: tn.host, Port: 23479,
				}},
				Servers: []stnrv2.ServerConfig{{
					Name: "upstream", Type: "turn", Clusters: []string{upstreamCluster.Name},
				}},
				Clusters: []stnrv2.ClusterConfig{upstreamCluster},
			}), "upstream server started")

			downstream := NewStunner(Options{Name: "downstream", Net: tn.pod, LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
			defer closeStunner(downstream)
			require.NoError(t, downstream.Reconcile(&stnrv2.StunnerConfig{
				ApiVersion: stnrv2.ApiVersion,
				Admin:      stnrv2.AdminConfig{LogLevel: stunnerTestLoglevel, HealthCheckEndpoint: &noHealthCheck},
				Auth:       stnrv2.AuthConfig{Type: "none"},
				Listeners: []stnrv2.ListenerConfig{{
					Name: "downstream", Protocol: tc.clientProto, Servers: []string{"downstream"},
					Addr: tn.host, Port: 23478,
				}},
				Servers: []stnrv2.ServerConfig{{
					Name: "downstream", Type: "l4", Clusters: []string{"peer"},
				}},
				Clusters: []stnrv2.ClusterConfig{{
					Name:          "peer",
					Endpoints:     []string{peerAddr},
					RoutingPolicy: "ROUND_ROBIN",
					Protocol:      peerProto,
					Tunnel:        &stnrv2.TunnelConfig{URL: tunnelURL, Auth: &serverAuth},
				}},
			}), "downstream server started")

			buf := make([]byte, 1600)
			if tc.clientProto == "UDP" {
				client, err := tn.client.Dial("udp", net.JoinHostPort(tn.host, "23478"))
				require.NoError(t, err, "client dial")
				defer client.Close() //nolint:errcheck

				// retry the first round-trip: datagrams may be dropped while the
				// upstream permission is negotiated
				echoed := false
				for i := 0; i < 20 && !echoed; i++ {
					_, err = client.Write([]byte("Hello"))
					require.NoError(t, err, "client write")
					require.NoError(t, client.SetReadDeadline(time.Now().Add(250*time.Millisecond)))
					n, err2 := client.Read(buf)
					if err2 != nil {
						continue
					}
					assert.Equal(t, "Hello", string(buf[:n]), "echo payload")
					echoed = true
				}
				require.True(t, echoed, "raw echo through the tunnel chain")

				// a couple more round-trips over the established flow
				require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
				for _, msg := range []string{"Hello-0", "Hello-1", "Hello-2"} {
					_, err = client.Write([]byte(msg))
					require.NoError(t, err, "client write")
					n, err := client.Read(buf)
					require.NoError(t, err, "echo")
					assert.Equal(t, msg, string(buf[:n]), "echo payload")
				}
			} else {
				client, err := net.Dial("tcp", "127.0.0.1:23478")
				require.NoError(t, err, "client dial")
				defer client.Close() //nolint:errcheck

				// TCP buffers the client side, so one write suffices; wait out the
				// upstream session setup with a generous deadline
				require.NoError(t, client.SetReadDeadline(time.Now().Add(10*time.Second)))
				for _, msg := range []string{"Hello-0", "Hello-1", "Hello-2"} {
					_, err = client.Write([]byte(msg))
					require.NoError(t, err, "client write")
					n, err := client.Read(buf)
					require.NoError(t, err, "echo")
					assert.Equal(t, msg, string(buf[:n]), "echo payload")
				}
			}

			// the flow rides an upstream allocation and counts as a downstream flow
			us := upstream.GetServer("upstream")
			require.NotNil(t, us, "upstream server")
			assert.Equal(t, 1, us.Sessions(), "upstream allocation count")
			ds := downstream.GetServer("downstream")
			require.NotNil(t, ds, "downstream server")
			assert.Equal(t, 1, ds.Sessions(), "downstream flow count")
		})
	}
}
