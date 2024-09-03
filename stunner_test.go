package stunner

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/logging"
	"github.com/pion/transport/v3"
	"github.com/pion/transport/v3/stdnet"
	"github.com/pion/transport/v3/test"
	"github.com/pion/transport/v3/vnet"
	"github.com/pion/turn/v4"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"

	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/pkg/logger"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	stnrv1a1 "github.com/l7mp/stunner/pkg/apis/v1alpha1"
	a12n "github.com/l7mp/stunner/pkg/authentication"
	cfgclient "github.com/l7mp/stunner/pkg/config/client"
)

var stunnerTestLoglevel string = "all:ERROR"

// var stunnerTestLoglevel string = stnrv1.DefaultLogLevel
// var stunnerTestLoglevel string = "all:INFO"
// var stunnerTestLoglevel string = "all:TRACE"
// var stunnerTestLoglevel string = "all:TRACE,vnet:INFO,turn:ERROR,turnc:ERROR"

var certPem, keyPem, _ = GenerateSelfSignedKey()
var certPem64 = base64.StdEncoding.EncodeToString(certPem)
var keyPem64 = base64.StdEncoding.EncodeToString(keyPem)

/********************************************
 *
 * test lib
 *
 *********************************************/
type echoTestConfig struct {
	t *testing.T
	// net
	podnet, wan transport.Net
	// server
	stunner     *Stunner
	stunnerAddr string
	// client
	lconn      net.PacketConn
	user, pass string
	natAddr    net.IP
	// echo
	echoServerAddr                            string
	allocateSuccess, bindSuccess, echoSuccess bool
	loggerFactory                             logging.LoggerFactory
}

// type bundle struct {
// 	addr net.Addr
// 	err  error
// }

// func bindingRequestWithTimeout(client *turn.Client, timeout time.Duration) (net.Addr, error){
//     res := make(chan bundle, 1)
//     go func() {
//             addr, err := client.SendBindingRequest()
//             res <- bundle{addr, err}
//     }()
//     select {
//     case <-time.After(timeout):
//         return nil, fmt.Errorf("timeout")
//     case result := <-res:
//         return result.addr, result.err
//     }
// }

func stunnerEchoTest(conf echoTestConfig) {
	t := conf.t
	log := conf.loggerFactory.NewLogger("test")

	client, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: conf.stunnerAddr,
		TURNServerAddr: conf.stunnerAddr,
		Username:       conf.user,
		Password:       conf.pass,
		Conn:           conf.lconn,
		Net:            conf.wan,
		LoggerFactory:  conf.loggerFactory,
	})

	assert.NoError(t, err, "cannot create TURN client")
	assert.NoError(t, client.Listen(), "cannot listen on TURN client")
	defer client.Close()

	log.Debug("sending a binding request")
	// reflAddr, err := bindingRequestWithTimeout(client, 10000 * time.Millisecond)
	reflAddr, err := client.SendBindingRequest()
	if conf.bindSuccess == false {
		assert.Error(t, err, "binding request failed")
	} else {
		assert.NoError(t, err, "binding request ok")
		log.Debugf("mapped-address: %v", reflAddr.String())
		udpAddr := reflAddr.(*net.UDPAddr)

		// The mapped-address should have IP address that was assigned to the LAN router.
		assert.True(t, udpAddr.IP.Equal(conf.natAddr), "wrong srfx address")

		log.Debug("sending an allocate request")
		conn, err := client.Allocate()
		if conf.allocateSuccess == false {
			assert.Error(t, err, err)
		} else {
			assert.NoError(t, err, err)

			// log.Debugf("laddr: %s", conn.LocalAddr().String())

			log.Debugf("creating echo-server listener socket at: %s", conn.LocalAddr().String())
			echoConn, err := conf.podnet.ListenPacket("udp4", conf.echoServerAddr)
			assert.NoError(t, err, "creating echo socket")

			// assert.NotNil(t, err, "echo socket not nil")

			go func() {
				buf := make([]byte, 1600)
				for {
					n, from, err2 := echoConn.ReadFrom(buf)
					if err2 != nil {
						break
					}

					// verify the message was received from the relay address
					assert.Equal(t, conn.LocalAddr().String(), from.String(),
						"message should be received from the relay address")
					assert.Equal(t, "Hello", string(buf[:n]), "wrong message payload")

					// echo the data
					_, err2 = echoConn.WriteTo(buf[:n], from)
					assert.NoError(t, err2, err2)
				}
			}()

			if conf.echoSuccess == true {
				buf := make([]byte, 1600)
				for i := 0; i < 8; i++ {
					log.Debug("sending \"Hello\"")
					_, err = conn.WriteTo([]byte("Hello"), echoConn.LocalAddr())
					assert.NoError(t, err, err)

					n, from, err2 := conn.ReadFrom(buf)
					assert.NoError(t, err2, err2)
					assert.Equal(t, n, len("Hello"), "message OK")
					assert.Equal(t, []byte("Hello"), buf[:n], "message OK")

					// verify the message was received from the relay address
					assert.Equal(t, echoConn.LocalAddr().String(), from.String(),
						"message should be received from the relay address")

					time.Sleep(100 * time.Millisecond)
				}
			} else {
				// should fail
				_, err = conn.WriteTo([]byte("Hello"), echoConn.LocalAddr())
				assert.Errorf(t, err, "got error message %s", err)
			}
			assert.NoError(t, conn.Close(), "cannot close relay connection")
			assert.NoError(t, echoConn.Close(), "cannot close echo server connection")
		}
	}
	time.Sleep(150 * time.Millisecond)
	client.Close()
}

// *****************
// NAT traversal tests with VNet: VNet supports UDP only, TCP tests will run on the localhost
// *****************
// Topology:
//        	       	    lan
//              wan       /----- STUNner
//     client ------ gw-nat
//                        \----- echo-server
//

type VNet struct {
	gw     *vnet.Router // kube-proxy
	podnet *vnet.Net    // k8s pod network
	wan    *vnet.Net    // external network
}

func (v *VNet) Close() error {
	return v.gw.Stop()
}

func buildVNet(logger logging.LoggerFactory) (*VNet, error) {
	gw, err := vnet.NewRouter(&vnet.RouterConfig{
		Name:          "gw",
		CIDR:          "0.0.0.0/0",
		LoggerFactory: logger,
	})
	if err != nil {
		return nil, err
	}

	// client side
	podnet, _ := vnet.NewNet(&vnet.NetConfig{StaticIPs: []string{"1.2.3.4", "1.2.3.5", "1.2.3.10"}})
	err = gw.AddNet(podnet)
	if err != nil {
		return nil, err
	}

	// LAN
	nat, err := vnet.NewRouter(&vnet.RouterConfig{
		Name:      "lan",
		StaticIPs: []string{"5.6.7.8"}, // this router's external IP on eth0
		CIDR:      "192.168.0.0/24",
		NATType: &vnet.NATType{
			MappingBehavior:   vnet.EndpointIndependent,
			FilteringBehavior: vnet.EndpointIndependent,
		},
		LoggerFactory: logger,
	})
	if err != nil {
		return nil, err
	}

	wan, _ := vnet.NewNet(&vnet.NetConfig{})
	if err = nat.AddNet(wan); err != nil {
		return nil, err
	}
	if err = gw.AddRouter(nat); err != nil {
		return nil, err
	}
	if err = gw.Start(); err != nil {
		return nil, err
	}

	// register host names
	_ = gw.AddHost("stunner.l7mp.io", "1.2.3.4")
	_ = gw.AddHost("echo-server.l7mp.io", "1.2.3.5")
	_ = gw.AddHost("dummy.l7mp.io", "1.2.3.10")

	return &VNet{
		gw:     gw,
		podnet: podnet,
		wan:    wan,
	}, nil
}

/********************************************
 *
 *  UDP/TCP/TLS/DTLS tests over localhost (VNet supports UDP only)
 *  *****************
 *  Topology:
 *                 /----- STUNner (udp/tcp/tls/dtls:23478)
 *      client--- lo
 *                 \----- echo-server (udp:25678)
 *
 *********************************************/

type TestStunnerConfigCase struct {
	config stnrv1.StunnerConfig
	uri    string
}

var TestStunnerConfigsWithLocalhost = []TestStunnerConfigCase{
	{
		config: stnrv1.StunnerConfig{
			// udp, static
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:       "udp",
				Protocol:   "turn-udp",
				Addr:       "127.0.0.1",
				Port:       23478,
				PublicAddr: "1.2.3.4",
				PublicPort: 3478,
				Routes:     []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		uri: "turn:1.2.3.4:3478?transport=udp",
	},
	{
		config: stnrv1.StunnerConfig{
			// udp, ephemeral
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "ephemeral",
				Credentials: map[string]string{
					"secret": "my-secret",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:       "udp",
				Protocol:   "turn-udp",
				Addr:       "127.0.0.1",
				Port:       23478,
				PublicAddr: "1.2.3.4",
				PublicPort: 3478,
				Routes:     []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		uri: "turn:1.2.3.4:3478?transport=udp",
	},
	{
		config: stnrv1.StunnerConfig{
			// tcp, static
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:       "tcp",
				Protocol:   "turn-tcp",
				Addr:       "127.0.0.1",
				Port:       23478,
				PublicAddr: "1.2.3.4",
				PublicPort: 3478,
				Routes:     []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		uri: "turn:1.2.3.4:3478?transport=tcp",
	},
	{
		config: stnrv1.StunnerConfig{
			// tcp, ephemeral
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "ephemeral",
				Credentials: map[string]string{
					"secret": "my-secret",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:       "tcp",
				Protocol:   "turn-tcp",
				Addr:       "127.0.0.1",
				Port:       23478,
				PublicAddr: "1.2.3.4",
				PublicPort: 3478,
				Routes:     []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		uri: "turn:1.2.3.4:3478?transport=tcp",
	},
	{
		config: stnrv1.StunnerConfig{
			// tls, static
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:       "tls",
				Protocol:   "turn-tls",
				Addr:       "127.0.0.1",
				PublicAddr: "1.2.3.4",
				PublicPort: 3478,
				Port:       23478,
				Cert:       certPem64,
				Key:        keyPem64,
				Routes:     []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		uri: "turns:1.2.3.4:3478?transport=tcp",
	},
	{
		config: stnrv1.StunnerConfig{
			// tls, ephemeral
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "ephemeral",
				Credentials: map[string]string{
					"secret": "my-secret",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:       "tls",
				Protocol:   "turn-tls",
				Addr:       "127.0.0.1",
				Port:       23478,
				PublicAddr: "1.2.3.4",
				PublicPort: 3478,
				Cert:       certPem64,
				Key:        keyPem64,
				Routes:     []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		uri: "turns:1.2.3.4:3478?transport=tcp",
	},
	{
		config: stnrv1.StunnerConfig{
			// dtls, static
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:       "dtls",
				Protocol:   "turn-dtls",
				Addr:       "127.0.0.1",
				PublicAddr: "1.2.3.4",
				PublicPort: 3478,
				Port:       23478,
				Cert:       certPem64,
				Key:        keyPem64,
				Routes:     []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		uri: "turns:1.2.3.4:3478?transport=udp",
	},
	// // dtls, ephemeral
	// {
	// 	ApiVersion: stnrv1.ApiVersion,
	// 	Admin: stnrv1.AdminConfig{
	// 		LogLevel: stunnerTestLoglevel,
	// 	},
	// 	Auth: stnrv1.AuthConfig{
	// 		Type: "ephemeral",
	// 		Credentials: map[string]string{
	// 			"secret": "my-secret",
	// 		},
	// 	},
	// 	Listeners: []stnrv1.ListenerConfig{{
	// 		Name:     "dtls",
	// 		Protocol: "turn-dtls",
	// 		Addr:     "127.0.0.1",
	// 		Port:     23478,
	// 		Routes:   []string{"allow-any"},
	// 	}},
	// 	Clusters: []stnrv1.ClusterConfig{{
	// 		Name:      "allow-any",
	// 		Endpoints: []string{"0.0.0.0/0"},
	// 	}},
	// },
}

func TestStunnerServerLocalhost(t *testing.T) {
	testStunnerLocalhost(t, 1, TestStunnerConfigsWithLocalhost)
}

func testStunnerLocalhost(t *testing.T, udpThreadNum int, tests []TestStunnerConfigCase) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	// loggerFactory := logger.NewLoggerFactory("all:TRACE")
	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	// assert.NoError(t, err, "cannot generate SSL SSL cert/key")

	for _, test := range tests {
		c := test.config
		auth := c.Auth.Type
		proto := c.Listeners[0].Protocol
		testName := fmt.Sprintf("TestStunner_NewStunner_Localhost_auth:%s_client:%s", auth, proto)

		t.Run(testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", testName)

			log.Debug("testing TURN URI")
			uri, err := GetUriFromListener(&c.Listeners[0])
			assert.NoError(t, err, "GetUriFromListener")
			assert.Equal(t, test.uri, uri, "listener uri")

			log.Debug("creating a stunnerd")
			stunner := NewStunner(Options{
				LogLevel:             stunnerTestLoglevel,
				SuppressRollback:     true,
				UDPListenerThreadNum: udpThreadNum,
			})

			assert.False(t, stunner.shutdown, "lifecycle 1: alive")
			// HACK!
			assert.True(t, stunner.ready, "lifecycle 1: not-ready")
			// assert.False(t, stunner.ready, "lifecycle 1: not-ready")
			assert.True(t, stunner.IsReady(), "lifecycle 1: not-ready")
			// assert.False(t, stunner.IsReady(), "lifecycle 1: not-ready")

			log.Debug("starting stunnerd")
			assert.NoError(t, stunner.Reconcile(&c), "starting server")

			assert.False(t, stunner.shutdown, "lifecycle 2: alive")
			assert.True(t, stunner.ready, "lifecycle 2: ready")
			assert.True(t, stunner.IsReady(), "lifecycle 2: ready")

			var u, p string
			switch auth {
			case "plaintext", "static":
				u = "user1"
				p = "passwd1"
			case "longterm", "ephemeral":
				u = a12n.GenerateTimeWindowedUsername(time.Now(), time.Minute, "")
				p2, err := a12n.GetLongTermCredential(u, "my-secret")
				assert.NoError(t, err, err)
				p = p2
			default:
				assert.NoError(t, fmt.Errorf("internal error: unknown auth type in test"))
			}

			stunnerAddr := "127.0.0.1:23478"

			log.Debug("creating a client")
			var lconn net.PacketConn
			switch proto {
			case "turn-udp":
				lconn, err = net.ListenPacket("udp", "0.0.0.0:0")
				assert.NoError(t, err, "cannot create UDP client socket")
			case "turn-tcp":
				conn, cErr := net.Dial("tcp", stunnerAddr)
				assert.NoError(t, cErr, "cannot create TCP client socket")
				lconn = turn.NewSTUNConn(conn)
			case "turn-tls":
				cer, err := tls.X509KeyPair(certPem, keyPem)
				assert.NoError(t, err, "cannot create certificate for TLS client socket")
				conn, err := tls.Dial("tcp", stunnerAddr, &tls.Config{
					MinVersion:         tls.VersionTLS12,
					Certificates:       []tls.Certificate{cer},
					InsecureSkipVerify: true,
				})
				assert.NoError(t, err, "cannot create TLS client socket")
				lconn = turn.NewSTUNConn(conn)
			case "turn-dtls":
				cer, err := tls.X509KeyPair(certPem, keyPem)
				assert.NoError(t, err, "cannot create certificate for DTLS client socket")
				// for some reason dtls.Listen requires a UDPAddr and not an addr string
				udpAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 23478}
				conn, err := dtls.Dial("udp", udpAddr, &dtls.Config{
					Certificates:       []tls.Certificate{cer},
					InsecureSkipVerify: true,
				})
				assert.NoError(t, err, "cannot create DTLS client socket")
				lconn = turn.NewSTUNConn(conn)
			default:
				assert.NoError(t, fmt.Errorf("internal error: unknown client protocol in test"))
			}

			stdnet, _ := stdnet.NewNet()
			testConfig := echoTestConfig{t, stdnet, stdnet, stunner,
				stunnerAddr, lconn, u, p, net.IPv4(127, 0, 0, 1),
				"127.0.0.1:25678", true, true, true, loggerFactory}
			stunnerEchoTest(testConfig)

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")

			assert.False(t, stunner.shutdown, "lifecycle 3: alive")
			assert.True(t, stunner.ready, "lifecycle 3: ready")
			assert.True(t, stunner.IsReady(), "lifecycle 3: ready")

			stunner.Shutdown()

			assert.True(t, stunner.shutdown, "lifecycle 4: shutting down")
			assert.False(t, stunner.ready, "lifecycle 4: not-ready")
			assert.False(t, stunner.IsReady(), "lifecycle 4: not-ready")

			stunner.Close()

			assert.True(t, stunner.shutdown, "lifecycle 3: shutting down")
			assert.False(t, stunner.ready, "lifecycle 3: not-ready")
			assert.False(t, stunner.IsReady(), "lifecycle 3: not-ready")
		})
	}
}

// *****************
// Cluster tests with VNet
// *****************
//
//	type StunnerClusterConfig struct {
//	        config stnrv1.StunnerConfig
//	        echoServerAddr string
//	        result bool
//	}
type StunnerTestClusterConfig struct {
	testName       string
	config         stnrv1.StunnerConfig
	echoServerAddr string
	result         bool
	tester         func(t *testing.T)
}

var testClusterConfigsWithVNet = []StunnerTestClusterConfig{
	{
		testName: "open ok",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes:   []string{"echo-server-cluster"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STATIC",
				Endpoints: []string{
					"1.2.3.5",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
	{
		testName: "default cluster type static ok",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Endpoints: []string{
					"1.2.3.5",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
	{
		testName: "static endpoint ok",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STATIC",
				Endpoints: []string{
					"1.2.3.5",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
	{
		testName: "static endpoint with wrong peer addr: fail",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STATIC",
				Endpoints: []string{
					"1.2.3.6",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         false,
	},
	{
		testName: "static endpoint with multiple routes ok",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
					"dummy_cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STATIC",
				Endpoints: []string{
					"1.2.3.5",
				},
			}, {
				Name: "dummy_cluster",
				Type: "STATIC",
				Endpoints: []string{
					"9.8.7.6",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
	{
		testName: "static endpoint with multiple routes and wrong peer addr fail",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"dummy_cluster",
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STATIC",
				Endpoints: []string{
					"1.2.3.6",
				},
			}, {
				Name: "dummy_cluster",
				Type: "STATIC",
				Endpoints: []string{
					"9.8.7.6",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         false,
	},
	{
		testName: "static endpoint with multiple ips ok",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STATIC",
				Endpoints: []string{
					"1.2.3.1",
					"1.2.3.2",
					"1.2.3.3",
					"1.2.3.5",
					"1.2.3.6",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
	{
		testName: "static endpoint with multiple ips with wrong peer addr fail",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STATIC",
				Endpoints: []string{
					"1.2.3.1",
					"1.2.3.2",
					"1.2.3.3",
					"1.2.3.6",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         false,
	},
	{
		testName: "strict_dns ok",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STRICT_DNS",
				Endpoints: []string{
					"echo-server.l7mp.io",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
	{
		testName: "strict_dns cluster and wrong peer addr fail",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STRICT_DNS",
				Endpoints: []string{
					"echo-server.l7mp.io",
				},
			}},
		},
		echoServerAddr: "1.2.3.10:5678",
		result:         false,
	},
	{
		testName: "strict_dns cluster with multiple domains ok",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STRICT_DNS",
				Endpoints: []string{
					"stunner.l7mp.io",
					"echo-server.l7mp.io",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
	{
		testName: "multiple strict_dns clusters ok",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"stunner-cluster",
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "stunner-cluster",
				Type: "STRICT_DNS",
				Endpoints: []string{
					"stunner.l7mp.io",
				},
			}, {
				Name: "echo-server-cluster",
				Type: "STRICT_DNS",
				Endpoints: []string{
					"echo-server.l7mp.io",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
}

func TestStunnerClusterWithVNet(t *testing.T) {
	lim := test.TimeOut(time.Second * 60)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, c := range testClusterConfigsWithVNet {
		t.Run(c.testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", c.testName)

			// patch in the vnet
			log.Debug("building virtual network")
			v, err := buildVNet(loggerFactory)
			assert.NoError(t, err, err)

			log.Debug("setting up the mock DNS")
			mockDns := resolver.NewMockResolver(map[string]([]string){
				"stunner.l7mp.io":     []string{"1.2.3.4"},
				"echo-server.l7mp.io": []string{"1.2.3.5"},
				"dummy.l7mp.io":       []string{"1.2.3.10"},
			}, loggerFactory)

			log.Debug("creating a stunnerd")
			stunner := NewStunner(Options{
				LogLevel:         stunnerTestLoglevel,
				SuppressRollback: true,
				Resolver:         mockDns,
				Net:              v.podnet,
			})

			log.Debug("starting stunnerd")
			assert.NoError(t, stunner.Reconcile(&c.config), "starting server")

			var u, p string
			auth := c.config.Auth.Type
			switch auth {
			case "plaintext", "static":
				u = "user1"
				p = "passwd1"
			case "longterm", "ephemeral":
				u, p, err = turn.GenerateLongTermCredentials("my-secret", time.Minute)
				assert.NoError(t, err, err)
			default:
				assert.NoError(t, fmt.Errorf("internal error: unknown auth type in test"))
			}

			log.Debug("creating a client")
			lconn, err := v.wan.ListenPacket("udp4", "0.0.0.0:0")
			assert.NoError(t, err, "cannot create client listening socket")

			testConfig := echoTestConfig{t, v.podnet, v.wan, stunner,
				"stunner.l7mp.io:3478", lconn, u, p, net.IPv4(5, 6, 7, 8),
				c.echoServerAddr, true, true, c.result, loggerFactory}
			stunnerEchoTest(testConfig)

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")
			stunner.Close()
			assert.NoError(t, v.Close(), "cannot close VNet")
		})
	}
}

// *****************
// Port range filtering tests with VNet
// *****************
var testPortRangeConfigsWithVNet = []StunnerTestClusterConfig{
	{
		testName: "static endpoint with peer address in the admitted port range ok",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STATIC",
				Endpoints: []string{
					"1.2.3.5:<5670-5680>",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
		tester: func(t *testing.T) {
			c := telemetry.ListenerConnsTotal
			assert.Equal(t, 1, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.Equal(t, float64(1), testutil.ToFloat64(c.WithLabelValues("udp")))

			g := telemetry.ListenerConnsActive
			assert.Equal(t, 1, testutil.CollectAndCount(g), "ListenerConnsTotal")
			assert.Equal(t, float64(1), testutil.ToFloat64(g.WithLabelValues("udp")))

			c = telemetry.ListenerPacketsTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "rx")), float64(500))
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "tx")), float64(500))

			c = telemetry.ListenerBytesTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "rx")), float64(2000))
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "tx")), float64(2000))

			c = telemetry.ClusterPacketsTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "rx")), float64(500))
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "tx")), float64(500))

			c = telemetry.ClusterBytesTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "rx")), float64(2000))
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "tx")), float64(2000))
		},
	},
	{
		testName: "static endpoint with peer address matching singleton admitted port ok",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{
				{
					Name:      "dummy-cluster",
					Type:      "STATIC",
					Endpoints: []string{"1.2.3.6:<5678-5678>"},
				}, {
					Name:      "echo-server-cluster",
					Type:      "STATIC",
					Endpoints: []string{"1.2.3.5:<5678-5678>"},
				},
			},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
		tester: func(t *testing.T) {
			c := telemetry.ListenerConnsTotal
			assert.Equal(t, 1, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.Equal(t, float64(1), testutil.ToFloat64(c.WithLabelValues("udp")))

			g := telemetry.ListenerConnsActive
			assert.Equal(t, 1, testutil.CollectAndCount(g), "ListenerConnsTotal")
			assert.Equal(t, float64(1), testutil.ToFloat64(g.WithLabelValues("udp")))

			c = telemetry.ListenerPacketsTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "rx")), float64(500))
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "tx")), float64(500))

			c = telemetry.ListenerBytesTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "rx")), float64(2000))
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "tx")), float64(2000))

			c = telemetry.ClusterPacketsTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "rx")), float64(500))
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "tx")), float64(500))

			c = telemetry.ClusterBytesTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "rx")), float64(2000))
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "tx")), float64(2000))
		},
	},
	{
		testName: "static endpoint with peer address in rejected port range fails",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STATIC",
				Endpoints: []string{
					"1.2.3.5:<1-5677>",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         false,
		tester: func(t *testing.T) {
			c := telemetry.ListenerConnsTotal
			assert.Equal(t, 1, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.Equal(t, float64(1), testutil.ToFloat64(c.WithLabelValues("udp")))

			g := telemetry.ListenerConnsActive
			assert.Equal(t, 1, testutil.CollectAndCount(g), "ListenerConnsTotal")
			assert.Equal(t, float64(1), testutil.ToFloat64(g.WithLabelValues("udp")))

			c = telemetry.ListenerPacketsTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "rx")), float64(500)) // signaling+data
			assert.LessOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "tx")), float64(50))     // just signaling

			c = telemetry.ListenerBytesTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "rx")), float64(1000)) // signaling+data
			assert.LessOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "tx")), float64(1000))    // just signaling

			c = telemetry.ClusterPacketsTotal
			assert.Equal(t, 0, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.Equal(t, float64(0), testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "rx")))
			assert.Equal(t, float64(0), testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "tx")))

			c = telemetry.ClusterBytesTotal
			assert.Equal(t, 0, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.Equal(t, float64(0), testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "rx")))
			assert.Equal(t, float64(0), testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "tx")))
		},
	},
	{
		testName: "static endpoint with peer address in rejected singleton port fails",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name: "echo-server-cluster",
				Type: "STATIC",
				Endpoints: []string{
					"1.2.3.5:<5677-5677>",
				},
			}},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         false,
		tester: func(t *testing.T) {
			c := telemetry.ListenerConnsTotal
			assert.Equal(t, 1, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.Equal(t, float64(1), testutil.ToFloat64(c.WithLabelValues("udp")))

			g := telemetry.ListenerConnsActive
			assert.Equal(t, 1, testutil.CollectAndCount(g), "ListenerConnsTotal")
			assert.Equal(t, float64(1), testutil.ToFloat64(g.WithLabelValues("udp")))

			c = telemetry.ListenerPacketsTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "rx")), float64(500)) // signaling+data
			assert.LessOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "tx")), float64(50))     // just signaling

			c = telemetry.ListenerBytesTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "rx")), float64(1000)) // signaling+data
			assert.LessOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "tx")), float64(1000))    // just signaling

			c = telemetry.ClusterPacketsTotal
			assert.Equal(t, 0, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.Equal(t, float64(0), testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "rx")))
			assert.Equal(t, float64(0), testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "tx")))

			c = telemetry.ClusterBytesTotal
			assert.Equal(t, 0, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.Equal(t, float64(0), testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "rx")))
			assert.Equal(t, float64(0), testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "tx")))
		},
	},
	{
		testName: "strict_dns with default port range ok",
		config: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
				Type: "static",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv1.ListenerConfig{{
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []stnrv1.ClusterConfig{
				{
					Name:      "dummy-cluster",
					Type:      "STATIC",
					Endpoints: []string{"1.2.3.6"},
				}, {
					Name:      "echo-server-cluster",
					Type:      "STRICT_DNS",
					Endpoints: []string{"echo-server.l7mp.io"},
				},
			},
		},
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
		tester: func(t *testing.T) {
			c := telemetry.ListenerConnsTotal
			assert.Equal(t, 1, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.Equal(t, float64(1), testutil.ToFloat64(c.WithLabelValues("udp")))

			g := telemetry.ListenerConnsActive
			assert.Equal(t, 1, testutil.CollectAndCount(g), "ListenerConnsTotal")
			assert.Equal(t, float64(1), testutil.ToFloat64(g.WithLabelValues("udp")))

			c = telemetry.ListenerPacketsTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "rx")), float64(500))
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "tx")), float64(500))

			c = telemetry.ListenerBytesTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "rx")), float64(2000))
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "tx")), float64(2000))

			c = telemetry.ClusterPacketsTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "rx")), float64(500))
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "tx")), float64(500))

			c = telemetry.ClusterBytesTotal
			assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "rx")), float64(2000))
			assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "tx")), float64(2000))
		},
	},
	// TODO: implement port-range filtering for DNS clusters
	// {
	// 	testName: "strict_dns with prohibited port range fails",
	// 	config: stnrv1.StunnerConfig{
	// 		ApiVersion: stnrv1.ApiVersion,
	// 		Admin: stnrv1.AdminConfig{
	// 			LogLevel: stunnerTestLoglevel,
	// 		},
	// 		Auth: stnrv1.AuthConfig{
	// 			Type: "static",
	// 			Credentials: map[string]string{
	// 				"username": "user1",
	// 				"password": "passwd1",
	// 			},
	// 		},
	// 		Listeners: []stnrv1.ListenerConfig{{
	// 			Name:     "udp",
	// 			Protocol: "turn-udp",
	// 			Addr:     "1.2.3.4",
	// 			Port:     3478,
	// 			Routes: []string{
	// 				"echo-server-cluster",
	// 			},
	// 		}},
	// 		Clusters: []stnrv1.ClusterConfig{{
	// 			Name:         "echo-server-cluster",
	// 			Type:         "STRICT_DNS",
	// 			MinRelayPort: 1,
	// 			MaxRelayPort: 1,
	// 			Endpoints: []string{
	// 				"echo-server.l7mp.io",
	// 			},
	// 		}},
	// 	},
	// 	echoServerAddr: "1.2.3.5:5678",
	// 	result:         false,
	// 	tester: func(t *testing.T) {
	// 		c := telemetry.ListenerConnsTotal
	// 		assert.Equal(t, 1, testutil.CollectAndCount(c), "ListenerConnsTotal")
	// 		assert.Equal(t, float64(1), testutil.ToFloat64(c.WithLabelValues("udp")))

	// 		g := telemetry.ListenerConnsActive
	// 		assert.Equal(t, 1, testutil.CollectAndCount(g), "ListenerConnsTotal")
	// 		assert.Equal(t, float64(1), testutil.ToFloat64(g.WithLabelValues("udp")))

	// 		c = telemetry.ListenerPacketsTotal
	// 		assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
	// 		assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "rx")), float64(500)) // signaling+data
	// 		assert.LessOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "tx")), float64(50))     // just signaling

	// 		c = telemetry.ListenerBytesTotal
	// 		assert.Equal(t, 2, testutil.CollectAndCount(c), "ListenerConnsTotal")
	// 		assert.GreaterOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "rx")), float64(1000)) // signaling+data
	// 		assert.LessOrEqual(t, testutil.ToFloat64(c.WithLabelValues("udp", "tx")), float64(1000))    // just signaling

	// 		c = telemetry.ClusterPacketsTotal
	// 		assert.Equal(t, 0, testutil.CollectAndCount(c), "ListenerConnsTotal")
	// 		assert.Equal(t, float64(0), testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "rx")))
	// 		assert.Equal(t, float64(0), testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "tx")))

	// 		c = telemetry.ClusterBytesTotal
	// 		assert.Equal(t, 0, testutil.CollectAndCount(c), "ListenerConnsTotal")
	// 		assert.Equal(t, float64(0), testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "rx")))
	// 		assert.Equal(t, float64(0), testutil.ToFloat64(c.WithLabelValues("echo-server-cluster", "tx")))
	// 	},
	// },
}

func TestStunnerPortRangeWithVNet(t *testing.T) {
	lim := test.TimeOut(time.Second * 60)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	// log rate-limiter settings
	LogRateLimit = 2
	LogBurst = 1

	for _, c := range testPortRangeConfigsWithVNet {
		t.Run(c.testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", c.testName)

			// patch in the vnet
			log.Debug("building virtual network")
			v, err := buildVNet(loggerFactory)
			assert.NoError(t, err, err)

			log.Debug("setting up the mock DNS")
			mockDns := resolver.NewMockResolver(map[string]([]string){
				"stunner.l7mp.io":     []string{"1.2.3.4"},
				"echo-server.l7mp.io": []string{"1.2.3.5"},
				"dummy.l7mp.io":       []string{"1.2.3.10"},
			}, loggerFactory)

			log.Debug("creating a stunnerd")
			stunner := NewStunner(Options{
				LogLevel:         stunnerTestLoglevel,
				SuppressRollback: true,
				Resolver:         mockDns,
				Net:              v.podnet,
			})

			log.Debug("starting stunnerd")
			assert.NoError(t, stunner.Reconcile(&c.config), "starting server")

			var u, p string
			auth := c.config.Auth.Type
			switch auth {
			case "plaintext", "static":
				u = "user1"
				p = "passwd1"
			case "longterm", "ephemeral":
				u, p, err = turn.GenerateLongTermCredentials("my-secret", time.Minute)
				assert.NoError(t, err, err)
			default:
				assert.NoError(t, fmt.Errorf("internal error: unknown auth type in test"))
			}

			log.Debug("creating a client")
			lconn, err := v.wan.ListenPacket("udp4", "0.0.0.0:0")
			assert.NoError(t, err, "cannot create client listening socket")

			testConfig := echoTestConfig{t, v.podnet, v.wan, stunner,
				"stunner.l7mp.io:3478", lconn, u, p, net.IPv4(5, 6, 7, 8),
				c.echoServerAddr, true, true, c.result, loggerFactory}
			stunnerEchoFloodTest(testConfig)

			if c.tester != nil {
				c.tester(t)
			}

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")
			stunner.Close()
			assert.NoError(t, v.Close(), "cannot close VNet")
		})
	}
}

func stunnerEchoFloodTest(conf echoTestConfig) {
	t := conf.t
	log := conf.loggerFactory.NewLogger("test")

	client, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: conf.stunnerAddr,
		TURNServerAddr: conf.stunnerAddr,
		Username:       conf.user,
		Password:       conf.pass,
		Conn:           conf.lconn,
		Net:            conf.wan,
		LoggerFactory:  conf.loggerFactory,
	})

	assert.NoError(t, err, "cannot create TURN client")
	assert.NoError(t, client.Listen(), "cannot listen on TURN client")
	defer client.Close()

	log.Debug("sending a binding request")
	// reflAddr, err := bindingRequestWithTimeout(client, 10000 * time.Millisecond)
	reflAddr, err := client.SendBindingRequest()
	if conf.bindSuccess == false {
		assert.Error(t, err, "binding request failed")
	} else {
		assert.NoError(t, err, "binding request ok")
		log.Debugf("mapped-address: %v", reflAddr.String())
		udpAddr := reflAddr.(*net.UDPAddr)

		// The mapped-address should have IP address that was assigned to the LAN router.
		assert.True(t, udpAddr.IP.Equal(conf.natAddr), "wrong srfx address")

		log.Debug("sending an allocate request")
		conn, err := client.Allocate()
		if conf.allocateSuccess == false {
			assert.Error(t, err, err)
		} else {
			assert.NoError(t, err, err)

			// log.Debugf("laddr: %s", conn.LocalAddr().String())

			log.Debugf("creating echo-server listener socket at: %s", conn.LocalAddr().String())
			echoConn, err := conf.podnet.ListenPacket("udp4", conf.echoServerAddr)
			assert.NoError(t, err, "creating echo socket")

			// assert.NotNil(t, err, "echo socket not nil")

			go func() {
				buf := make([]byte, 1600)
				for {
					n, from, err2 := echoConn.ReadFrom(buf)
					if err2 != nil {
						break
					}

					// verify the message was received from the relay address
					assert.Equal(t, conn.LocalAddr().String(), from.String(),
						"message should be received from the relay address")
					assert.Equal(t, "Hello", string(buf[:n]), "wrong message payload")

					// echo the data
					_, err2 = echoConn.WriteTo(buf[:n], from)
					assert.NoError(t, err2, err2)
				}
			}()

			buf := make([]byte, 1600)
			if conf.echoSuccess == true {
				for i := 0; i < 500; i++ {
					log.Debug("sending \"Hello\"")
					_, err = conn.WriteTo([]byte("Hello"), echoConn.LocalAddr())
					assert.NoError(t, err, err)

					n, from, err2 := conn.ReadFrom(buf)
					assert.NoError(t, err2, err2)
					assert.Equal(t, n, len("Hello"), "message OK")
					assert.Equal(t, []byte("Hello"), buf[:n], "message OK")

					// verify the message was received from the relay address
					assert.Equal(t, echoConn.LocalAddr().String(), from.String(),
						"message should be received from the relay address")

					time.Sleep(2 * time.Millisecond)
				}
			} else {
				// should fail but it does not: client does not get feedback on
				// server-side port filtering
				for i := 0; i < 500; i++ {
					log.Debug("sending \"Hello\"")
					_, err = conn.WriteTo([]byte("Hello"), echoConn.LocalAddr())
					assert.NoError(t, err, err)

					// read should time out
					assert.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Millisecond)), "read deafline")
					_, _, err2 := conn.ReadFrom(buf)
					assert.Error(t, err2, "deadline exceeded")
				}
			}
			assert.NoError(t, conn.Close(), "cannot close relay connection")
			assert.NoError(t, echoConn.Close(), "cannot close echo server connection")
		}
	}

	time.Sleep(150 * time.Millisecond)
	client.Close()
}

/********************************************
 *
 * lifecycle + health check tests
 *
 *********************************************/
type stunnerLifecycleTestConfig struct {
	name                            string
	hcEndpoint                      *string
	livenessTester, readinessTester func(t *testing.T, status bool, err error)
}

var testLifecycleURLSpecDefault = "http://127.0.0.1:8086"
var testLifecycleURLDisable = ""
var testLifecycleURLNoAddr = "http://:8086"
var testLifecycleURLDiffPort = "http://0.0.0.0:8087"
var testLifecycleURLNoAddrNoPort = "http://"

var testLifecycle = []stunnerLifecycleTestConfig{
	{
		name:       "default",
		hcEndpoint: nil,
		livenessTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "liveness test: running")
			assert.True(t, status, "liveness test: alive")
		},
		readinessTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "readiness test: running")
			assert.True(t, status, "readiness test: ready")
		},
	},
	{
		name:       "enable with full health-check spec",
		hcEndpoint: &testLifecycleURLSpecDefault,
		livenessTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "liveness test: running")
			assert.True(t, status, "liveness test: alive")
		},
		readinessTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "readiness test: running")
			assert.True(t, status, "readiness test: ready")
		},
	},
	{
		name:       "disable",
		hcEndpoint: &testLifecycleURLDisable,
		livenessTester: func(t *testing.T, status bool, err error) {
			assert.Error(t, err, "liveness test: not running")
		},
		readinessTester: func(t *testing.T, status bool, err error) {
			assert.Error(t, err, "readiness test: not running")
		},
	},
	{
		name:       "enable with no addr",
		hcEndpoint: &testLifecycleURLNoAddr,
		livenessTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "liveness test: running")
			assert.True(t, status, "liveness test: alive")
		},
		readinessTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "readiness test: running")
			assert.True(t, status, "readiness test: ready")
		},
	},
	{
		name:       "reconcile with a different port",
		hcEndpoint: &testLifecycleURLDiffPort,
		livenessTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "liveness test: running")
			assert.True(t, status, "liveness test: alive")
		},
		readinessTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "readiness test: running")
			assert.True(t, status, "readiness test: ready")
		},
	},
	{
		name:       "reconcile with no addr and no port",
		hcEndpoint: &testLifecycleURLNoAddrNoPort,
		livenessTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "liveness test: running")
			assert.True(t, status, "liveness test: alive")
		},
		readinessTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "readiness test: running")
			assert.True(t, status, "readiness test: ready")
		},
	},
	{
		name:       "reconcole with full health-check spec again",
		hcEndpoint: &testLifecycleURLSpecDefault,
		livenessTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "liveness test: running")
			assert.True(t, status, "liveness test: alive")
		},
		readinessTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "readiness test: running")
			assert.True(t, status, "readiness test: ready")
		},
	},
}

func TestStunnerLifecycle(t *testing.T) {
	lim := test.TimeOut(time.Second * 120)
	defer lim.Stop()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	log.Debug("creating a stunnerd")
	s := NewStunner(Options{
		LogLevel: stunnerTestLoglevel,
	})

	// HACK
	assert.True(t, s.IsReady(), "empty server not ready")
	// assert.False(t, s.IsReady(), "empty server not ready")

	// health-check empty server
	_, err := doLivenessCheck("http://127.0.0.1:8086")
	assert.Error(t, err, "no default liveness check for empty server")
	_, err = doReadinessCheck("http://127.0.0.1:8086")
	assert.Error(t, err, "no default readiness check for empty server")

	log.Debug("starting stunnerd with an empty stunner config")
	conf := stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
		Admin:      stnrv1.AdminConfig{LogLevel: stunnerTestLoglevel},
		Auth: stnrv1.AuthConfig{
			Credentials: map[string]string{
				"username": "user-1",
				"password": "pass-1",
			},
		},
		Listeners: []stnrv1.ListenerConfig{},
		Clusters:  []stnrv1.ClusterConfig{},
	}

	log.Debug("reconciling empty server")
	err = s.Reconcile(&conf)
	assert.NoError(t, err, "reconcile empty server")

	status, err := doLivenessCheck("http://127.0.0.1:8086")
	assert.NoError(t, err, "liveness test minimal server: running")
	assert.True(t, status, "liveness test minimal server: alive")

	status, err = doReadinessCheck("http://127.0.0.1:8086")
	assert.NoError(t, err, "readiness test minimal server: running")
	assert.True(t, status, "readiness test minimal server: ready")

	for _, c := range testLifecycle {
		t.Run(c.name, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", c.name)

			log.Debug("reconciling server")
			conf.Admin.HealthCheckEndpoint = c.hcEndpoint
			err := s.Reconcile(&conf)
			assert.NoError(t, err, "cannot reconcile")

			// obtain hc address
			e := "http://127.0.0.1:8086"
			if c.hcEndpoint != nil {
				e = *c.hcEndpoint
			}
			u, err := url.Parse(e)
			assert.NoError(t, err)

			addr := u.Hostname()
			if addr == "" || addr == "0.0.0.0" {
				addr = "127.0.0.1"
			}

			port := u.Port()
			if port == "" {
				port = strconv.Itoa(stnrv1.DefaultHealthCheckPort)
			}

			hc := fmt.Sprintf("http://%s:%s", addr, port)

			status, err := doLivenessCheck(hc)
			c.livenessTester(t, status, err)

			status, err = doReadinessCheck(hc)
			c.readinessTester(t, status, err)
		})
	}

	// make sure health-check is running
	h := "0.0.0.0"
	conf.Admin.HealthCheckEndpoint = &h
	assert.NoError(t, s.Reconcile(&conf), "cannot reconcile")

	status, err = doLivenessCheck("http://127.0.0.1:8086")
	assert.NoError(t, err, "liveness test before graceful-shutdown: running")
	assert.True(t, status, "liveness test before graceful-shutdown: alive")

	status, err = doReadinessCheck("http://127.0.0.1:8086")
	assert.NoError(t, err, "readiness test before graceful-shutdown: running")
	assert.True(t, status, "readiness test before graceful-shutdown: ready")

	s.Shutdown()

	status, err = doLivenessCheck("http://127.0.0.1:8086")
	assert.NoError(t, err, "liveness test after graceful-shutdown: running")
	assert.True(t, status, "liveness test after graceful-shutdown: alive")

	status, err = doReadinessCheck("http://127.0.0.1:8086")
	assert.NoError(t, err, "readiness test after graceful-shutdown: running")
	assert.False(t, status, "readiness test after graceful-shutdown: ready")

	s.Close()

	_, err = doLivenessCheck("http://127.0.0.1:8086")
	assert.Error(t, err, "liveness test before close: not running")

	_, err = doReadinessCheck("http://127.0.0.1:8086")
	assert.Error(t, err, "readiness test before close: not running")
}

/********************************************
 *
 *  metric server tests
 *
 *********************************************/
type stunnerMetricsTestConfig struct {
	name, mcEndpoint string
	metricsTester    func(t *testing.T, status bool, err error)
}

var testMetrics = []stunnerMetricsTestConfig{
	{
		name:       "enable with full metric-server spec",
		mcEndpoint: "http://127.0.0.1:9080/metrics",
		metricsTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "metric server: running")
			assert.True(t, status, "metric server: serving")
		},
	},
	{
		name:       "reconcile with no path",
		mcEndpoint: "http://127.0.0.1:9080",
		metricsTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "metric server: running")
			assert.True(t, status, "metric server: serving")
		},
	},
	{
		name:       "disable",
		mcEndpoint: "",
		metricsTester: func(t *testing.T, status bool, err error) {
			assert.Error(t, err, "metric server: not running")
		},
	},
	{
		name:       "enable with no addr",
		mcEndpoint: "http://:9080/metrics",
		metricsTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "metric server: running")
			assert.True(t, status, "metric server: serving")
		},
	},
	{
		name:       "reconcile with a different port",
		mcEndpoint: "http://0.0.0.0:9087/metrics",
		metricsTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "metric server: running")
			assert.True(t, status, "metric server: serving")
		},
	},
	{
		name:       "reconcile with no addr and no port",
		mcEndpoint: "http://",
		metricsTester: func(t *testing.T, status bool, err error) {
			assert.NoError(t, err, "metric server: running")
			assert.True(t, status, "metric server: serving")
		},
	},
}

func TestStunnerMetrics(t *testing.T) {
	lim := test.TimeOut(time.Second * 120)
	defer lim.Stop()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	log.Debug("creating a stunnerd")
	s := NewStunner(Options{
		LogLevel: stunnerTestLoglevel,
	})

	// HACK
	assert.True(t, s.IsReady(), "empty server not ready")
	// assert.False(t, s.IsReady(), "empty server not ready")

	log.Debug("starting stunnerd with an empty stunner config")
	conf := stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
		Admin:      stnrv1.AdminConfig{LogLevel: stunnerTestLoglevel},
		Auth: stnrv1.AuthConfig{
			Credentials: map[string]string{
				"username": "user-1",
				"password": "pass-1",
			},
		},
		Listeners: []stnrv1.ListenerConfig{},
		Clusters:  []stnrv1.ClusterConfig{},
	}

	log.Debug("reconciling empty server")
	err := s.Reconcile(&conf)
	assert.NoError(t, err, "reconcile empty server")

	assert.True(t, s.IsReady(), "server ready")

	for _, c := range testMetrics {
		t.Run(c.name, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", c.name)

			log.Debug("reconciling server")
			conf.Admin.MetricsEndpoint = c.mcEndpoint
			err := s.Reconcile(&conf)
			assert.NoError(t, err, "cannot reconcile")

			// obtain metric address
			u, err := url.Parse(c.mcEndpoint)
			assert.NoError(t, err)

			addr := u.Hostname()
			if addr == "" || addr == "0.0.0.0" {
				addr = "127.0.0.1"
			}

			port := u.Port()
			if port == "" {
				port = strconv.Itoa(stnrv1.DefaultMetricsPort)
			}

			path := u.EscapedPath()
			hc := fmt.Sprintf("http://%s:%s/%s", addr, port, path)

			status, err := doHttp(hc)
			c.metricsTester(t, status, err)
		})
	}

	assert.True(t, s.IsReady(), "server ready")

	s.Shutdown()

	assert.False(t, s.IsReady(), "server ready")

	s.Close()
}

func doHttp(uri string) (bool, error) {
	resp, err := http.Get(uri)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return true, nil
	}

	return false, nil
}

func doLivenessCheck(uri string) (bool, error) {
	return doHttp(uri + "/live")
}

func doReadinessCheck(uri string) (bool, error) {
	return doHttp(uri + "/ready")
}

// *****************
// v1alpha1 API compatibility tests
// *****************
type TestConfigV1Alpha1 struct {
	testName       string
	config         []byte
	echoServerAddr string
	result         bool
}

var testConfigsV1Alpha1 = []TestConfigV1Alpha1{
	{
		testName:       "open ok",
		config:         []byte(`{"version":"v1alpha1","admin":{"loglevel":"all:ERROR"},"auth":{"type":"plaintext","credentials":{"password":"passwd1","username":"user1"}},"listeners":[{"name":"udp","protocol":"turn-udp","address":"1.2.3.4","port":3478,"routes":["echo-server-cluster"]}],"clusters":[{"name":"echo-server-cluster","type":"STATIC","endpoints":["1.2.3.5"]}]}`),
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
	{
		testName:       "default cluster type static ok",
		config:         []byte(`{"version":"v1alpha1","admin":{"loglevel":"all:ERROR"},"auth":{"type":"plaintext","credentials":{"password":"passwd1","username":"user1"}},"listeners":[{"name":"udp","protocol":"turn-udp","address":"1.2.3.4","port":3478,"routes":["echo-server-cluster"]}],"clusters":[{"name":"echo-server-cluster","endpoints":["1.2.3.5"]}]}`),
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
	{
		testName:       "static endpoint ok",
		config:         []byte(`{"version":"v1alpha1","admin":{"loglevel":"all:ERROR"},"auth":{"type":"plaintext","credentials":{"password":"passwd1","username":"user1"}},"listeners":[{"name":"udp","protocol":"turn-udp","address":"1.2.3.4","port":3478,"routes":["echo-server-cluster"]}],"clusters":[{"name":"echo-server-cluster","type":"STATIC","endpoints":["1.2.3.5"]}]}`),
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
	{
		testName:       "static endpoint with multiple routes ok",
		config:         []byte(`{"version":"v1alpha1","admin":{"loglevel":"all:ERROR"},"auth":{"type":"plaintext","credentials":{"password":"passwd1","username":"user1"}},"listeners":[{"name":"udp","protocol":"turn-udp","address":"1.2.3.4","port":3478,"routes":["echo-server-cluster","dummy_cluster"]}],"clusters":[{"name":"echo-server-cluster","type":"STATIC","endpoints":["1.2.3.5"]},{"name":"dummy_cluster","type":"STATIC","endpoints":["9.8.7.6"]}]}`),
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
	{
		testName:       "longterm endpoint with multiple routes ok",
		config:         []byte(`{"version":"v1alpha1","admin":{"loglevel":"all:ERROR"},"auth":{"type":"longterm","credentials":{"secret":"my-secret"}},"listeners":[{"name":"udp","protocol":"turn-udp","public_address":"1.2.3.4","public_port":3478,"address":"127.0.0.1","port":3478,"routes":["allow-any"]}],"clusters":[{"name":"allow-any","endpoints":["0.0.0.0/0"]}]}`),
		echoServerAddr: "1.2.3.5:5678",
		result:         true,
	},
}

func TestStunnerConfigV1Alpha1(t *testing.T) {
	lim := test.TimeOut(time.Second * 60)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, c := range testConfigsV1Alpha1 {
		t.Run(c.testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", c.testName)

			// patch in the vnet
			log.Debug("building virtual network")
			v, err := buildVNet(loggerFactory)
			assert.NoError(t, err, err)

			log.Debug("creating a stunnerd")
			stunner := NewStunner(Options{
				LogLevel:         stunnerTestLoglevel,
				SuppressRollback: true,
				Net:              v.podnet,
			})

			log.Debug("parsing config to v1alpha1 format")
			a := stnrv1a1.StunnerConfig{}
			assert.NoError(t, json.Unmarshal(c.config, &a), "parsing config file to v1alpha1 format")

			assert.Equal(t, stnrv1a1.ApiVersion, a.ApiVersion, "version")
			assert.Equal(t, "all:ERROR", a.Admin.LogLevel, "loglevel")
			// expect the old names
			assert.True(t, a.Auth.Type == "plaintext" || a.Auth.Type == "longterm", "loglevel")
			assert.Len(t, a.Listeners, 1, "listeners len")
			assert.Equal(t, "udp", a.Listeners[0].Name, "listener name")
			assert.Equal(t, "turn-udp", a.Listeners[0].Protocol, "listener proto")
			assert.Equal(t, 3478, a.Listeners[0].Port, "listener port")
			assert.True(t, len(a.Clusters) > 0, "clusters len")

			log.Debug("conveting config to v1 format")
			a = stnrv1a1.StunnerConfig{}
			assert.NoError(t, json.Unmarshal(c.config, &a), "parsing config file to v1alpha1 format")
			config, err := stnrv1a1.ConvertToV1(&a)
			assert.NoError(t, err, "convert load v1alpha1 config to v1")

			assert.Equal(t, stnrv1.ApiVersion, config.ApiVersion, "version")
			assert.Equal(t, "all:ERROR", config.Admin.LogLevel, "loglevel")
			// expect the new names
			assert.True(t, config.Auth.Type == "static" || config.Auth.Type == "ephemeral", "loglevel")
			assert.Len(t, config.Listeners, 1, "listeners len")
			assert.Equal(t, "udp", config.Listeners[0].Name, "listener name")
			assert.Equal(t, "turn-udp", config.Listeners[0].Protocol, "listener proto")
			assert.Equal(t, 3478, config.Listeners[0].Port, "listener port")
			assert.True(t, len(config.Clusters) > 0, "clusters len")

			log.Debug("parsing config directly to v1 format")
			config, err = cfgclient.ParseConfig(c.config)
			assert.NoError(t, err, "load v1alpha1 config ")

			assert.Equal(t, stnrv1.ApiVersion, config.ApiVersion, "version")
			assert.Equal(t, "all:ERROR", config.Admin.LogLevel, "loglevel")
			// expect the new names
			assert.True(t, config.Auth.Type == "static" || config.Auth.Type == "ephemeral", "loglevel")
			assert.Len(t, config.Listeners, 1, "listeners len")
			assert.Equal(t, "udp", config.Listeners[0].Name, "listener name")
			assert.Equal(t, "turn-udp", config.Listeners[0].Protocol, "listener proto")
			assert.Equal(t, 3478, config.Listeners[0].Port, "listener port")
			assert.True(t, len(config.Clusters) > 0, "clusters len")

			log.Debug("starting stunnerd")
			assert.NoError(t, stunner.Reconcile(config), "starting server")

			var u, p string
			auth := config.Auth.Type
			switch auth {
			case "plaintext", "static":
				u = "user1"
				p = "passwd1"
			case "longterm", "ephemeral":
				u, p, err = turn.GenerateLongTermCredentials("my-secret", time.Minute)
				assert.NoError(t, err, err)
			default:
				assert.NoError(t, fmt.Errorf("internal error: unknown auth type in test"))
			}

			log.Debug("creating a client")
			lconn, err := v.wan.ListenPacket("udp4", "0.0.0.0:0")
			assert.NoError(t, err, "cannot create client listening socket")

			testConfig := echoTestConfig{t, v.podnet, v.wan, stunner,
				"stunner.l7mp.io:3478", lconn, u, p, net.IPv4(5, 6, 7, 8),
				c.echoServerAddr, true, true, c.result, loggerFactory}
			stunnerEchoTest(testConfig)

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")
			stunner.Close()
			assert.NoError(t, v.Close(), "cannot close VNet")
		})
	}
}
