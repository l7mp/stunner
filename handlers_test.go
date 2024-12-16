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

	"github.com/pion/transport/v3/test"
	"github.com/pion/turn/v4"
	"github.com/stretchr/testify/assert"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

/********************************************
 *
 * Auth handler tests with VNet
 *
 *********************************************/

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
	conf       stnrv1.StunnerConfig
	auth       func() (string, string)
	clientAddr string
}

var testStunnerAuthWithVnet = []StunnerTestAuthWithVnet{
	{
		testName:   "static",
		clientAddr: "1.1.1.1",
		conf: stnrv1.StunnerConfig{
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
				Routes:   []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		auth: func() (string, string) { return "user1", "passwd1" },
	},
	{
		testName:   "default auth type: static",
		clientAddr: "1.1.1.1",
		conf: stnrv1.StunnerConfig{
			ApiVersion: stnrv1.ApiVersion,
			Admin: stnrv1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv1.AuthConfig{
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
				Routes:   []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		auth: func() (string, string) { return "user1", "passwd1" },
	},
	{
		testName: "ephemeral - plain timestamp in username",
		conf: stnrv1.StunnerConfig{
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
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes:   []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		auth: func() (string, string) {
			u, p, _ := turn.GenerateLongTermCredentials("my-secret", time.Minute)
			return u, p
		},
	},
	{
		testName: "ephemeral - timestamp:userid in username",
		conf: stnrv1.StunnerConfig{
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
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes:   []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		auth: func() (string, string) {
			t := time.Now().Add(time.Minute).Unix()
			u := fmt.Sprintf("%s:%s", "dummy-user-id", strconv.FormatInt(t, 10))
			p, _ := longTermCredentials(u, "my-secret")
			return u, p
		},
	},
	{
		testName: "ephemeral - userid:timestamp in username",
		conf: stnrv1.StunnerConfig{
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
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes:   []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		auth: func() (string, string) {
			t := time.Now().Add(time.Minute).Unix()
			u := fmt.Sprintf("%s:%s", "dummy-user-id", strconv.FormatInt(t, 10))
			p, _ := longTermCredentials(u, "my-secret")
			return u, p
		},
	},
	{
		testName: "ephemeral - userid:timestamp:ramdom-crap in username",
		conf: stnrv1.StunnerConfig{
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
				Name:     "udp",
				Protocol: "turn-udp",
				Addr:     "1.2.3.4",
				Port:     3478,
				Routes:   []string{"allow-any"},
			}},
			Clusters: []stnrv1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		auth: func() (string, string) {
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
			assert.NoError(t, stunner.Reconcile(&c), "starting server")

			log.Debug("creating a client")
			lconn, err := v.wan.ListenPacket("udp4", "0.0.0.0:0")
			assert.NoError(t, err, "cannot create client listening socket")

			u, p := test.auth()

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
