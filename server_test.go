package stunner

import (
	"fmt"
	"net"
	// "reflect"
	"testing"
	"time"

	"github.com/pion/transport/test"
	"github.com/pion/turn/v2"
	"github.com/stretchr/testify/assert"

	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// *****************
// Cluster tests with VNet
// *****************
// type StunnerClusterConfig struct {
//         config v1alpha1.StunnerConfig
//         echoServerAddr string
//         result bool
// }
type StunnerTestClusterConfig struct {
	testName       string
	config         v1alpha1.StunnerConfig
	echoServerAddr string
	result         bool
}

var testClusterConfigsWithVNet = []StunnerTestClusterConfig{
	{
		testName: "open ok",
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes:   []string{"echo-server-cluster"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
					"dummy_cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"dummy_cluster",
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
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
		testName: "multiple strict_dns clusters  ok",
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes: []string{
					"stunner-cluster",
					"echo-server-cluster",
				},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
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
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, c := range testClusterConfigsWithVNet {
		t.Run(c.testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", c.testName)

			// patch in the vnet
			log.Debug("building virtual network")
			v, err := buildVNet(loggerFactory)
			assert.NoError(t, err, err)
			c.config.Net = v.podnet

			log.Debug("creating a stunnerd")
			stunner, err := NewStunner(c.config)
			assert.NoError(t, err, err)

			log.Debug("setting up the mock DNS")
			mockDns := &resolver.MockResolver{
				Zone: map[string]([]string){
					"stunner.l7mp.io":     []string{"1.2.3.4"},
					"echo-server.l7mp.io": []string{"1.2.3.5"},
					"dummy.l7mp.io":       []string{"1.2.3.10"},
				}}

			cluster := stunner.GetCluster("echo-server-cluster")
			assert.NotNil(t, cluster, "echo-server-cluster found")

			if cluster.Resolver != nil {
				cluster.Resolver.SetResolver(mockDns)
			}

			log.Debug("starting stunnerd")
			assert.NoError(t, stunner.Start())

			var u, p string
			auth := c.config.Auth.Type
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
				c.echoServerAddr, true, c.result, loggerFactory}
			stunnerEchoTest(testConfig)

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")
			stunner.Close()
			assert.NoError(t, v.Close(), "cannot close VNet")
		})
	}
}
