package whipconn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"

	slogger "github.com/l7mp/stunner/pkg/logger"
)

var (
	testerLogLevel = "all:ERROR"
	// testerLogLevel = "all:TRACE"
	// testerLogLevel = "all:INFO"
	addr                                = "localhost:12345"
	timeout                             = 5 * time.Second
	interval                            = 50 * time.Millisecond
	defaultConfig                       = Config{BearerToken: "whiptoken"}
	logger        logging.LoggerFactory = slogger.NewLoggerFactory(testerLogLevel)
	log           logging.LeveledLogger = logger.NewLogger("test")
)

func echoTest(t *testing.T, conn net.Conn, content string) {
	t.Helper()

	n, err := conn.Write([]byte(content))
	assert.NoError(t, err)
	assert.Equal(t, len(content), n)

	buf := make([]byte, 2048)
	n, err = conn.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, content, string(buf[:n]))
}

var testerTestCases = []struct {
	name   string
	config *Config
	tester func(t *testing.T, ctx context.Context, l *Listener)
}{
	{
		name: "Basic connectivity",
		tester: func(t *testing.T, ctx context.Context, l *Listener) {
			log.Debug("Creating dialer")
			d := NewDialer(defaultConfig, logger)
			assert.NotNil(t, d)

			log.Debug("Dialing")
			clientConn, err := d.DialContext(ctx, addr)
			assert.NoError(t, err)

			log.Debug("Echo test round 1")
			echoTest(t, clientConn, "test1")
			log.Debug("Echo test round 2")
			echoTest(t, clientConn, "test2")

			assert.NoError(t, clientConn.Close(), "client conn close")
		},
	}, {
		name: "Invalid bearer token refused",
		tester: func(t *testing.T, ctx context.Context, l *Listener) {
			log.Debug("Creating dialer")
			d := NewDialer(Config{BearerToken: "dummy-token"}, logger)
			assert.NotNil(t, d)

			log.Debug("Dialing")
			_, err := d.DialContext(ctx, addr)
			assert.Error(t, err)
		},
	}, {
		name:   "Empty bearer token accepted",
		config: &Config{BearerToken: ""},
		tester: func(t *testing.T, ctx context.Context, l *Listener) {
			log.Debug("Creating dialer")
			config := defaultConfig
			config.BearerToken = ""
			d := NewDialer(config, logger)
			assert.NotNil(t, d)

			log.Debug("Dialing")
			clientConn, err := d.DialContext(ctx, addr)
			assert.NoError(t, err)

			log.Debug("Echo test round 1")
			echoTest(t, clientConn, "test1")
			log.Debug("Echo test round 2")
			echoTest(t, clientConn, "test2")

			assert.NoError(t, clientConn.Close(), "client conn close")
		},
	}, {
		name: "Closing dialer does not close client connection",
		tester: func(t *testing.T, serverCtx context.Context, l *Listener) {
			// a new context for the dialer
			dialerCtx, dialerCancel := context.WithCancel(context.Background())

			log.Debug("Creating dialer")
			d := NewDialer(defaultConfig, logger)
			assert.NotNil(t, d)

			log.Debug("Dialing")
			clientConn, err := d.DialContext(dialerCtx, addr)
			assert.NoError(t, err)

			log.Debug("Echo test round 1")
			echoTest(t, clientConn, "test1")

			log.Debug("Closing dialer")
			dialerCancel()

			log.Debug("Echo test round 2")
			echoTest(t, clientConn, "test2")
		},
	}, {
		name: "Client side close closes server",
		tester: func(t *testing.T, serverCtx context.Context, l *Listener) {
			log.Debug("Creating dialer")
			d := NewDialer(defaultConfig, logger)
			assert.NotNil(t, d)

			log.Debug("Dialing")
			clientConn, err := d.DialContext(serverCtx, addr)
			assert.NoError(t, err)

			assert.Eventually(t, func() bool { return len(l.conns) == 1 }, timeout, interval)

			log.Debug("Closing client connection")
			assert.NoError(t, clientConn.Close())

			// should close the server conn too
			assert.Eventually(t, func() bool { return len(l.conns) == 0 }, timeout, interval)
		},
	}, {
		name: "Server side close closes client",
		tester: func(t *testing.T, serverCtx context.Context, l *Listener) {
			clientCtx, clientCancel := context.WithCancel(context.Background())
			defer clientCancel()

			log.Debug("Creating dialer")
			d := NewDialer(defaultConfig, logger)
			assert.NotNil(t, d)

			log.Debug("Dialing")
			clientConn, err := d.DialContext(clientCtx, addr)
			assert.NoError(t, err)

			assert.Eventually(t, func() bool { return len(l.conns) == 1 }, timeout, interval)

			log.Debug("Closing server connections")
			for _, lConn := range l.GetConns() {
				assert.NoError(t, lConn.Close())
			}

			assert.Eventually(t, func() bool { return len(l.conns) == 0 }, timeout, interval)

			// should close the client conn too
			assert.Eventually(t, func() bool { return clientConn.(*DialerConn).closed == true }, timeout, interval)
		},
	}, {
		name: "Multiple connections",
		tester: func(t *testing.T, ctx context.Context, l *Listener) {
			log.Debug("Creating dialer")
			d := NewDialer(defaultConfig, logger)
			assert.NotNil(t, d)

			log.Debug("Dialing: creating 5 connections")
			var wg sync.WaitGroup
			wg.Add(5)
			connChan := make(chan net.Conn, 5)
			for i := 0; i < 5; i++ {
				go func() {
					defer wg.Done()

					clientConn, err := d.DialContext(ctx, addr)
					assert.NoError(t, err)

					log.Debug("Echo test round 1")
					echoTest(t, clientConn, "test1111")

					log.Debug("Echo test round 2")
					echoTest(t, clientConn, "test2222")

					connChan <- clientConn
				}()
			}

			wg.Wait()
			close(connChan)

			assert.Eventually(t, func() bool { return len(l.conns) == 5 }, timeout, interval)

			for c := range connChan {
				c.Close()
			}

			assert.Eventually(t, func() bool { return len(l.conns) == 0 }, timeout, interval)
		},
	}, {
		name: "Closing invalid resource fails",
		tester: func(t *testing.T, ctx context.Context, l *Listener) {
			uri := fmt.Sprintf("http://%s/whip/dummy-id", addr)
			req, err := http.NewRequest("DELETE", uri, nil)
			assert.NoError(t, err)
			req.Header.Add("Authorization", "Bearer "+defaultConfig.BearerToken)

			r, err := http.DefaultClient.Do(req)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusNotFound, r.StatusCode)
		},
	}, {
		name:   "Connecting with custom path",
		config: &Config{WHIPEndpoint: "/custompath"},
		tester: func(t *testing.T, ctx context.Context, l *Listener) {
			log.Debug("Creating dialer")
			d := NewDialer(Config{WHIPEndpoint: "/custompath"}, logger)
			assert.NotNil(t, d)

			log.Debug("Dialing")
			clientConn, err := d.DialContext(ctx, addr)
			assert.NoError(t, err)

			log.Debug("Echo test round 1")
			echoTest(t, clientConn, "test1")
			log.Debug("Echo test round 2")
			echoTest(t, clientConn, "test2")

			assert.NoError(t, clientConn.Close(), "client conn close")
		},
	}, {
		name: "Failed datachannel connection fails Dial",
		tester: func(t *testing.T, ctx context.Context, l *Listener) {
			log.Debug("Creating dialer")
			// empty ICE servers and forcing relay policy will fail the dialer
			d := NewDialer(Config{
				ICEServers:         []webrtc.ICEServer{},
				ICETransportPolicy: webrtc.ICETransportPolicyRelay,
				BearerToken:        "whiptoken",
			}, logger)
			assert.NotNil(t, d)

			// set agggressive timeouts
			e := webrtc.SettingEngine{}
			e.SetICETimeouts(500*time.Millisecond, 1000*time.Millisecond, time.Second)
			d.WithSettingEngine(e)

			log.Debug("Dialing")
			_, err := d.DialContext(ctx, addr)
			assert.Error(t, err)
		},
	},
}

func TestTesterConn(t *testing.T) {
	for _, c := range testerTestCases {
		var l *Listener

		t.Run(c.name, func(t *testing.T) {
			log.Infof("--------------------- %s ----------------------", c.name)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			config := defaultConfig
			if c.config != nil {
				config = *c.config
			}

			log.Debug("Creating listener")
			listener, err := NewListener(addr, config, logger)
			assert.NoError(t, err)
			l = listener
			assert.NotNil(t, l)

			log.Debug("Creating echo services")
			go func() {
				for {
					conn, err := l.Accept()
					if err != nil {
						return
					}

					log.Debug("Accepting server connection")

					// readloop
					go func() {
						buf := make([]byte, 100)
						for {
							n, err := conn.Read(buf)
							if err != nil {
								return
							}

							_, err = conn.Write(buf[:n])
							assert.NoError(t, err)
						}
					}()
				}
			}()

			c.tester(t, ctx, l)

			l.Close() //nolint
		})
	}
}

func TestConfigEndpoint(t *testing.T) {
	log.Debug("Creating listener")
	l, err := NewListener(addr, defaultConfig, logger)
	assert.NoError(t, err)
	assert.NotNil(t, l)

	uri := "http://" + addr + "/config"
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	req.Header.Add("Content-Type", "application/json")
	assert.NoError(t, err)
	res, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)

	config := Config{}
	err = json.NewDecoder(res.Body).Decode(&config)
	assert.NoError(t, err)
	assert.Equal(t, defaultConfig.ICEServers, config.ICEServers)
	assert.Equal(t, defaultConfig.ICETransportPolicy, config.ICETransportPolicy)
	assert.Equal(t, defaultConfig.BearerToken, config.BearerToken)
	assert.Equal(t, WhipEndpoint, config.WHIPEndpoint)

	ss := []webrtc.ICEServer{{
		URLs:       []string{"a", "b"},
		Username:   "test-user-1",
		Credential: "test-passwd-1r",
	}, {
		URLs:       []string{"c", "d"},
		Username:   "test-user-2",
		Credential: "test-passwd-2",
	}}

	config = Config{
		ICEServers:         ss,
		ICETransportPolicy: webrtc.ICETransportPolicyRelay,
		BearerToken:        "some-token",
		WHIPEndpoint:       "will-be-ignored",
	}

	b, err := json.Marshal(config)
	assert.NoError(t, err)
	_, err = http.Post(uri, "application/json", bytes.NewReader(b))
	assert.NoError(t, err)

	req, err = http.NewRequest(http.MethodGet, uri, nil)
	req.Header.Add("Content-Type", "application/json")
	assert.NoError(t, err)
	res, err = http.DefaultClient.Do(req)
	assert.NoError(t, err)

	newConfig := Config{}
	err = json.NewDecoder(res.Body).Decode(&newConfig)
	assert.NoError(t, err)
	assert.Equal(t, config.ICEServers, newConfig.ICEServers)
	assert.Equal(t, config.ICETransportPolicy, newConfig.ICETransportPolicy)
	assert.Equal(t, config.BearerToken, newConfig.BearerToken)
	assert.Equal(t, WhipEndpoint, newConfig.WHIPEndpoint)

	l.Close() //nolint
}
