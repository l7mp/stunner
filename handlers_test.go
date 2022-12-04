package stunner

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec,gci
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/pion/transport/test"
	"github.com/pion/turn/v2"
	"github.com/stretchr/testify/assert"

	"github.com/l7mp/stunner/internal/logger"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// *****************
// Auth handler tests with VNet
// *****************

// unfortunately the helper longTermCredentials() is not exported by pion/turn so we have to
// reproduce the same functionality here for testing, but with the added twist that usernames can
// appear anywhere
func longTermCredentials(username string, sharedSecret string) (string, error) {
	mac := hmac.New(sha1.New, []byte(sharedSecret))
	_, err := mac.Write([]byte(username))
	if err != nil {
		return "", err // Not sure if this will ever happen
	}
	password := mac.Sum(nil)
	return base64.StdEncoding.EncodeToString(password), nil
}

type StunnerTestAuthWithVnet struct {
	testName   string
	conf       v1alpha1.StunnerConfig
	authCred   func() (string, string)
	clientAddr string
}

var testStunnerAuthWithVnet = []StunnerTestAuthWithVnet{
	{
		testName:   "plaintext",
		clientAddr: "1.1.1.1",
		conf: v1alpha1.StunnerConfig{
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
				Routes:   []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		authCred: func() (string, string) { return "user1", "passwd1" },
	},
	{
		testName: "longterm - plain timestamp in username",
		conf: v1alpha1.StunnerConfig{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes:   []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		authCred: func() (string, string) {
			u, p, _ := turn.GenerateLongTermCredentials("my-secret", time.Minute)
			return u, p
		},
	},
	{
		testName: "longterm - timestamp:userid in username",
		conf: v1alpha1.StunnerConfig{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes:   []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		authCred: func() (string, string) {
			t := time.Now().Add(time.Minute).Unix()
			u := fmt.Sprintf("%s:%s", "dummy-user-id", strconv.FormatInt(t, 10))
			p, _ := longTermCredentials(u, "my-secret")
			return u, p
		},
	},
	{
		testName: "longterm - userid:timestamp in username",
		conf: v1alpha1.StunnerConfig{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes:   []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		authCred: func() (string, string) {
			t := time.Now().Add(time.Minute).Unix()
			u := fmt.Sprintf("%s:%s", "dummy-user-id", strconv.FormatInt(t, 10))
			p, _ := longTermCredentials(u, "my-secret")
			return u, p
		},
	},
	{
		testName: "longterm - userid:timestamp:ramdom-crap in username",
		conf: v1alpha1.StunnerConfig{
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
				Name:     "udp",
				Protocol: "udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes:   []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		authCred: func() (string, string) {
			t := time.Now().Add(time.Minute).Unix()
			u := fmt.Sprintf("%s:%s:random-crap", "dummy-user-id", strconv.FormatInt(t, 10))
			p, _ := longTermCredentials(u, "my-secret")
			return u, p
		},
	},
}

func TestStunnerAuthServerVNet(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, test := range testStunnerAuthWithVnet {
		t.Run(test.testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", test.testName)
			c := test.conf

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

			log.Debug("starting stunnerd")
			assert.ErrorContains(t, stunner.Reconcile(c), "restart", "starting server")

			log.Debug("creating a client")
			lconn, err := v.wan.ListenPacket("udp4", "0.0.0.0:0")
			assert.NoError(t, err, "cannot create client listening socket")

			u, p := test.authCred()

			testConfig := echoTestConfig{t, v.podnet, v.wan, stunner,
				"stunner.l7mp.io:3478", lconn, u, p, net.IPv4(5, 6, 7, 8),
				"1.2.3.5:5678", true, true, true, loggerFactory}
			stunnerEchoTest(testConfig)

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")
			stunner.Close()
			assert.NoError(t, v.Close(), "cannot close VNet")
		})
	}
}
