package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/go-openapi/testify/v2/require"
	"github.com/stretchr/testify/assert"

	"github.com/l7mp/stunner/v2/pkg/logger"
	"github.com/l7mp/stunner/v2/pkg/whipconn"
)

var (
	testerLogLevel = "all:WARN"
	// testerLogLevel = "all:TRACE"
	// testerLogLevel = "all:INFO"
	defaultConfig = whipconn.Config{BearerToken: "whiptoken"}
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
	config *whipconn.Config
	tester func(t *testing.T, ctx context.Context)
}{
	{
		name: "Basic connectivity",
		tester: func(t *testing.T, ctx context.Context) {
			log.Debug("creating dialer")
			d := whipconn.NewDialer(defaultConfig, loggerFactory)
			assert.NotNil(t, d)

			log.Debug("dialing")
			// the listener starts in the background: retry until it is up
			var clientConn net.Conn
			require.Eventually(t, func() bool {
				c, err := d.DialContext(ctx, defaultICETesterAddr)
				clientConn = c
				return err == nil
			}, 5*time.Second, 50*time.Millisecond, "dial")

			log.Debug("echo test round 1")
			echoTest(t, clientConn, "test1")
			log.Debug("echo test round 2")
			echoTest(t, clientConn, "test2")

			assert.NoError(t, clientConn.Close(), "client conn close")
		},
	},
}

func TestICETesterConn(t *testing.T) {
	loggerFactory = logger.NewLoggerFactory(testerLogLevel)
	log = loggerFactory.NewLogger("icester")

	for _, c := range testerTestCases {
		t.Run(c.name, func(t *testing.T) {
			log.Infof("--------------------- %s ----------------------", c.name)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			config := defaultConfig
			if c.config != nil {
				config = *c.config
			}

			log.Debug("running listener loop")
			go func() {
				err := runICETesterListener(ctx, defaultICETesterAddr, config)
				assert.NoError(t, err)
			}()

			c.tester(t, ctx)
		})
	}
}
