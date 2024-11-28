package main

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/l7mp/stunner/pkg/logger"
	"github.com/l7mp/stunner/pkg/whipconn"
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
			log.Debug("Creating dialer")
			d := whipconn.NewDialer(defaultConfig, loggerFactory)
			assert.NotNil(t, d)

			log.Debug("Dialing")
			clientConn, err := d.DialContext(ctx, defaultICETesterAddr)
			assert.NoError(t, err)

			log.Debug("Echo test round 1")
			echoTest(t, clientConn, "test1")
			log.Debug("Echo test round 2")
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

			log.Debug("Running listener loop")
			go func() {
				err := runICETesterListener(ctx, defaultICETesterAddr, config)
				assert.NoError(t, err)
			}()

			c.tester(t, ctx)
		})
	}
}
