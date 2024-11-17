package whipconn

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/stretchr/testify/assert"

	slogger "github.com/l7mp/stunner/pkg/logger"
)

var (
	testerLogLevel = "all:WARN"
	// testerLogLevel = "all:TRACE"
	// testerLogLevel = "all:INFO"
	addr                                = "localhost:12345"
	timeout                             = 5 * time.Second
	interval                            = 50 * time.Millisecond
	defaultConfig                       = Config{Token: "whiptoken"}
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
			d := NewDialer(Config{Token: "dummy-token"}, logger)
			assert.NotNil(t, d)

			log.Debug("Dialing")
			_, err := d.DialContext(ctx, addr)
			assert.Error(t, err)
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
			for _, lConn := range l.Conns() {
				assert.NoError(t, lConn.Close())
			}

			assert.Eventually(t, func() bool { return len(l.conns) == 0 }, timeout, interval)

			// should close the client conn too
			assert.Eventually(t, func() bool { return clientConn.(*dialerConn).closed == true }, timeout, interval)
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
			req.Header.Add("Authorization", "Bearer "+defaultConfig.Token)

			r, err := http.DefaultClient.Do(req)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusNotFound, r.StatusCode)
		},
	}, {
		name:   "Connecting with custom path",
		config: &Config{Endpoint: "/custompath"},
		tester: func(t *testing.T, ctx context.Context, l *Listener) {
			log.Debug("Creating dialer")
			d := NewDialer(Config{Endpoint: "/custompath"}, logger)
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
