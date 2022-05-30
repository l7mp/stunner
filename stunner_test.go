package stunner

import (
	"os"
	"net"
	"fmt"
	// "reflect"
	"testing"
	"time"
	"strconv"
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

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
	"github.com/l7mp/stunner/internal/object"
)

//var stunnerTestLoglevel string = v1alpha1.DefaultLogLevel
var stunnerTestLoglevel string = "all:ERROR"
// var stunnerTestLoglevel string = "all:TRACE"
// var stunnerTestLoglevel string = "all:DEBUG"

type echoTestConfig struct {
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
        expectedResult bool
	loggerFactory *logging.DefaultLoggerFactory
}

func stunnerEchoTest(conf echoTestConfig){
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

        if conf.expectedResult == true {
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
        } else {
                // should fail 
                _, err = conn.WriteTo([]byte("Hello"), echoConn.LocalAddr())
                assert.Errorf(t, err, "got error message %s", err.Error())
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
	err = gw.AddHost("echo-server.l7mp.io", "1.2.3.5")
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
                testName := fmt.Sprintf("TestStunner_NewDefaultStunnerConfig_URI:%s", conf)
		t.Run(testName, func(t *testing.T) {
                        log.Debugf("-------------- Running test: %s -------------", testName)

			log.Debug("creating default stunner config")
			c, err := NewDefaultStunnerConfig(conf)
			assert.NoError(t, err, err)

                        // patch in the loglevel
                        c.Admin.LogLevel = stunnerTestLoglevel

			// patch in the vnet
			log.Debug("building virtual network")
			v, err := buildVNet(loggerFactory)
			assert.NoError(t, err, err)
			c.Net = v.podnet

			log.Debug("creating a stunnerd")
			stunner, err := NewStunner(c)
			assert.NoError(t, err)

			log.Debug("starting stunnerd")
			assert.NoError(t, stunner.Start())
                        
			log.Debug("creating a client")
			lconn, err := v.wan.ListenPacket("udp4", "0.0.0.0:0")
			assert.NoError(t, err, "cannot create client listening socket")

			testConfig := echoTestConfig{t, v.podnet, v.wan, stunner,
				"stunner.l7mp.io:3478", lconn, "user1", "passwd1", net.IPv4(5, 6, 7, 8),
				"1.2.3.5:5678", true, loggerFactory}
			stunnerEchoTest(testConfig)

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")
			stunner.Close()
			assert.NoError(t, v.Close(), "cannot close VNet")
		})
	}
}

var testStunnerConfigsWithVnet = []v1alpha1.StunnerConfig{
	{
		ApiVersion: "v1alpha1",
		Admin: v1alpha1.AdminConfig{
			LogLevel: stunnerTestLoglevel,
		},
                Auth: v1alpha1.AuthConfig{
                        Type: "plaintext",
                        Credentials: map[string]string{
                                "username": "user1",
                                "password": "passwd1",
                        },
                },
                Listeners: []v1alpha1.ListenerConfig{{
                        Name: "udp",
                        Protocol: "udp",
                        Addr: "1.2.3.4",
                        Port: 3478,
                        Routes: []string{"allow-any"},
                }},
                Clusters: []v1alpha1.ClusterConfig{{
                        Name: "allow-any",
                        Endpoints: []string{"0.0.0.0/0"},
                }},
	},
	{
		ApiVersion: "v1alpha1",
		Admin: v1alpha1.AdminConfig{
			LogLevel: stunnerTestLoglevel,
		},
                Auth: v1alpha1.AuthConfig{
                        Type: "longterm",
                        Credentials: map[string]string{
                                "secret": "my-secret",
				},
                },
                Listeners: []v1alpha1.ListenerConfig{{
                        Name: "udp",
                        Protocol: "udp",
                        Addr: "1.2.3.4",
                        Port: 3478,
                        Routes: []string{"allow-any"},
                }},
                Clusters: []v1alpha1.ClusterConfig{{
                        Name: "allow-any",
                        Endpoints: []string{"0.0.0.0/0"},
                }},
	},
}

func TestStunnerAuthServerVNet(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, c := range testStunnerConfigsWithVnet {
		auth := c.Auth.Type
		testName := fmt.Sprintf("TestStunner_NewStunner_VNet_auth:%s", auth)
		t.Run(testName, func(t *testing.T) {
                        log.Debugf("-------------- Running test: %s -------------", testName)
                      
			// patch in the vnet
			log.Debug("building virtual network")
			v, err := buildVNet(loggerFactory)
			assert.NoError(t, err, err)
			c.Net = v.podnet

			log.Debug("creating a stunnerd")
			stunner, err := NewStunner(&c)
			assert.NoError(t, err, err)

			log.Debug("starting stunnerd")
			assert.NoError(t, stunner.Start())

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

			testConfig := echoTestConfig{t, v.podnet, v.wan, stunner,
				"stunner.l7mp.io:3478", lconn, u, p, net.IPv4(5, 6, 7, 8),
				"1.2.3.5:5678", true, loggerFactory}
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
//                /----- STUNner (udp/tcp/tls/dtls:23478)
//     client--- lo
//                \----- echo-server (udp:25678)

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

        testStunnerConfigsWithLocalhost := []v1alpha1.StunnerConfig{
		// udp, plaintext
		{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
                        Auth: v1alpha1.AuthConfig{
                                Type: "plaintext",
                                Credentials: map[string]string{
                                        "username": "user1",
                                        "password": "passwd1",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "udp",
                                Protocol: "udp",
                                Addr: "127.0.0.1",
                                Port: 23478,
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
		},
		// udp, longterm
		{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
                        Auth: v1alpha1.AuthConfig{
                                Type: "longterm",
                                Credentials: map[string]string{
                                        "secret": "my-secret",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "udp",
                                Protocol: "udp",
                                Addr: "127.0.0.1",
                                Port: 23478,
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
		},
		// tcp, plaintext
		{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
                        Auth: v1alpha1.AuthConfig{
                                Type: "plaintext",
                                Credentials: map[string]string{
                                        "username": "user1",
                                        "password": "passwd1",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "udp",
                                Protocol: "tcp",
                                Addr: "127.0.0.1",
                                Port: 23478,
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
		},
		// tcp, longterm
		{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
                        Auth: v1alpha1.AuthConfig{
                                Type: "longterm",
                                Credentials: map[string]string{
                                        "secret": "my-secret",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "tcp",
                                Protocol: "tcp",
                                Addr: "127.0.0.1",
                                Port: 23478,
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
		},
		// tls, plaintext
		{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
                        Auth: v1alpha1.AuthConfig{
                                Type: "plaintext",
                                Credentials: map[string]string{
                                        "username": "user1",
                                        "password": "passwd1",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "tls",
                                Protocol: "tls",
                                Addr: "127.0.0.1",
                                Port: 23478,
                                Cert: certFile.Name(),
                                Key: keyFile.Name(),
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
		},
		// tls, longterm
		{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
                        Auth: v1alpha1.AuthConfig{
                                Type: "longterm",
                                Credentials: map[string]string{
                                        "secret": "my-secret",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "tls",
                                Protocol: "tls",
                                Addr: "127.0.0.1",
                                Port: 23478,
                                Cert: certFile.Name(),
                                Key: keyFile.Name(),
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
		},
		// dtls, plaintext
		{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
                        Auth: v1alpha1.AuthConfig{
                                Type: "plaintext",
                                Credentials: map[string]string{
                                        "username": "user1",
                                        "password": "passwd1",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "dtls",
                                Protocol: "dtls",
                                Addr: "127.0.0.1",
                                Port: 23478,
                                Cert: certFile.Name(),
                                Key: keyFile.Name(),
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
		},
		// dtls, longterm
		{
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
                        Auth: v1alpha1.AuthConfig{
                                Type: "longterm",
                                Credentials: map[string]string{
                                        "secret": "my-secret",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "dtls",
                                Protocol: "dtls",
                                Addr: "127.0.0.1",
                                Port: 23478,
                                Cert: certFile.Name(),
                                Key: keyFile.Name(),
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
		},
	}

	for _, c := range testStunnerConfigsWithLocalhost {
		auth := c.Auth.Type
		proto := c.Listeners[0].Protocol
		testName := fmt.Sprintf("TestStunner_NewStunner_Localhost_auth:%s_client:%s", auth, proto)

		t.Run(testName, func(t *testing.T) {
                     log.Debugf("-------------- Running test: %s -------------", testName)

			log.Debug("creating a stunnerd")
			stunner, err := NewStunner(&c)
			assert.NoError(t, err, err)

			log.Debug("starting stunnerd")
			assert.NoError(t, stunner.Start())

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

			testConfig := echoTestConfig{t, vnet.NewNet(nil), vnet.NewNet(nil), stunner,
				stunnerAddr, lconn, u, p, net.IPv4(127, 0, 0, 1),
				"127.0.0.1:25678", true, loggerFactory}
			stunnerEchoTest(testConfig)

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")
			stunner.Close()
		})
	}
}


// *****************
// Cluster tests with VNet
// *****************
// type StunnerClusterConfig struct {
//         config v1alpha1.StunnerConfig
//         echoServerAddr string
//         result bool
// }
type StunnerTestClusterConfig struct {
        name string
        config v1alpha1.StunnerConfig
        echoServerAddr string
        result bool
}
        
var testClusterConfigsWithVNet = []StunnerTestClusterConfig{
        {
                name: "open ok",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                LogLevel: stunnerTestLoglevel,
                        },
                        Auth: v1alpha1.AuthConfig{
                                Type: "plaintext",
                                Credentials: map[string]string{
                                        "username": "user1",
                                        "password": "passwd1",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "udp",
                                Protocol: "udp",
                                Addr: "1.2.3.4",
                                Port: 3478,
                                Routes: []string{"echo_server_cluster"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "echo_server_cluster",
                                Type: "STATIC",
                                Endpoints: []string{
                                        "1.2.3.5",
                                },
                        }},
                },
                echoServerAddr: "1.2.3.5:5678",
                result: true,
        },
        {
                name: "default cluster type static ok",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                LogLevel: stunnerTestLoglevel,
                        },
                        Auth: v1alpha1.AuthConfig{
                                Type: "plaintext",
                                Credentials: map[string]string{
                                        "username": "user1",
                                        "password": "passwd1",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "udp",
                                Protocol: "udp",
                                Addr: "1.2.3.4",
                                Port: 3478,
                                Routes: []string{
                                        "echo_server_cluster",
                                },
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "echo_server_cluster",
                                Endpoints: []string{
                                        "1.2.3.5",
                                },
                        }},
                },
                echoServerAddr: "1.2.3.5:5678",
                result: true,
        },
        {
                name: "static endpoint ok",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                LogLevel: stunnerTestLoglevel,
                        },
                        Auth: v1alpha1.AuthConfig{
                                Type: "plaintext",
                                Credentials: map[string]string{
                                        "username": "user1",
                                        "password": "passwd1",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "udp",
                                Protocol: "udp",
                                Addr: "1.2.3.4",
                                Port: 3478,
                                Routes: []string{
                                        "echo_server_cluster",
                                },
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "echo_server_cluster",
                                Type: "STATIC",
                                Endpoints: []string{
                                        "1.2.3.5",
                                },
                        }},
                },
                echoServerAddr: "1.2.3.5:5678",
                result: true,
        },
        {
                name: "static endpoint with wrong peer addr: fail",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                LogLevel: stunnerTestLoglevel,
                        },
                        Auth: v1alpha1.AuthConfig{
                                Type: "plaintext",
                                Credentials: map[string]string{
                                        "username": "user1",
                                        "password": "passwd1",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "udp",
                                Protocol: "udp",
                                Addr: "1.2.3.4",
                                Port: 3478,
                                Routes: []string{
                                        "echo_server_cluster",
                                },
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "echo_server_cluster",
                                Type: "STATIC",
                                Endpoints: []string{
                                        "1.2.3.6",
                                },
                        }},
                },
                echoServerAddr: "1.2.3.5:5678",
                result: false,
        },       
        {
                name: "static endpoint with multiple routes ok",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                LogLevel: stunnerTestLoglevel,
                        },
                        Auth: v1alpha1.AuthConfig{
                                Type: "plaintext",
                                Credentials: map[string]string{
                                        "username": "user1",
                                        "password": "passwd1",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "udp",
                                Protocol: "udp",
                                Addr: "1.2.3.4",
                                Port: 3478,
                                Routes: []string{
                                        "echo_server_cluster",
                                        "dummy_cluster",
                                },
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "echo_server_cluster",
                                Type: "STATIC",
                                Endpoints: []string{
                                        "1.2.3.5",
                                },
                        },{
                                Name: "dummy_cluster",
                                Type: "STATIC",
                                Endpoints: []string{
                                        "9.8.7.6",
                                },
                        }},
                },
                echoServerAddr: "1.2.3.5:5678",
                result: true,
        },        
        {
                name: "static endpoint with multiple routes and wrong peer addr fail",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                LogLevel: stunnerTestLoglevel,
                        },
                        Auth: v1alpha1.AuthConfig{
                                Type: "plaintext",
                                Credentials: map[string]string{
                                        "username": "user1",
                                        "password": "passwd1",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "udp",
                                Protocol: "udp",
                                Addr: "1.2.3.4",
                                Port: 3478,
                                Routes: []string{
                                        "dummy_cluster",
                                        "echo_server_cluster",
                                },
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "echo_server_cluster",
                                Type: "STATIC",
                                Endpoints: []string{
                                        "1.2.3.6",
                                },
                        },{
                                Name: "dummy_cluster",
                                Type: "STATIC",
                                Endpoints: []string{
                                        "9.8.7.6",
                                },
                        }},
                },
                echoServerAddr: "1.2.3.5:5678",
                result: false,
        },        
        {
                name: "static endpoint with multiple ips ok",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                LogLevel: stunnerTestLoglevel,
                        },
                        Auth: v1alpha1.AuthConfig{
                                Type: "plaintext",
                                Credentials: map[string]string{
                                        "username": "user1",
                                        "password": "passwd1",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "udp",
                                Protocol: "udp",
                                Addr: "1.2.3.4",
                                Port: 3478,
                                Routes: []string{
                                        "echo_server_cluster",
                                },
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "echo_server_cluster",
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
                result: true,
        },
        {
                name: "static endpoint with multiple ips with wrong peer addr fail",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                LogLevel: stunnerTestLoglevel,
                        },
                        Auth: v1alpha1.AuthConfig{
                                Type: "plaintext",
                                Credentials: map[string]string{
                                        "username": "user1",
                                        "password": "passwd1",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "udp",
                                Protocol: "udp",
                                Addr: "1.2.3.4",
                                Port: 3478,
                                Routes: []string{
                                        "echo_server_cluster",
                                },
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "echo_server_cluster",
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
                result: false,
        },
        // {
        //         name: "strict_dns ok",
        //         config: v1alpha1.StunnerConfig{
        //                 ApiVersion: "v1alpha1",
        //                 Admin: v1alpha1.AdminConfig{
        //                         LogLevel: stunnerTestLoglevel,
        //                 },
        //                 Auth: v1alpha1.AuthConfig{
        //                         Type: "plaintext",
        //                         Credentials: map[string]string{
        //                                 "username": "user1",
        //                                 "password": "passwd1",
        //                         },
        //                 },
        //                 Listeners: []v1alpha1.ListenerConfig{{
        //                         Name: "udp",
        //                         Protocol: "udp",
        //                         Addr: "1.2.3.4",
        //                         Port: 3478,
        //                         Routes: []string{
        //                                 "echo_server_cluster",
        //                         },
        //                 }},
        //                 Clusters: []v1alpha1.ClusterConfig{{
        //                         Name: "echo_server_cluster",
        //                         Type: "STRICT_DNS",
        //                         Endpoints: []string{
        //                                 "echo-server.l7mp.io",
        //                         },
        //                 }},
        //         },
        //         echoServerAddr: "echo-server.l7mp.io:5678",
        //         result: true,
        // },
        // {
        //         name: "strict_dns cluster and wrong peer addr fail",
        //         config: v1alpha1.StunnerConfig{
        //                 ApiVersion: "v1alpha1",
        //                 Admin: v1alpha1.AdminConfig{
        //                         LogLevel: stunnerTestLoglevel,
        //                 },
        //                 Auth: v1alpha1.AuthConfig{
        //                         Type: "plaintext",
        //                         Credentials: map[string]string{
        //                                 "username": "user1",
        //                                 "password": "passwd1",
        //                         },
        //                 },
        //                 Listeners: []v1alpha1.ListenerConfig{{
        //                         Name: "udp",
        //                         Protocol: "udp",
        //                         Addr: "1.2.3.4",
        //                         Port: 3478,
        //                         Routes: []string{
        //                                 "echo_server_cluster",
        //                         },
        //                 }},
        //                 Clusters: []v1alpha1.ClusterConfig{{
        //                         Name: "echo_server_cluster",
        //                         Type: "STRICT_DNS",
        //                         Endpoints: []string{
        //                                 "echo-server.l7mp.io",
        //                         },
        //                 }},
        //         },
        //         echoServerAddr: "1.2.3.10:5678",
        //         result: false,
        // },
}

func TestStunnerClusterWithVNet(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, c := range testClusterConfigsWithVNet {
		auth := c.config.Auth.Type
		testName := fmt.Sprintf("TestStunnerClusterWithVNet:%s", auth)
		t.Run(testName, func(t *testing.T) {
                     log.Debugf("-------------- Running test: %s -------------", testName)

			// patch in the vnet
			log.Debug("building virtual network")
			v, err := buildVNet(loggerFactory)
			assert.NoError(t, err, err)
			c.config.Net = v.podnet

			log.Debug("creating a stunnerd")
			stunner, err := NewStunner(&c.config)
			assert.NoError(t, err, err)

			log.Debug("starting stunnerd")
			assert.NoError(t, stunner.Start())

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

			testConfig := echoTestConfig{t, v.podnet, v.wan, stunner,
				"stunner.l7mp.io:3478", lconn, u, p, net.IPv4(5, 6, 7, 8),
				c.echoServerAddr, c.result, loggerFactory}
			stunnerEchoTest(testConfig)

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")
			stunner.Close()
			assert.NoError(t, v.Close(), "cannot close VNet")
		})
	}
}

// *****************
// Reconciliation tests
// *****************
type StunnerReconcileTestConfig struct {
        name string
        config v1alpha1.StunnerConfig
        tester func(t *testing.T, s *Stunner, err error)
}
        
var testReconcileDefault = []StunnerReconcileTestConfig{
        {
                name: "reconcile-test: default admin",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "no restart needed")

                        assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
                        admin := s.GetAdmin()
                        assert.Equal(t, admin.Name, v1alpha1.DefaultStunnerName, "stunner name")
                        // make sure we get the right loglevel, we may override this for debugging the tests
                        // assert.Equal(t, admin.LogLevel, v1alpha1.DefaultLogLevel, "stunner loglevel")

                        assert.Len(t, s.authManager.Keys(), 1, "authManager keys")
                        auth := s.GetAuth()
                        assert.Equal(t, auth.Type, v1alpha1.AuthTypePlainText, "auth type ok")

                        assert.Equal(t, auth.Username, "user", "username ok")
                        assert.Equal(t, auth.Password, "pass", "password ok")

                        key, ok := auth.Handler("user", v1alpha1.DefaultRealm,
                                &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
                        assert.True(t, ok, "authHandler key ok")
                        assert.Equal(t, key, turn.GenerateAuthKey("user",
                                v1alpha1.DefaultRealm, "pass"), "auth handler ok")
                        
                        assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")
                        
                        l := s.GetListener("default-listener")
                        assert.NotNil(t, l, "listener found")
                        assert.IsType(t, l, &object.Listener{}, "listener type ok")

                        assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUdp, "listener proto ok")
                        assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
                        assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
                        assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
                        assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
                        assert.Len(t, l.Routes, 1, "listener route count ok")
                        assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

                        assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

                        c := s.GetCluster("allow-any")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("0.0.0.0/0")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

                        // listener  uses the open cluster for routing

                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
                },
        },
        {
                name: "reconcile-test: empty credentials errs: user",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
			assert.ErrorContains(t, err, "empty username or password")
                },
        },
        {
                name: "reconcile-test: empty credentials errs: passwd",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
			assert.ErrorContains(t, err, "empty username or password")
                },
        },
        {
                name: "reconcile-test: empty listener is fine",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
                        // deleting a listener requires a restart
			assert.ErrorIs(t, err, v1alpha1.ErrRestartRequired, "restart required")
                },
        },
        {
                name: "reconcile-test: empty listener name errs",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
			assert.ErrorContains(t, err, "missing name")
                },
        },
        {
                name: "reconcile-test: empty cluster is fine",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, "no restart needed")
                },
        },
        {
                name: "reconcile-test: empty cluster name errs",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
			assert.ErrorContains(t, err, "missing name", "missing username")
                },
        },
        ////////////// reconcile tests
        /// admin
        {
                name: "reconcile-test: reconcile name",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                Name: "new-name",
                        },
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
                        // no restart!
			assert.NoError(t, err, "no restart needed")

                        // check everyting
                        assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
                        admin := s.GetAdmin()
                        assert.Equal(t, admin.Name, "new-name", "stunner name")
                        // assert.Equal(t, admin.LogLevel, v1alpha1.DefaultLogLevel, "stunner loglevel")

                        assert.Len(t, s.authManager.Keys(), 1, "authManager keys")
                        auth := s.GetAuth()
                        assert.Equal(t, auth.Type, v1alpha1.AuthTypePlainText, "auth type ok")

                        assert.Equal(t, auth.Username, "user", "username ok")
                        assert.Equal(t, auth.Password, "pass", "password ok")

                        key, ok := auth.Handler("user", v1alpha1.DefaultRealm,
                                &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
                        assert.True(t, ok, "authHandler key ok")
                        assert.Equal(t, key, turn.GenerateAuthKey("user",
                                v1alpha1.DefaultRealm, "pass"), "auth handler ok")
                        
                        assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")
                        
                        l := s.GetListener("default-listener")
                        assert.NotNil(t, l, "listener found")
                        assert.IsType(t, l, &object.Listener{}, "listener type ok")

                        assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUdp, "listener proto ok")
                        assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
                        assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
                        assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
                        assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
                        assert.Len(t, l.Routes, 1, "listener route count ok")
                        assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

                        assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

                        c := s.GetCluster("allow-any")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("0.0.0.0/0")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

                        // listener  uses the open cluster for routing
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
                },
        },
        {
                name: "reconcile-test: reconcile loglevel",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                LogLevel: "anything",
                        },
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
                        // no restart!
			assert.NoError(t, err, "no restart needed")

                        assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
                        admin := s.GetAdmin()
                        assert.Equal(t, admin.Name, "default-stunnerd", "stunner name")
                        // assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

                        assert.Len(t, s.authManager.Keys(), 1, "authManager keys")
                        auth := s.GetAuth()
                        assert.Equal(t, auth.Type, v1alpha1.AuthTypePlainText, "auth type ok")

                        assert.Equal(t, auth.Username, "user", "username ok")
                        assert.Equal(t, auth.Password, "pass", "password ok")

                        key, ok := auth.Handler("user", v1alpha1.DefaultRealm,
                                &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
                        assert.True(t, ok, "authHandler key ok")
                        assert.Equal(t, key, turn.GenerateAuthKey("user",
                                v1alpha1.DefaultRealm, "pass"), "auth handler ok")
                        
                        assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")
                        
                        l := s.GetListener("default-listener")
                        assert.NotNil(t, l, "listener found")
                        assert.IsType(t, l, &object.Listener{}, "listener type ok")

                        assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUdp, "listener proto ok")
                        assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
                        assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
                        assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
                        assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
                        assert.Len(t, l.Routes, 1, "listener route count ok")
                        assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

                        assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

                        c := s.GetCluster("allow-any")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("0.0.0.0/0")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

                        // listener  uses the open cluster for routing
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
                },
        },
        /// auth
        {
                name: "reconcile-test: reconcile plaintextauth name",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                LogLevel: "anything",
                        },
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "newuser",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
                        // no restart!
			assert.NoError(t, err, "no restart needed")

                        auth := s.GetAuth()
                        assert.Equal(t, auth.Type, v1alpha1.AuthTypePlainText, "auth type ok")

                        assert.Equal(t, auth.Username, "newuser", "username ok")
                        assert.Equal(t, auth.Password, "pass", "password ok")

                        key, ok := auth.Handler("newuser", v1alpha1.DefaultRealm,
                                &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
                        assert.True(t, ok, "authHandler key ok")
                        assert.Equal(t, key, turn.GenerateAuthKey("newuser",
                                v1alpha1.DefaultRealm, "pass"), "auth handler ok")

                        assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
                        admin := s.GetAdmin()
                        assert.Equal(t, admin.Name, v1alpha1.DefaultStunnerName, "stunner name")
                        // assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

                        assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")
                        
                        l := s.GetListener("default-listener")
                        assert.NotNil(t, l, "listener found")
                        assert.IsType(t, l, &object.Listener{}, "listener type ok")

                        assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUdp, "listener proto ok")
                        assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
                        assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
                        assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
                        assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
                        assert.Len(t, l.Routes, 1, "listener route count ok")
                        assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

                        assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

                        c := s.GetCluster("allow-any")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("0.0.0.0/0")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

                        // listener  uses the open cluster for routing
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
                },
        },
        {
                name: "reconcile-test: reconcile plaintext auth passwd",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                LogLevel: "anything",
                        },
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "newpass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
                        // no restart!
			assert.NoError(t, err, "no restart needed")

                        auth := s.GetAuth()
                        assert.Equal(t, auth.Type, v1alpha1.AuthTypePlainText, "auth type ok")

                        assert.Equal(t, auth.Username, "user", "username ok")
                        assert.Equal(t, auth.Password, "newpass", "password ok")
                        
                        key, ok := auth.Handler("user", v1alpha1.DefaultRealm,
                                &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
                        assert.True(t, ok, "authHandler key ok")
                        assert.Equal(t, key, turn.GenerateAuthKey("user",
                                v1alpha1.DefaultRealm, "newpass"), "auth handler ok")

                        assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
                        admin := s.GetAdmin()
                        assert.Equal(t, admin.Name, v1alpha1.DefaultStunnerName, "stunner name")
                        // assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

                        assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")
                        
                        l := s.GetListener("default-listener")
                        assert.NotNil(t, l, "listener found")
                        assert.IsType(t, l, &object.Listener{}, "listener type ok")

                        assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUdp, "listener proto ok")
                        assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
                        assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
                        assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
                        assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
                        assert.Len(t, l.Routes, 1, "listener route count ok")
                        assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

                        assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

                        c := s.GetCluster("allow-any")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("0.0.0.0/0")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

                        // listener  uses the open cluster for routing
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
                },
        },
        {
                name: "reconcile-test: reconcile longterm auth",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{
                                LogLevel: "anything",
                        },
                        Auth: v1alpha1.AuthConfig{
                                Type: "longterm",
                                Credentials: map[string]string{
                                        "secret": "newsecret",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
                        // no restart!
			assert.NoError(t, err, "no restart needed")

                        auth := s.GetAuth()
                        assert.Equal(t, auth.Type, v1alpha1.AuthTypeLongTerm, "auth type ok")
                        assert.Equal(t, auth.Secret, "newsecret")

                        logger := NewLoggerFactory(stunnerTestLoglevel)
                        handler := turn.NewLongTermAuthHandler("newsecret", logger.NewLogger("test-auth"))
                        duration, _ := time.ParseDuration("10h")
                        d := time.Now().Add(duration).Unix()
                        username := strconv.FormatInt(d, 10)

                        key, ok := auth.Handler(username, v1alpha1.DefaultRealm,
                                &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
                        assert.True(t, ok, "authHandler key ok")
                        
                        key2, ok2 := handler(username, v1alpha1.DefaultRealm,
                                &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234})
                        assert.True(t, ok2)
                        assert.Equal(t, key, key2)

                        assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
                        admin := s.GetAdmin()
                        assert.Equal(t, admin.Name, v1alpha1.DefaultStunnerName, "stunner name")
                        // assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

                        assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")
                        
                        l := s.GetListener("default-listener")
                        assert.NotNil(t, l, "listener found")
                        assert.IsType(t, l, &object.Listener{}, "listener type ok")

                        assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUdp, "listener proto ok")
                        assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
                        assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
                        assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
                        assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
                        assert.Len(t, l.Routes, 1, "listener route count ok")
                        assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

                        assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

                        c := s.GetCluster("allow-any")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("0.0.0.0/0")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

                        // listener  uses the open cluster for routing
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
                },
        },
        /// listener
        {
                name: "reconcile-test: reconcile existing listener",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Protocol: "tcp",
                                Addr: "127.0.0.2",
                                Port: 12345,
                                MinRelayPort: 10,
                                MaxRelayPort: 100,
                                Routes: []string{"none", "dummy"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
                        // requires a restart!
			assert.ErrorIs(t, err, v1alpha1.ErrRestartRequired, "restart required")

                        assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")
                        
                        l := s.GetListener("default-listener")
                        assert.NotNil(t, l, "listener found")
                        assert.IsType(t, l, &object.Listener{}, "listener type ok")

                        assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolTcp, "listener proto ok")
                        assert.Equal(t, l.Addr.String(), "127.0.0.2", "listener address ok")
                        assert.Equal(t, l.Port, 12345, "listener port ok")
                        assert.Equal(t, l.MinPort, 10, "listener minport ok")
                        assert.Equal(t, l.MaxPort, 100, "listener maxport ok")
                        assert.Len(t, l.Routes, 2, "listener route count ok")
                        // sorted!!!
                        assert.Equal(t, l.Routes[0], "dummy", "listener route name ok")
                        assert.Equal(t, l.Routes[1], "none", "listener route name ok")

                        assert.Len(t, s.adminManager.Keys(), 1, "adminManager keys")
                        admin := s.GetAdmin()
                        assert.Equal(t, admin.Name, v1alpha1.DefaultStunnerName, "stunner name")
                        // assert.Equal(t, admin.LogLevel, "anything", "stunner loglevel")

                        assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

                        c := s.GetCluster("allow-any")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("0.0.0.0/0")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

                        // listener uses the old cluster for routing
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")
                },
        },
        {
                name: "reconcile-test: reconcile new listener",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "newlistener",
                                Protocol: "tcp",
                                Addr: "127.0.0.2",
                                Port: 1,
                                MinRelayPort: 10,
                                MaxRelayPort: 100,
                                Routes: []string{"none", "dummy"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
                        // requires a restart!
			assert.ErrorIs(t, err, v1alpha1.ErrRestartRequired, "restart required")

                        assert.Len(t, s.listenerManager.Keys(), 1, "listenerManager keys")
                        
                        l := s.GetListener("default-listener")
                        assert.Nil(t, l, "listener found")

                        l = s.GetListener("newlistener")
                        assert.NotNil(t, l, "listener found")
                        assert.IsType(t, l, &object.Listener{}, "listener type ok")

                        assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolTcp, "listener proto ok")
                        assert.Equal(t, l.Addr.String(), "127.0.0.2", "listener address ok")
                        assert.Equal(t, l.Port, 1, "listener port ok")
                        assert.Equal(t, l.MinPort, 10, "listener minport ok")
                        assert.Equal(t, l.MaxPort, 100, "listener maxport ok")
                        assert.Len(t, l.Routes, 2, "listener route count ok")
                        // sorted!
                        assert.Equal(t, l.Routes[0], "dummy", "listener route name ok")
                        assert.Equal(t, l.Routes[1], "none", "listener route name ok")

                        c := s.GetCluster("allow-any")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("0.0.0.0/0")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

                        // listener uses the old cluster for routing
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")
                },
        },
        {
                name: "reconcile-test: reconcile additional listener",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        },{
                                Name: "newlistener",
                                Protocol: "tcp",
                                Addr: "127.0.0.2",
                                Port: 1,
                                MinRelayPort: 10,
                                MaxRelayPort: 100,
                                Routes: []string{"none", "dummy"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
                        // requires a restart!
			assert.ErrorIs(t, err, v1alpha1.ErrRestartRequired, "restart required")

                        assert.Len(t, s.listenerManager.Keys(), 2, "listenerManager keys")

                        l := s.GetListener("default-listener")
                        assert.NotNil(t, l, "listener found")
                        assert.IsType(t, l, &object.Listener{}, "listener type ok")
                        assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolUdp, "listener proto ok")
                        assert.Equal(t, l.Addr.String(), "127.0.0.1", "listener address ok")
                        assert.Equal(t, l.Port, v1alpha1.DefaultPort, "listener port ok")
                        assert.Equal(t, l.MinPort, v1alpha1.DefaultMinRelayPort, "listener minport ok")
                        assert.Equal(t, l.MaxPort, v1alpha1.DefaultMaxRelayPort, "listener maxport ok")
                        assert.Len(t, l.Routes, 1, "listener route count ok")
                        assert.Equal(t, l.Routes[0], "allow-any", "listener route name ok")

                        c := s.GetCluster("allow-any")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("0.0.0.0/0")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

                        // listener uses the old cluster for routing
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")

                        l = s.GetListener("newlistener")
                        assert.NotNil(t, l, "listener found")
                        assert.IsType(t, l, &object.Listener{}, "listener type ok")

                        assert.Equal(t, l.Proto, v1alpha1.ListenerProtocolTcp, "listener proto ok")
                        assert.Equal(t, l.Addr.String(), "127.0.0.2", "listener address ok")
                        assert.Equal(t, l.Port, 1, "listener port ok")
                        assert.Equal(t, l.MinPort, 10, "listener minport ok")
                        assert.Equal(t, l.MaxPort, 100, "listener maxport ok")
                        assert.Len(t, l.Routes, 2, "listener route count ok")
                        // sorted!
                        assert.Equal(t, l.Routes[0], "dummy", "listener route name ok")
                        assert.Equal(t, l.Routes[1], "none", "listener route name ok")

                        p = s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")

                },
        },
        {
                name: "reconcile-test: reconcile deleted listener",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
                        // requires a restart!
			assert.ErrorIs(t, err, v1alpha1.ErrRestartRequired, "restart required")

                        l := s.GetListener("default-listener")
                        assert.Nil(t, l, "listener found")

                        l = s.GetListener("newlistener")
                        assert.Nil(t, l, "listener found")
                        assert.IsType(t, l, &object.Listener{}, "listener type ok")

                        assert.Len(t, s.listenerManager.Keys(), 0, "listenerManager keys")
                },
        },
        /// cluster
        {
                name: "reconcile-test: reconcile existing cluster",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "allow-any",
                                Endpoints: []string{"1.1.1.1", "2.2.2.2/8"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

                        assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

                        c := s.GetCluster("allow-any")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 2, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("1.1.1.1/32")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")
                        _, n, _ = net.ParseCIDR("2.2.2.2/8")
                        assert.IsType(t, c.Endpoints[1], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[1].String(), n.String(), "cluster endpoint ok")

                        l := s.GetListener("default-listener")
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")

                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")
                },
        },
        {
                name: "reconcile-test: reconcile new cluster",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "newcluster",
                                Endpoints: []string{"1.1.1.1", "2.2.2.2/8"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

                        assert.Len(t, s.clusterManager.Keys(), 1, "clusterManager keys")

                        c := s.GetCluster("allow-any")
                        assert.Nil(t, c, "cluster found")

                        c = s.GetCluster("newcluster")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 2, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("1.1.1.1/32")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")
                        _, n, _ = net.ParseCIDR("2.2.2.2/8")
                        assert.IsType(t, c.Endpoints[1], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[1].String(), n.String(), "cluster endpoint ok")

                        l := s.GetListener("default-listener")
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")

                        // listener still uses the old cluster for routing
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")
                },
        },
        {
                name: "reconcile-test: reconcile additional cluster",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "newcluster",
                                Endpoints: []string{"1.1.1.1", "2.2.2.2/8"},
                        },{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

                        assert.Len(t, s.clusterManager.Keys(), 2, "clusterManager keys")

                        c := s.GetCluster("allow-any")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("0.0.0.0/0")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

                        l := s.GetListener("default-listener")
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")

                        c = s.GetCluster("newcluster")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 2, "cluster endpoint count ok")
                        _, n, _ = net.ParseCIDR("1.1.1.1/32")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")
                        _, n, _ = net.ParseCIDR("2.2.2.2/8")
                        assert.IsType(t, c.Endpoints[1], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[1].String(), n.String(), "cluster endpoint ok")

                        // listener still uses the old open cluster for routing
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 ok")
                },
        },
        {
                name: "reconcile-test: reconcile additional cluster and reroute",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"newcluster"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{{
                                Name: "newcluster",
                                Endpoints: []string{"1.1.1.1", "2.2.2.2/8"},
                        },{
                                Name: "allow-any",
                                Endpoints: []string{"0.0.0.0/0"},
                        }},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
                        // only routes have changed, we shouldn't need a restart
			assert.NoError(t, err, err)

                        assert.Len(t, s.clusterManager.Keys(), 2, "clusterManager keys")

                        c := s.GetCluster("allow-any")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 1, "cluster endpoint count ok")
                        _, n, _ := net.ParseCIDR("0.0.0.0/0")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")

                        l := s.GetListener("default-listener")
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")

                        c = s.GetCluster("newcluster")
                        assert.NotNil(t, c, "cluster found")
                        assert.IsType(t, c, &object.Cluster{}, "cluster type ok")
                        assert.Equal(t, c.Type, v1alpha1.ClusterTypeStatic, "cluster mode ok")
                        assert.Len(t, c.Endpoints, 2, "cluster endpoint count ok")
                        _, n, _ = net.ParseCIDR("1.1.1.1/32")
                        assert.IsType(t, c.Endpoints[0], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[0].String(), n.String(), "cluster endpoint ok")
                        _, n, _ = net.ParseCIDR("2.2.2.2/8")
                        assert.IsType(t, c.Endpoints[1], *n, "cluster endpoint type ok")
                        assert.Equal(t, c.Endpoints[1].String(), n.String(), "cluster endpoint ok")

                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 ok")
                        assert.True(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 ok")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")
                },
        },
        {
                name: "reconcile-test: reconcile deleted cluster",
                config: v1alpha1.StunnerConfig{
                        ApiVersion: "v1alpha1",
                        Admin: v1alpha1.AdminConfig{},
                        Auth: v1alpha1.AuthConfig{
                                Credentials: map[string]string{
                                        "username": "user",
                                        "password": "pass",
                                },
                        },
                        Listeners: []v1alpha1.ListenerConfig{{
                                Name: "default-listener",
                                Addr: "127.0.0.1",
                                Routes: []string{"allow-any"},
                        }},
                        Clusters: []v1alpha1.ClusterConfig{},
                },
                tester: func(t *testing.T, s *Stunner, err error) {
			assert.NoError(t, err, err)

                        assert.Len(t, s.clusterManager.Keys(), 0, "clusterManager keys")

                        l := s.GetListener("default-listener")
                        p := s.NewPermissionHandler(l)
                        assert.NotNil(t, p, "permission handler exists")

                        // missing cluster, deny all IPs
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.1")), "route to 1.1.1.1 ok")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("1.1.1.2")), "route to 1.1.1.2 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.2.2.2")), "route to 2.2.2.2 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("2.128.3.3")), "route to 2.128.3.3 fails")
                        assert.False(t, p(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234},
                                net.ParseIP("3.0.0.0")), "route to 3.0.0.0 fails")
                },
        },
}

// start with default config and then reconcile with the given config
func TestStunnerReconcile(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, c := range testReconcileDefault {
		t.Run(c.name, func(t *testing.T) {
                        log.Debugf("-------------- Running test: %s -------------", c.name)

			log.Debug("creating a stunnerd")
                        conf, err := NewDefaultStunnerConfig("turn://user:pass@127.0.0.1:3478")
			assert.NoError(t, err, err)

                        conf.Admin.LogLevel = stunnerTestLoglevel
                        
			s, err := NewStunner(conf)
			assert.NoError(t, err, err)

                        runningConf := s.GetConfig()
                        assert.NotNil(t, runningConf, "default stunner get config ok")

                        // fmt.Printf("default conf: %#v\n", conf.Clusters[0])
                        // fmt.Printf("running conf: %#v\n", runningConf.Clusters[0])
                        // x := reflect.DeepEqual(conf.Clusters[0], runningConf.Clusters[0])
                        // fmt.Printf("deepeq: %#v\n", x)
                        // x = conf.Clusters[0].DeepEqual(&runningConf.Clusters[0])
                        // fmt.Printf("deepeqqqqqqq: %#v\n", x)
                        
                        assert.True(t, conf.Admin.DeepEqual(&runningConf.Admin),
                                "default stunner admin config ok")
                        assert.True(t, conf.Auth.DeepEqual(&runningConf.Auth),
                                "default stunner auth config ok")
                        assert.True(t, conf.Listeners[0].DeepEqual(
                                &runningConf.Listeners[0]),"default stunner listener config ok")
                        assert.True(t, conf.Clusters[0].DeepEqual(
                                &runningConf.Clusters[0]),"default stunner cluster config ok")

                        assert.True(t, conf.DeepEqual(runningConf),"default stunner config ok")
                        
                        err = s.Reconcile(&c.config)
                        c.tester(t, s, err)
                        
			s.Close()
		})
	}
}

