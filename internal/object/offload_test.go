package object_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/offload"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server/l4"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/utils/turnclient"
)

func TestOffloadObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "offload",
		setup: func(t *testing.T) (runtime.Object, stnrv2.Config, *stnrv2.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewOffload(nil, env.rt)
			require.NoError(t, err)
			return obj, &object.OffloadConfig{Engine: stnrv2.OffloadEngineNone.String()}, &stnrv2.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{name: "engine-change-restart", conf: &object.OffloadConfig{Engine: stnrv2.OffloadEngineAuto.String()}, want: runtime.ActionRestart},
			{name: "interfaces-change-restart", conf: &object.OffloadConfig{Engine: stnrv2.OffloadEngineNone.String(), Interfaces: []string{"eth0"}}, want: runtime.ActionRestart},
			{name: "same-config-none", conf: &object.OffloadConfig{Engine: stnrv2.OffloadEngineNone.String()}, want: runtime.ActionNone},
		},
	})
}

func TestOffloadWithInterfacesObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "offload-with-interfaces",
		setup: func(t *testing.T) (runtime.Object, stnrv2.Config, *stnrv2.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewOffload(nil, env.rt)
			require.NoError(t, err)
			return obj, &object.OffloadConfig{Engine: stnrv2.OffloadEngineNone.String(), Interfaces: []string{"eth0"}}, &stnrv2.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{name: "same-config-none", conf: &object.OffloadConfig{Engine: stnrv2.OffloadEngineNone.String(), Interfaces: []string{"eth0"}}, want: runtime.ActionNone},
			{name: "engine-change-restart", conf: &object.OffloadConfig{Engine: stnrv2.OffloadEngineAuto.String(), Interfaces: []string{"eth0"}}, want: runtime.ActionRestart},
			{name: "interfaces-change-restart", conf: &object.OffloadConfig{Engine: stnrv2.OffloadEngineNone.String(), Interfaces: []string{"eth1"}}, want: runtime.ActionRestart},
		},
	})
}

// recordingEngine records the offloads registered with it, as listener/cluster.
type recordingEngine struct {
	offload.NullEngine
	mu      sync.Mutex
	upserts []string
}

func (e *recordingEngine) Upsert(_, _ offload.Connection, listener, cluster string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.upserts = append(e.upserts, listener+"/"+cluster)
	return nil
}

// registered returns the offloads registered for a listener.
func (e *recordingEngine) registered(listener string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	ret := []string{}
	for _, u := range e.upserts {
		if strings.HasPrefix(u, listener+"/") {
			ret = append(ret, u)
		}
	}
	return ret
}

// TestOffloadMatrix drives a session through every server type, cluster protocol and client
// transport, and checks which ones are registered with the offload engine against the table of
// docs/cmd/stunnerd.md. The engines accelerate exactly one leg shape, plaintext ChannelData on
// one side and raw datagrams on the other, which leaves two mirror-image cases: a TURN server
// relaying a plain UDP client through a plain UDP cluster, and an L4 server tunnelling a plain UDP
// client through a TURN-UDP cluster.
func TestOffloadMatrix(t *testing.T) {
	// the L4 server learns the upstream channel asynchronously; a short backoff lets a rejected
	// registration be told from a slow one without waiting out the real pacing
	backoff := l4.ChannelPollBackoff
	l4.ChannelPollBackoff = 5 * time.Millisecond
	t.Cleanup(func() { l4.ChannelPollBackoff = backoff })

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
	cert := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	key := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))

	clients := [4]stnrv2.Protocol{stnrv2.ProtocolUDP, stnrv2.ProtocolTCP, stnrv2.ProtocolTLS, stnrv2.ProtocolDTLS}
	matrix := []struct {
		server  stnrv2.ServerType
		cluster stnrv2.Protocol
		want    [4]bool // per client transport: UDP, TCP, TLS, DTLS
	}{
		{stnrv2.ServerTypeTURN, stnrv2.ProtocolUDP, [4]bool{true, false, false, false}},
		{stnrv2.ServerTypeTURN, stnrv2.ProtocolTCP, [4]bool{false, false, false, false}},
		{stnrv2.ServerTypeTURN, stnrv2.ProtocolTURNUDP, [4]bool{false, false, false, false}},
		{stnrv2.ServerTypeTURN, stnrv2.ProtocolTURNTCP, [4]bool{false, false, false, false}},
		{stnrv2.ServerTypeTURN, stnrv2.ProtocolTURNTLS, [4]bool{false, false, false, false}},
		{stnrv2.ServerTypeTURN, stnrv2.ProtocolTURNDTLS, [4]bool{false, false, false, false}},
		{stnrv2.ServerTypeL4, stnrv2.ProtocolUDP, [4]bool{false, false, false, false}},
		{stnrv2.ServerTypeL4, stnrv2.ProtocolTCP, [4]bool{false, false, false, false}},
		{stnrv2.ServerTypeL4, stnrv2.ProtocolTURNUDP, [4]bool{true, false, false, false}},
		{stnrv2.ServerTypeL4, stnrv2.ProtocolTURNTCP, [4]bool{false, false, false, false}},
		{stnrv2.ServerTypeL4, stnrv2.ProtocolTURNTLS, [4]bool{false, false, false, false}},
		{stnrv2.ServerTypeL4, stnrv2.ProtocolTURNDTLS, [4]bool{false, false, false, false}},
	}
	// the TURN transport over a listener protocol, and back
	turnOver := map[stnrv2.Protocol]stnrv2.Protocol{
		stnrv2.ProtocolUDP: stnrv2.ProtocolTURNUDP, stnrv2.ProtocolTCP: stnrv2.ProtocolTURNTCP,
		stnrv2.ProtocolTLS: stnrv2.ProtocolTURNTLS, stnrv2.ProtocolDTLS: stnrv2.ProtocolTURNDTLS,
	}
	listenerOf := map[stnrv2.Protocol]stnrv2.Protocol{}
	for l, tp := range turnOver {
		listenerOf[tp] = l
	}

	// listen starts a listener of a protocol feeding a server and returns its address.
	listen := func(t *testing.T, env *testEnv, srv runtime.Object, name string, proto stnrv2.Protocol) string {
		t.Helper()
		network := "tcp"
		if proto == stnrv2.ProtocolUDP || proto == stnrv2.ProtocolDTLS {
			network = "udp"
		}
		conf := &stnrv2.ListenerConfig{Name: name, Protocol: proto.String(), Servers: []string{srv.Name()},
			Addr: "127.0.0.1", Port: freePort(t, network)}
		if proto == stnrv2.ProtocolTLS || proto == stnrv2.ProtocolDTLS {
			conf.Cert, conf.Key = cert, key
		}
		l, err := object.NewListener(conf, env.rt)
		require.NoError(t, err)
		env.start(t, l, nil)
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(conf.Port))
	}
	// echo writes to a conn until the peer's echo comes back.
	echo := func(t *testing.T, c net.Conn) {
		t.Helper()
		require.Eventually(t, func() bool {
			if _, err := c.Write([]byte("hello")); err != nil {
				return false
			}
			_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			buf := make([]byte, 1500)
			n, err := c.Read(buf)
			return err == nil && string(buf[:n]) == "hello"
		}, 5*time.Second, 10*time.Millisecond)
	}

	for _, row := range matrix {
		for i, client := range clients {
			want := row.want[i]
			name := fmt.Sprintf("%s-server/%s-cluster/%s-client", row.server.String(),
				row.cluster.String(), client.String())
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				eng := &recordingEngine{}
				env := newLiveEnv(t, eng)

				udpPeer, err := net.ListenPacket("udp4", "127.0.0.1:0") //nolint:noctx
				require.NoError(t, err)
				t.Cleanup(func() { _ = udpPeer.Close() })
				go func() {
					buf := make([]byte, 1500)
					for {
						n, from, err := udpPeer.ReadFrom(buf)
						if err != nil {
							return
						}
						_, _ = udpPeer.WriteTo(buf[:n], from)
					}
				}()
				tcpPeer, err := net.Listen("tcp4", "127.0.0.1:0") //nolint:noctx
				require.NoError(t, err)
				t.Cleanup(func() { _ = tcpPeer.Close() })
				go func() {
					for {
						c, err := tcpPeer.Accept()
						if err != nil {
							return
						}
						go func() { _, _ = io.Copy(c, c) }()
					}
				}()

				// a TURN server admits the loopback peers, an L4 server forwards to the
				// peer of the cluster's network
				eps := []string{"127.0.0.0/8"}
				if row.server == stnrv2.ServerTypeL4 {
					eps = []string{udpPeer.LocalAddr().String()}
					if row.cluster == stnrv2.ProtocolTCP {
						eps = []string{tcpPeer.Addr().String()}
					}
				}
				// a TURN-* column is a UDP cluster tunnelled over that TURN transport
				cconf := &stnrv2.ClusterConfig{Name: "peer", Endpoints: eps,
					Protocol: row.cluster.String(), Addrs: []string{"127.0.0.1"}}
				if row.server == stnrv2.ServerTypeL4 {
					cconf.RoutingPolicy = stnrv2.RoutingPolicyRoundRobin.String()
				}
				if up, ok := listenerOf[row.cluster]; ok {
					// the upstream TURN server of the tunnel relays to the peers through a
					// UDP cluster
					upCluster, err := object.NewCluster(&stnrv2.ClusterConfig{Name: "upstream-peer",
						Endpoints: []string{"127.0.0.0/8"}, Protocol: "UDP",
						Addrs: []string{"127.0.0.1"}}, env.rt)
					require.NoError(t, err)
					env.start(t, upCluster, nil)
					upServer, err := object.NewServer(&stnrv2.ServerConfig{Name: "upstream",
						Clusters: []string{"upstream-peer"}}, env.rt)
					require.NoError(t, err)
					env.start(t, upServer, nil)
					addr := listen(t, env, upServer, "upstream", up)
					_, port, _ := net.SplitHostPort(addr)
					p, _ := strconv.Atoi(port)
					cconf.Protocol = stnrv2.ProtocolUDP.String()
					cconf.Tunnel = &stnrv2.TunnelConfig{
						URL:  (&stnrv2.URI{Protocol: row.cluster, Host: "127.0.0.1", Port: p}).String(),
						Auth: staticAuthConfig(), Insecure: true}
				}
				cl, err := object.NewCluster(cconf, env.rt)
				require.NoError(t, err)
				env.start(t, cl, nil)

				srv, err := object.NewServer(&stnrv2.ServerConfig{Name: "server",
					Type: row.server.String(), Clusters: []string{"peer"}}, env.rt)
				require.NoError(t, err)
				env.start(t, srv, nil)
				addr := listen(t, env, srv, "client", client)

				switch {
				case row.server == stnrv2.ServerTypeTURN && row.cluster == stnrv2.ProtocolTCP:
					// a TCP cluster makes no relay sockets, only RFC 6062 relayed
					// connections, which a stream client opens and which carry no
					// channel
					d := turnclient.Dialer{Config: turnclient.Config{Protocol: turnOver[client],
						ServerAddr: addr, Username: "user", Password: "pass",
						Realm: stnrv2.DefaultRealm, Insecure: true}}
					if client == stnrv2.ProtocolUDP || client == stnrv2.ProtocolDTLS {
						_, err := d.ListenPacket(context.Background(), "udp", "")
						require.Error(t, err, "no datagram relaying")
						break
					}
					if client == stnrv2.ProtocolTLS {
						t.Skip("turnclient cannot open an RFC 6062 data connection over TLS: " +
							"pion's DialWithConn takes a transport.TCPConn only")
					}
					c, err := d.DialContext(context.Background(), "tcp", tcpPeer.Addr().String())
					require.NoError(t, err)
					t.Cleanup(func() { _ = c.Close() })
					echo(t, c)

				case row.server == stnrv2.ServerTypeTURN:
					d := turnclient.Dialer{Config: turnclient.Config{Protocol: turnOver[client],
						ServerAddr: addr, Username: "user", Password: "pass",
						Realm: stnrv2.DefaultRealm, Insecure: true}}
					c, err := d.ListenPacket(context.Background(), "udp", "")
					require.NoError(t, err)
					t.Cleanup(func() { _ = c.Close() })
					peer := udpPeer.LocalAddr()
					require.Eventually(t, func() bool {
						if _, err := c.WriteTo([]byte("hello"), peer); err != nil {
							return false
						}
						_ = c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
						buf := make([]byte, 1500)
						n, _, err := c.ReadFrom(buf)
						// the channel is bound once the client frames with it
						_, bound := c.Channel(peer)
						return err == nil && string(buf[:n]) == "hello" && bound
					}, 5*time.Second, 10*time.Millisecond)

				default:
					var c net.Conn
					switch client {
					case stnrv2.ProtocolUDP, stnrv2.ProtocolTCP:
						c, err = net.Dial(strings.ToLower(client.String()), addr) //nolint:noctx
					case stnrv2.ProtocolTLS:
						c, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec,noctx
					case stnrv2.ProtocolDTLS:
						var sock *net.UDPConn
						sock, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
						require.NoError(t, err)
						raddr, _ := net.ResolveUDPAddr("udp", addr)
						c, err = dtls.ClientWithOptions(sock, raddr, dtls.WithInsecureSkipVerify(true))
					}
					require.NoError(t, err)
					t.Cleanup(func() { _ = c.Close() })
					echo(t, c)
				}

				if want {
					require.Eventually(t, func() bool {
						return len(eng.registered("client")) > 0
					}, 5*time.Second, 10*time.Millisecond, "offload registered")
					// the peer side is attributed to the cluster
					assert.Equal(t, []string{"client/peer"}, eng.registered("client"))
					return
				}
				// the channel is bound and the L4 channel probe has run its course several
				// times over by now, so an empty engine means the rule rejected the session
				assert.Never(t, func() bool { return len(eng.registered("client")) > 0 },
					300*time.Millisecond, 10*time.Millisecond, "no offload registered")
			})
		}
	}
}
