package stunner

import (
	"os"
	"net"
	"fmt"
	"testing"
	"time"
	"math/big"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"crypto/rsa"
	"crypto/rand"
	"encoding/pem"
	
	"github.com/pion/logging"
	"github.com/pion/turn/v2"
	"github.com/pion/dtls/v2"
	"github.com/pion/transport/test"
	"github.com/pion/transport/vnet"
	"github.com/stretchr/testify/assert"
)

var stunnerTestLoglevel string = "all:ERROR"
//var stunnerTestLoglevel string = "all:TRACE"

type stunnerEchoTestConfig struct {
	t *testing.T
	// net
	podnet, wan *vnet.Net
	// server
	stunner *Stunner
	stunnerAddr string
	// client
	lconn net.PacketConn
	user, pass string
	natAddr net.IP
	// echo
	echoServerAddr string
	loggerFactory *logging.DefaultLoggerFactory
}

func stunnerEchoTest(conf stunnerEchoTestConfig){
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
	reflAddr, err := client.SendBindingRequest()
	assert.NoError(t, err, err)
	log.Debugf("mapped-address: %v", reflAddr.String())
	udpAddr := reflAddr.(*net.UDPAddr)

	// The mapped-address should have IP address that was assigned to the LAN router.
	assert.True(t, udpAddr.IP.Equal(conf.natAddr), "wrong srfx address")

	log.Debug("sending an allocate request")
	conn, err := client.Allocate()
	assert.NoError(t, err, err)

	log.Debugf("laddr: %s", conn.LocalAddr().String())

	echoConn, err := conf.podnet.ListenPacket("udp4", conf.echoServerAddr)
	assert.NoError(t, err, err)

	log.Debug("obtaining TURN server")
	server := conf.stunner.GetServer()

	// ensure allocation is counted
	assert.Equal(t, 1, server.AllocationCount())

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
	for i := 0; i < 8; i++ {
		log.Debug("sending \"Hello\"")
		_, err = conn.WriteTo([]byte("Hello"), echoConn.LocalAddr())
		assert.NoError(t, err, err)

		_, from, err2 := conn.ReadFrom(buf)
		assert.NoError(t, err2, err2)

		// verify the message was received from the relay address
		assert.Equal(t, echoConn.LocalAddr().String(), from.String(),
			"message should be received from the relay address")

		time.Sleep(100 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)
	client.Close()

	assert.NoError(t, conn.Close(), "cannot close relay connection")
	assert.NoError(t, echoConn.Close(), "cannot close echo server connection")
}

func generateKey(crtFile, keyFile *os.File) error{
	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return err
	}
	keyBytes := x509.MarshalPKCS1PrivateKey(key)
	// PEM encoding of private key
	keyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: keyBytes,
		},
	)

	notBefore := time.Now()
	notAfter := notBefore.Add(365*24*100*time.Hour)

	//Create certificate template
	template := x509.Certificate{
		SerialNumber:          big.NewInt(0),
		Subject:               pkix.Name{CommonName: "localhost"},
		SignatureAlgorithm:    x509.SHA256WithRSA,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyAgreement |
			x509.KeyUsageKeyEncipherment | x509.KeyUsageDataEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}

	//Create certificate using template
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return err

	}

	//pem encoding of certificate
	certPem := pem.EncodeToMemory(
		&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: derBytes,
		},
	)

	crtFile.Write(certPem)
	keyFile.Write(keyPEM)

	return nil
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
	gw      *vnet.Router  // kube-proxy
	podnet  *vnet.Net     // k8s pod network
	wan     *vnet.Net     // external network
}

func (v *VNet) Close() error {
	return v.gw.Stop()
}

func buildVNet(logger *logging.DefaultLoggerFactory) (*VNet, error) {
	gw, err := vnet.NewRouter(&vnet.RouterConfig{
		Name:          "gw",
		CIDR:          "0.0.0.0/0",
		LoggerFactory: logger,
	})
	if err != nil { return nil, err }

	// client side
	podnet := vnet.NewNet(&vnet.NetConfig{StaticIPs: []string{"1.2.3.4", "1.2.3.5"}})
	err = gw.AddNet(podnet)
	if err != nil { return nil, err }

	// LAN
	nat, err := vnet.NewRouter(&vnet.RouterConfig{
		Name:      "lan",
		StaticIPs: []string{"5.6.7.8"}, // this router's external IP on eth0
		CIDR:      "192.168.0.0/24",
		NATType:   &vnet.NATType{
			MappingBehavior:   vnet.EndpointIndependent,
			FilteringBehavior: vnet.EndpointIndependent,
		},
		LoggerFactory: logger,
	})
	if err != nil { return nil, err }

	wan := vnet.NewNet(&vnet.NetConfig{})
	if err = nat.AddNet(wan); err != nil { return nil, err }
	if err = gw.AddRouter(nat); err != nil { return nil, err }
	if err = gw.Start(); err != nil { return nil, err }

	// register host names
	err = gw.AddHost("stunner.l7mp.io", "1.2.3.4")
	if err != nil { return nil, err }

	return &VNet{
		gw:     gw,
		podnet: podnet,
		wan:    wan,
	}, nil
}

func TestStunnerDefaultServerVNet(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	// loggerFactory := NewLoggerFactory("all:TRACE")
	loggerFactory := NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, conf := range []string{
		"turn://user1:passwd1@1.2.3.4:3478?transport=udp",
		 "udp://user1:passwd1@1.2.3.4:3478?transport=udp",
		 "udp://user1:passwd1@1.2.3.4:3478",
	}{
		t.Run(fmt.Sprintf("TestStunner_NewDefaultStunnerConfig_URI:%s", conf), func(t *testing.T) {
			log.Debug("creating default stunner config")
			c, err := NewDefaultStunnerConfig(conf, stunnerTestLoglevel)
			assert.NoError(t, err, err)

			// patch in the vnet
			log.Debug("building virtual network")
			v, err := buildVNet(loggerFactory)
			assert.NoError(t, err, err)
			c.Net = v.podnet

			log.Debug("creating a stunnerd")
			stunner, err := NewStunner(c)
			assert.NoError(t, err)

			log.Debug("creating a client")
			lconn, err := v.wan.ListenPacket("udp4", "0.0.0.0:0")
			assert.NoError(t, err, "cannot create client listening socket")

			testConfig := stunnerEchoTestConfig{t, v.podnet, v.wan, stunner,
				"stunner.l7mp.io:3478", lconn, "user1", "passwd1", net.IPv4(5, 6, 7, 8),
				"1.2.3.5:5678", loggerFactory}
			stunnerEchoTest(testConfig)

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")
			stunner.Close()
			assert.NoError(t, v.Close(), "cannot close VNet")
		})
	}
}

var testVNetConfigs = []StunnerConfig{
	{
		ApiVersion: "v1alpha1",
		Admin: AdminConfig{
			LogLevel: stunnerTestLoglevel,
		},
		Static: StaticResourceConfig{
			Auth: AuthConfig{
				Type: "plaintext",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []ListenerConfig{{
				Protocol: "udp",
				Addr: "1.2.3.4",
				Port: 3478,
			}},
		},
	},
	{
		ApiVersion: "v1alpha1",
		Admin: AdminConfig{
			LogLevel: stunnerTestLoglevel,
		},
		Static: StaticResourceConfig{
			Auth: AuthConfig{
				Type: "longterm",
				Credentials: map[string]string{
					"secret": "my-secret",
				},
			},
			Listeners: []ListenerConfig{{
				Protocol: "udp",
				Addr: "1.2.3.4",
				Port: 3478,
			}},
		},
	},
}

func TestStunnerAuthServerVNet(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, c := range testVNetConfigs {
		auth := c.Static.Auth.Type
		testName := fmt.Sprintf("TestStunner_NewStunner_VNet_auth:%s", auth)
		t.Run(testName, func(t *testing.T) {
			// patch in the vnet
			log.Debug("building virtual network")
			v, err := buildVNet(loggerFactory)
			assert.NoError(t, err, err)
			c.Net = v.podnet

			log.Debug("creating a stunnerd")
			stunner, err := NewStunner(&c)
			assert.NoError(t, err, err)

			var u, p string
			switch auth {
			case "plaintext":
				u = "user1"
				p = "passwd1"
			case "longterm":
				u, p, err = turn.GenerateLongTermCredentials("my-secret", time.Minute)
				assert.NoError(t, err, err)
			default:
				assert.NoError(t, fmt.Errorf("internal error: unknown auth type in test"))
			}

			log.Debug("creating a client")
			lconn, err := v.wan.ListenPacket("udp4", "0.0.0.0:0")
			assert.NoError(t, err, "cannot create client listening socket")

			testConfig := stunnerEchoTestConfig{t, v.podnet, v.wan, stunner,
				"stunner.l7mp.io:3478", lconn, u, p, net.IPv4(5, 6, 7, 8),
				"1.2.3.5:5678", loggerFactory}
			stunnerEchoTest(testConfig)

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")
			stunner.Close()
			assert.NoError(t, v.Close(), "cannot close VNet")
		})
	}
}

// *****************
// TCP/TLS/DTLS tests over localhost: VNet supports UDP only so these tests will run over the localhost
// *****************
// Topology:
//
//                /----- STUNner (udp/tcp/tls/dtls:23478)
//     client--- lo
//                \----- echo-server (udp:25678)
//

func TestStunnerServerLocalhost(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	// loggerFactory := NewLoggerFactory("all:TRACE")
	loggerFactory := NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	certFile, err := os.CreateTemp("", "stunner_test.*.cert")
	assert.NoError(t, err, "cannot create temp file for SSL cert")
	defer certFile.Close()
	defer func() { assert.NoError(t, os.Remove(certFile.Name()), "cannot delete SSL cert file")}()

	keyFile, err := os.CreateTemp("", "stunner_test.*.key")
	assert.NoError(t, err, "cannot create temp file for SSL key")
	defer keyFile.Close()
	defer func() { assert.NoError(t, os.Remove(keyFile.Name()), "cannot delete SSL key file")}()

	err = generateKey(certFile, keyFile)
	assert.NoError(t, err, "cannot generate SSL SSL cert/key")

	testLocalhostConfigs := []StunnerConfig{
		// udp, plaintext
		{
			ApiVersion: "v1alpha1",
			Admin: AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Static: StaticResourceConfig{
				Auth: AuthConfig{
					Type: "plaintext",
					Credentials: map[string]string{
						"username": "user1",
						"password": "passwd1",
					},
				},
				Listeners: []ListenerConfig{{
					Protocol: "udp",
					Addr: "127.0.0.1",
					Port: 23478,
				}},
			},
		},
		// udp, longterm
		{
			ApiVersion: "v1alpha1",
			Admin: AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Static: StaticResourceConfig{
				Auth: AuthConfig{
					Type: "longterm",
					Credentials: map[string]string{
						"secret": "my-secret",
					},
				},
				Listeners: []ListenerConfig{{
					Protocol: "udp",
					Addr: "127.0.0.1",
					Port: 23478,
				}},
			},
		},
		// tcp, plaintext
		{
			ApiVersion: "v1alpha1",
			Admin: AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Static: StaticResourceConfig{
				Auth: AuthConfig{
					Type: "plaintext",
					Credentials: map[string]string{
						"username": "user1",
						"password": "passwd1",
					},
				},
				Listeners: []ListenerConfig{{
					Protocol: "tcp",
					Addr: "127.0.0.1",
					Port: 23478,
				}},
			},
		},
		// tcp, longterm
		{
			ApiVersion: "v1alpha1",
			Admin: AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Static: StaticResourceConfig{
				Auth: AuthConfig{
					Type: "longterm",
					Credentials: map[string]string{
						"secret": "my-secret",
					},
				},
				Listeners: []ListenerConfig{{
					Protocol: "tcp",
					Addr: "127.0.0.1",
					Port: 23478,
				}},
			},
		},
		// tls, plaintext
		{
			ApiVersion: "v1alpha1",
			Admin: AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Static: StaticResourceConfig{
				Auth: AuthConfig{
					Type: "plaintext",
					Credentials: map[string]string{
						"username": "user1",
						"password": "passwd1",
					},
				},
				Listeners: []ListenerConfig{{
					Protocol: "tls",
					Addr: "127.0.0.1",
					Port: 23478,
					Cert: certFile.Name(),
					Key: keyFile.Name(),
				}},
			},
		},
		// tls, longterm
		{
			ApiVersion: "v1alpha1",
			Admin: AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Static: StaticResourceConfig{
				Auth: AuthConfig{
					Type: "longterm",
					Credentials: map[string]string{
						"secret": "my-secret",
					},
				},
				Listeners: []ListenerConfig{{
					Protocol: "tls",
					Addr: "127.0.0.1",
					Port: 23478,
					Cert: certFile.Name(),
					Key: keyFile.Name(),
				}},
			},
		},
		// dtls, plaintext
		{
			ApiVersion: "v1alpha1",
			Admin: AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Static: StaticResourceConfig{
				Auth: AuthConfig{
					Type: "plaintext",
					Credentials: map[string]string{
						"username": "user1",
						"password": "passwd1",
					},
				},
				Listeners: []ListenerConfig{{
					Protocol: "dtls",
					Addr: "127.0.0.1",
					Port: 23478,
					Cert: certFile.Name(),
					Key: keyFile.Name(),
				}},
			},
		},
		// dtls, longterm
		{
			ApiVersion: "v1alpha1",
			Admin: AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Static: StaticResourceConfig{
				Auth: AuthConfig{
					Type: "longterm",
					Credentials: map[string]string{
						"secret": "my-secret",
					},
				},
				Listeners: []ListenerConfig{{
					Protocol: "dtls",
					Addr: "127.0.0.1",
					Port: 23478,
					Cert: certFile.Name(),
					Key: keyFile.Name(),
				}},
			},
		},
	}

	for _, c := range testLocalhostConfigs {
		auth := c.Static.Auth.Type
		proto := c.Static.Listeners[0].Protocol
		testName := fmt.Sprintf("TestStunner_NewStunner_Localhost_auth:%s_client:%s", auth, proto)

		t.Run(testName, func(t *testing.T) {
			log.Debug("creating a stunnerd")
			stunner, err := NewStunner(&c)
			assert.NoError(t, err, err)

			var u, p string
			switch auth {
			case "plaintext":
				u = "user1"
				p = "passwd1"
			case "longterm":
				u, p, err = turn.GenerateLongTermCredentials("my-secret", time.Minute)
				assert.NoError(t, err, err)
			default:
				assert.NoError(t, fmt.Errorf("internal error: unknown auth type in test"))
			}

			stunnerAddr := "127.0.0.1:23478"

			log.Debug("creating a client")
			var lconn net.PacketConn
			switch proto {
			case "udp":
				lconn, err = net.ListenPacket("udp4", "0.0.0.0:0")
				assert.NoError(t, err, "cannot create UDP client socket")
			case "tcp":
				conn, cErr := net.Dial("tcp", stunnerAddr)
				assert.NoError(t, cErr, "cannot create TCP client socket")
				lconn = turn.NewSTUNConn(conn)
			case "tls":
				cert, err := tls.LoadX509KeyPair(certFile.Name(), keyFile.Name())
				assert.NoError(t, err, "cannot create certificate for TLS client socket")
				conn, err := tls.Dial("tcp", stunnerAddr, &tls.Config{
					MinVersion:   tls.VersionTLS12,
					Certificates: []tls.Certificate{cert},
					InsecureSkipVerify: true,
				})
				assert.NoError(t, err, "cannot create TLS client socket")
				lconn = turn.NewSTUNConn(conn)
			case "dtls":
				cert, err := tls.LoadX509KeyPair(certFile.Name(), keyFile.Name())
				assert.NoError(t, err, "cannot create certificate for DTLS client socket")
				// for some reason dtls.Listen requires a UDPAddr and not an addr string
				udpAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 23478}
				conn, err := dtls.Dial("udp", udpAddr, &dtls.Config{
					Certificates: []tls.Certificate{cert},
					InsecureSkipVerify: true,
				})
				assert.NoError(t, err, "cannot create DTLS client socket")
				lconn = turn.NewSTUNConn(conn)
			default:
				assert.NoError(t, fmt.Errorf("internal error: unknown client protocol in test"))
			}

			testConfig := stunnerEchoTestConfig{t, vnet.NewNet(nil), vnet.NewNet(nil), stunner,
				stunnerAddr, lconn, u, p, net.IPv4(127, 0, 0, 1),
				"127.0.0.1:25678", loggerFactory}
			stunnerEchoTest(testConfig)

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")
			stunner.Close()
		})
	}
}
