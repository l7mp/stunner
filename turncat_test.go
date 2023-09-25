package stunner

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v3/test"
	"github.com/pion/turn/v3"
	"github.com/stretchr/testify/assert"

	"github.com/l7mp/stunner/pkg/logger"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

var turncatTestLoglevel string = "all:ERROR"

// var turncatTestLoglevel string = "all:TRACE"

var sharedSecret = "my-secret"
var defaultDuration = "10m"
var longtermAuthGen = func() (string, string, error) {
	d, _ := time.ParseDuration(defaultDuration)
	return turn.GenerateLongTermCredentials(sharedSecret, d)
}
var plaintextAuthGen = func() (string, string, error) {
	return "user1", "passwd1", nil
}

type turncatEchoTestConfig struct {
	t *testing.T
	// server
	stunner *Stunner
	// client
	lconn net.Conn
	// peer
	peer          net.Addr
	loggerFactory logging.LoggerFactory
}

func turncatEchoTest(conf turncatEchoTestConfig) {
	t := conf.t
	log := conf.loggerFactory.NewLogger("test")

	echoConn, err := net.ListenPacket(conf.peer.Network(), conf.peer.String())
	assert.NoError(t, err, "cannot allocate echo server connection")

	go func() {
		buf := make([]byte, 1600)
		for {
			n, from, err := echoConn.ReadFrom(buf)
			if err != nil {
				break
			}

			assert.Equal(t, "Hello", string(buf[:n]), "wrong message payload")

			// echo the data
			_, err = echoConn.WriteTo(buf[:n], from)
			assert.NoError(t, err, err)
		}
	}()

	buf := make([]byte, 1600)
	for i := 0; i < 8; i++ {
		log.Debug("sending \"Hello\"")
		_, err = conf.lconn.Write([]byte("Hello"))
		assert.NoError(t, err, err)

		_, err2 := conf.lconn.Read(buf)
		assert.NoError(t, err2, err2)

		time.Sleep(100 * time.Millisecond)
	}

	assert.NoError(t, echoConn.Close(), "cannot close echo server connection")
}

/********************************************
 *
 * UDP/TCP tests over localhost (VNet supports UDP only)
 *
 * Topology:
 *                +----- turncat (udp:25000, tcp:25000)
 *                |
 *                +----- STUNner (udp:23478, tcp:23478)
 *                |
 *     client--- lo
 *                |
 *                +----- echo-server (udp:25678)
 *
 *********************************************/

func TestTurncatPlaintext(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	logger := logger.NewLoggerFactory(turncatTestLoglevel)
	log := logger.NewLogger("test")

	log.Debug("creating a stunnerd")
	stunner := NewStunner(Options{
		LogLevel:         turncatTestLoglevel,
		SuppressRollback: true,
	})

	err := stunner.Reconcile(v1alpha1.StunnerConfig{
		ApiVersion: "v1alpha1",
		Admin: v1alpha1.AdminConfig{
			LogLevel:        turncatTestLoglevel,
			MetricsEndpoint: "",
		},
		Auth: v1alpha1.AuthConfig{
			Type: "plaintext",
			Credentials: map[string]string{
				"username": "user1",
				"password": "passwd1",
			},
		},
		Listeners: []v1alpha1.ListenerConfig{{
			Name:     "udp-listener-23478",
			Protocol: "turn-udp",
			Addr:     "127.0.0.1",
			Port:     23478,
			Routes:   []string{"allow-any"},
		}, {
			Name:     "tcp-listener-23478",
			Protocol: "turn-tcp",
			Addr:     "127.0.0.1",
			Port:     23478,
			Routes:   []string{"allow-any"},
		}},
		Clusters: []v1alpha1.ClusterConfig{{
			Name:      "allow-any",
			Endpoints: []string{"0.0.0.0/0"},
		}},
	})

	assert.NoError(t, err, "starting server")
	defer stunner.Close()

	testTurncatConfigs := []TurncatConfig{
		{
			ListenerAddr:  "udp://127.0.0.1:25000",
			ServerAddr:    "turn://127.0.0.1:23478?transport=udp",
			PeerAddr:      "udp://localhost:25678",
			AuthGen:       plaintextAuthGen,
			LoggerFactory: logger,
		},
		{
			ListenerAddr:  "udp://127.0.0.1:25000",
			ServerAddr:    "turn://127.0.0.1:23478?transport=tcp",
			PeerAddr:      "udp://localhost:25678",
			AuthGen:       plaintextAuthGen,
			LoggerFactory: logger,
		},
		{
			ListenerAddr:  "tcp://127.0.0.1:25000",
			ServerAddr:    "turn://127.0.0.1:23478?transport=udp",
			PeerAddr:      "udp://localhost:25678",
			AuthGen:       plaintextAuthGen,
			LoggerFactory: logger,
		},
		{
			ListenerAddr:  "tcp://127.0.0.1:25000",
			ServerAddr:    "turn://127.0.0.1:23478?transport=tcp",
			PeerAddr:      "udp://localhost:25678",
			AuthGen:       plaintextAuthGen,
			LoggerFactory: logger,
		},
	}

	for _, c := range testTurncatConfigs {
		listener, err := url.Parse(c.ListenerAddr)
		assert.NoError(t, err, "cannot parse turncat listener URI")

		server, err := ParseUri(c.ServerAddr)
		assert.NoError(t, err, "cannot parse server URI")

		testName := fmt.Sprintf("TestTurncat_NewTurncat_Plaintext_client:%s_server:%s",
			listener.Scheme, server.Protocol)

		t.Run(testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", testName)

			log.Debug("creating turncat relay")
			turncat, err := NewTurncat(&c)
			assert.NoError(t, err, "cannot create turncat relay")

			lconn, err := net.Dial(turncat.listenerAddr.Network(), turncat.listenerAddr.String())
			assert.NoError(t, err, "cannot create client socket")

			if strings.HasPrefix(turncat.listenerAddr.Network(), "tcp") {
				// prevent "addess already in use" errors: close sends RST
				assert.NoError(t, lconn.(*net.TCPConn).SetLinger(0),
					"cannnot set TCP linger")
			}

			testConfig := turncatEchoTestConfig{t, stunner, lconn, turncat.peerAddr, logger}
			turncatEchoTest(testConfig)

			turncat.Close()
			assert.NoError(t, lconn.Close(), "cannot close client connection")
		})
	}
}

func TestTurncatLongterm(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	logger := logger.NewLoggerFactory(turncatTestLoglevel)
	log := logger.NewLogger("test")

	log.Debug("creating a stunnerd")
	stunner := NewStunner(Options{
		LogLevel:         turncatTestLoglevel,
		SuppressRollback: true,
	})
	err := stunner.Reconcile(v1alpha1.StunnerConfig{
		ApiVersion: "v1alpha1",
		Admin: v1alpha1.AdminConfig{
			LogLevel: turncatTestLoglevel,
		},
		Auth: v1alpha1.AuthConfig{
			Type: "longterm",
			Credentials: map[string]string{
				"secret": sharedSecret,
			},
		},
		Listeners: []v1alpha1.ListenerConfig{{
			Name:     "udp-listener-23478",
			Protocol: "turn-udp",
			Addr:     "127.0.0.1",
			Port:     23478,
			Routes:   []string{"allow-any"},
		}, {
			Name:     "tcp-listener-23478",
			Protocol: "turn-tcp",
			Addr:     "127.0.0.1",
			Port:     23478,
			Routes:   []string{"allow-any"},
		}},
		Clusters: []v1alpha1.ClusterConfig{{
			Name:      "allow-any",
			Endpoints: []string{"0.0.0.0/0"},
		}},
	})
	assert.NoError(t, err, "starting server")
	// assert.NoError(t, err, "cannot set up STUNner daemon")
	defer stunner.Close()

	testTurncatConfigs := []TurncatConfig{
		{
			ListenerAddr:  "udp://127.0.0.1:25000",
			ServerAddr:    "turn://127.0.0.1:23478?transport=udp",
			PeerAddr:      "udp://localhost:25678",
			AuthGen:       longtermAuthGen,
			LoggerFactory: logger,
		},
		{
			ListenerAddr:  "udp://127.0.0.1:25000",
			ServerAddr:    "turn://127.0.0.1:23478?transport=tcp",
			PeerAddr:      "udp://localhost:25678",
			AuthGen:       longtermAuthGen,
			LoggerFactory: logger,
		},
		{
			ListenerAddr:  "tcp://127.0.0.1:25000",
			ServerAddr:    "turn://127.0.0.1:23478?transport=udp",
			PeerAddr:      "udp://localhost:25678",
			AuthGen:       longtermAuthGen,
			LoggerFactory: logger,
		},
		{
			ListenerAddr:  "tcp://127.0.0.1:25000",
			ServerAddr:    "turn://127.0.0.1:23478?transport=tcp",
			PeerAddr:      "udp://localhost:25678",
			AuthGen:       longtermAuthGen,
			LoggerFactory: logger,
		},
	}

	for _, c := range testTurncatConfigs {
		listener, err := url.Parse(c.ListenerAddr)
		assert.NoError(t, err, "cannot parse turncat listener URI")

		server, err := ParseUri(c.ServerAddr)
		assert.NoError(t, err, "cannot parse server URI")

		testName := fmt.Sprintf("TestTurncat_NewTurncat_Longterm_client:%s_server:%s",
			listener.Scheme, server.Protocol)

		t.Run(testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", testName)

			log.Debug("creating turncat relay")
			turncat, err := NewTurncat(&c)
			assert.NoError(t, err, "cannot create turncat relay")

			lconn, err := net.Dial(turncat.listenerAddr.Network(), turncat.listenerAddr.String())
			assert.NoError(t, err, "cannot create client socket")

			if strings.HasPrefix(turncat.listenerAddr.Network(), "tcp") {
				// prevent "addess already in use" errors: close sends RST
				assert.NoError(t, lconn.(*net.TCPConn).SetLinger(0),
					"cannnot set TCP linger")
			}

			testConfig := turncatEchoTestConfig{t, stunner, lconn, turncat.peerAddr, logger}
			turncatEchoTest(testConfig)

			turncat.Close()
			assert.NoError(t, lconn.Close(), "cannot close client connection")
		})
	}
}
