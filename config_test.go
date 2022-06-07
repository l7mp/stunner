package stunner

import (
	"net"
	"fmt"
	// "reflect"
	"testing"
	"time"

	"github.com/pion/transport/test"
	"github.com/stretchr/testify/assert"
)

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
                testName := fmt.Sprintf("TestStunner_NewDefaultConfig_URI:%s", conf)
		t.Run(testName, func(t *testing.T) {
                        log.Debugf("-------------- Running test: %s -------------", testName)

			log.Debug("creating default stunner config")
			c, err := NewDefaultConfig(conf)
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

