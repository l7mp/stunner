package stunner

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/zapr"
	"github.com/gorilla/websocket"
	"github.com/pion/transport/v3/test"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/yaml"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	cdsclient "github.com/l7mp/stunner/pkg/config/client"
	cdsserver "github.com/l7mp/stunner/pkg/config/server"
	"github.com/l7mp/stunner/pkg/logger"
)

var testerLogLevel = zapcore.Level(-10)

// var testerLogLevel = zapcore.DebugLevel
// var testerLogLevel = zapcore.ErrorLevel

/********************************************
 *
 * default-config
 *
 *********************************************/
func TestStunnerDefaultServerVNet(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	// loggerFactory := logger.NewLoggerFactory("all:TRACE")
	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, conf := range []string{
		"turn://user1:passwd1@1.2.3.4:3478?transport=udp",
		"turn://user1:passwd1@1.2.3.4?transport=udp",
		"turn://user1:passwd1@1.2.3.4:3478",
	} {
		testName := fmt.Sprintf("TestStunner_NewDefaultConfig_URI:%s", conf)
		t.Run(testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", testName)

			log.Debug("creating default stunner config")
			c, err := NewDefaultConfig(conf)
			assert.NoError(t, err, err)

			// patch in the loglevel
			c.Admin.LogLevel = stunnerTestLoglevel

			checkDefaultConfig(t, c, "TURN-UDP")

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
			assert.NoError(t, stunner.Reconcile(c), "starting server")

			log.Debug("creating a client")
			lconn, err := v.wan.ListenPacket("udp4", "0.0.0.0:0")
			assert.NoError(t, err, "cannot create client listening socket")

			testConfig := echoTestConfig{t, v.podnet, v.wan, stunner,
				"stunner.l7mp.io:3478", lconn, "user1", "passwd1", net.IPv4(5, 6, 7, 8),
				"1.2.3.5:5678", true, true, true, loggerFactory}
			stunnerEchoTest(testConfig)

			assert.NoError(t, lconn.Close(), "cannot close TURN client connection")
			stunner.Close()
			assert.NoError(t, v.Close(), "cannot close VNet")
		})
	}
}

func TestStunnerConfigFileRoundTrip(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	// loggerFactory := logger.NewLoggerFactory("all:TRACE")
	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test-roundtrip")

	conf := "turn://user1:passwd1@1.2.3.4:3478?transport=udp"
	testName := "TestStunnerConfigFileRoundTrip"
	log.Debugf("-------------- Running test: %s -------------", testName)

	log.Debug("creating default stunner config")
	c, err := NewDefaultConfig(conf)
	assert.NoError(t, err, "default config")

	// patch in the loglevel
	c.Admin.LogLevel = stunnerTestLoglevel

	checkDefaultConfig(t, c, "TURN-UDP")

	file, err2 := yaml.Marshal(c)
	assert.NoError(t, err2, "marschal config fike")

	newConf := &stnrv1.StunnerConfig{}
	err = yaml.Unmarshal(file, newConf)
	assert.NoError(t, err, "unmarshal config from file")
	assert.NoError(t, newConf.Validate(), "validate")

	ok := newConf.DeepEqual(c)
	assert.True(t, ok, "config file roundtrip")
}

// TestStunnerConfigFileWatcher tests the config file watcher
// - init watcher with nonexistent config file
// - write the default config to the config file and check
// - write a wrong config file: we should not receive anything
// - update the config file and check
func TestStunnerConfigFileWatcher(t *testing.T) {
	lim := test.TimeOut(time.Second * 10)
	defer lim.Stop()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test-watcher")

	testName := "TestStunnerConfigFileWatcher"
	log.Debugf("-------------- Running test: %s -------------", testName)

	log.Debug("creating a temp file for config")
	f, err := os.CreateTemp("", "stunner_conf_*.yaml")
	assert.NoError(t, err, "creating temp config file")
	// we just need the filename for now so we remove the file first
	file := f.Name()
	assert.NoError(t, os.Remove(file), "removing temp config file")

	log.Debug("creating a stunnerd")
	stunner := NewStunner(Options{LogLevel: stunnerTestLoglevel})

	log.Debug("starting watcher")
	conf := make(chan *stnrv1.StunnerConfig, 1)
	defer close(conf)

	log.Debug("init watcher with nonexistent config file")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	url := "file://" + file
	err = stunner.WatchConfig(ctx, url, conf, false)
	assert.NoError(t, err, "creating config watcher")

	// nothing should happen here: wait a bit so that the watcher has comfortable time to start
	time.Sleep(50 * time.Millisecond)

	log.Debug("write the default config to the config file and check")
	uri := "turn://user1:passwd1@1.2.3.4:3478?transport=udp"

	log.Debug("creating default stunner config")
	c, err := NewDefaultConfig(uri)
	assert.NoError(t, err, "default config")

	// patch in the loglevel
	c.Admin.LogLevel = stunnerTestLoglevel

	// recreate the temp file and write config
	f, err = os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0644)
	assert.NoError(t, err, "recreate temp config file")
	defer os.Remove(file) //nolint:errcheck

	y, err := yaml.Marshal(c)
	assert.NoError(t, err, "marshal config file")
	err = f.Truncate(0)
	assert.NoError(t, err, "truncate temp file")
	_, err = f.Seek(0, 0)
	assert.NoError(t, err, "seek temp file")
	_, err = f.Write(y)
	assert.NoError(t, err, "write config to temp file")

	// // wait a bit so that the watcher has time to react
	// time.Sleep(50 * time.Millisecond)

	c2, ok := <-conf
	assert.True(t, ok, "config emitted")
	checkDefaultConfig(t, c2, "TURN-UDP")

	log.Debug("write a wrong config file: WatchConfig validates")

	c2.Listeners[0].Protocol = "dummy"
	y, err = yaml.Marshal(c2)
	assert.NoError(t, err, "marshal config file")
	err = f.Truncate(0)
	assert.NoError(t, err, "truncate temp file")
	_, err = f.Seek(0, 0)
	assert.NoError(t, err, "seek temp file")
	_, err = f.Write(y)
	assert.NoError(t, err, "write config to temp file")

	// this makes sure that we do not share anything with ConfigWatch
	c2.Listeners[0].PublicAddr = "AAAAAAAAAAAAAa"

	// we should not read anything so that channel should not br redable
	time.Sleep(50 * time.Millisecond)
	readable := false
	select {
	case _, ok := <-conf:
		readable = ok
	default:
		readable = false
	}
	assert.False(t, readable, "wrong config file does not trigger a watch event")

	log.Debug("update the config file and check")
	c2.Listeners[0].Protocol = "TURN-TCP"
	y, err = yaml.Marshal(c2)
	assert.NoError(t, err, "marshal config file")
	err = f.Truncate(0)
	assert.NoError(t, err, "truncate temp file")
	_, err = f.Seek(0, 0)
	assert.NoError(t, err, "seek temp file")
	_, err = f.Write(y)
	assert.NoError(t, err, "write config to temp file")

	c3 := <-conf
	checkDefaultConfig(t, c3, "TURN-TCP")

	stunner.Close()
}

const (
	testConfigV1   = `{"version":"v1","admin":{"name":"ns1/tester", "loglevel":"all:ERROR"},"auth":{"type":"static","credentials":{"password":"passwd1","username":"user1"}},"listeners":[{"name":"udp","protocol":"turn-udp","address":"1.2.3.4","port":3478,"routes":["echo-server-cluster"]}],"clusters":[{"name":"echo-server-cluster","type":"STATIC","endpoints":["1.2.3.5"]}]}`
	testConfigV1A1 = `{"version":"v1alpha1","admin":{"name":"ns1/tester", "loglevel":"all:ERROR"},"auth":{"type":"ephemeral","credentials":{"secret":"test-secret"}},"listeners":[{"name":"udp","protocol":"turn-udp","address":"1.2.3.4","port":3478,"routes":["echo-server-cluster"]}],"clusters":[{"name":"echo-server-cluster","type":"STATIC","endpoints":["1.2.3.5"]}]}`
)

// test with v1alpha1 and v1
func TestStunnerConfigFileWatcherMultiVersion(t *testing.T) {
	lim := test.TimeOut(time.Second * 10)
	defer lim.Stop()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test-watcher")

	testName := "TestStunnerConfigFileWatcher"
	log.Debugf("-------------- Running test: %s -------------", testName)

	log.Debug("creating a temp file for config")
	f, err := os.CreateTemp("", "stunner_conf_*.yaml")
	assert.NoError(t, err, "creating temp config file")
	// we just need the filename for now so we remove the file first
	file := f.Name()
	assert.NoError(t, os.Remove(file), "removing temp config file")

	log.Debug("creating a stunnerd")
	stunner := NewStunner(Options{LogLevel: stunnerTestLoglevel})

	log.Debug("starting watcher")
	conf := make(chan *stnrv1.StunnerConfig, 1)
	defer close(conf)

	log.Debug("init watcher with nonexistent config file")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	url := "file://" + file
	err = stunner.WatchConfig(ctx, url, conf, false)
	assert.NoError(t, err, "creating config watcher")

	// nothing should happen here: wait a bit so that the watcher has comfortable time to start
	time.Sleep(50 * time.Millisecond)

	log.Debug("write v1 config and check")

	// recreate the temp file and write config
	f, err = os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0644)
	assert.NoError(t, err, "recreate temp config file")
	defer os.Remove(file) //nolint:errcheck

	err = f.Truncate(0)
	assert.NoError(t, err, "truncate temp file")
	_, err = f.Seek(0, 0)
	assert.NoError(t, err, "seek temp file")
	_, err = f.WriteString(testConfigV1)
	assert.NoError(t, err, "write config to temp file")

	c2, ok := <-conf
	assert.True(t, ok, "config emitted")

	assert.Equal(t, stnrv1.ApiVersion, c2.ApiVersion, "version")
	assert.Equal(t, "all:ERROR", c2.Admin.LogLevel, "loglevel")
	assert.True(t, c2.Auth.Type == "static" || c2.Auth.Type == "ephemeral", "loglevel")
	assert.Len(t, c2.Listeners, 1, "listeners len")
	assert.Equal(t, "udp", c2.Listeners[0].Name, "listener name")
	assert.Equal(t, "TURN-UDP", c2.Listeners[0].Protocol, "listener proto")
	assert.Equal(t, 3478, c2.Listeners[0].Port, "listener port")
	assert.Len(t, c2.Listeners[0].Routes, 1, "routes len")
	assert.Equal(t, "echo-server-cluster", c2.Listeners[0].Routes[0], "route name")
	assert.Len(t, c2.Clusters, 1, "clusters len")
	assert.Equal(t, "echo-server-cluster", c2.Clusters[0].Name, "cluster name")
	assert.Equal(t, "STATIC", c2.Clusters[0].Type, "cluster proto")
	assert.Len(t, c2.Clusters[0].Endpoints, 1, "endpoints len")
	assert.Equal(t, "1.2.3.5", c2.Clusters[0].Endpoints[0], "cluster port")

	err = f.Truncate(0)
	assert.NoError(t, err, "truncate temp file")
	_, err = f.Seek(0, 0)
	assert.NoError(t, err, "seek temp file")
	_, err = f.WriteString(testConfigV1A1)
	assert.NoError(t, err, "write config to temp file")

	c2, ok = <-conf
	assert.True(t, ok, "config emitted")

	assert.Equal(t, stnrv1.ApiVersion, c2.ApiVersion, "version")
	assert.Equal(t, "all:ERROR", c2.Admin.LogLevel, "loglevel")
	assert.True(t, c2.Auth.Type == "static" || c2.Auth.Type == "ephemeral", "loglevel")
	assert.Len(t, c2.Listeners, 1, "listeners len")
	assert.Equal(t, "udp", c2.Listeners[0].Name, "listener name")
	assert.Equal(t, "TURN-UDP", c2.Listeners[0].Protocol, "listener proto")
	assert.Equal(t, 3478, c2.Listeners[0].Port, "listener port")
	assert.Len(t, c2.Listeners[0].Routes, 1, "routes len")
	assert.Equal(t, "echo-server-cluster", c2.Listeners[0].Routes[0], "route name")
	assert.Len(t, c2.Clusters, 1, "clusters len")
	assert.Equal(t, "echo-server-cluster", c2.Clusters[0].Name, "cluster name")
	assert.Equal(t, "STATIC", c2.Clusters[0].Type, "cluster proto")
	assert.Len(t, c2.Clusters[0].Endpoints, 1, "endpoints len")
	assert.Equal(t, "1.2.3.5", c2.Clusters[0].Endpoints[0], "cluster port")

	stunner.Close()
}

func TestStunnerConfigPollerMultiVersion(t *testing.T) {
	lim := test.TimeOut(time.Second * 10)
	defer lim.Stop()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test-poller")

	testName := "TestStunnerConfigPoller"
	log.Debugf("-------------- Running test: %s -------------", testName)

	log.Debug("creating a mock CDS server")
	addr := "localhost:63479"
	origin := "ws://" + addr

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &http.Server{Addr: addr}
	defer s.Close() //nolint:errcheck

	http.HandleFunc("/api/v1/configs/ns1/tester",
		func(w http.ResponseWriter, req *http.Request) {
			upgrader := websocket.Upgrader{
				ReadBufferSize:  1024,
				WriteBufferSize: 1024,
			}

			conn, err := upgrader.Upgrade(w, req, nil)
			assert.NoError(t, err, "upgrade HTTP connection")
			defer func() { _ = conn.Close() }()

			// for the pong handler: conn.Close() will kill this
			go func() {
				for {
					_, _, err := conn.ReadMessage()
					if err != nil {
						return
					}
				}
			}()

			conn.SetPingHandler(func(string) error {
				return conn.WriteMessage(websocket.PongMessage, []byte("keepalive"))
			})

			// send v1config
			assert.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(testConfigV1)), "write config v1")

			// send v1config
			assert.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(testConfigV1A1)), "write config v1alpha1")

			select {
			case <-ctx.Done():
			case <-req.Context().Done():
			}

			conn.Close() //nolint:errcheck
		})

	// serve
	go func() {
		_ = s.ListenAndServe()
	}()

	// wait a bit so that the server has time to setup
	time.Sleep(50 * time.Millisecond)

	log.Debug("creating a stunnerd")
	stunner := NewStunner(Options{LogLevel: stunnerTestLoglevel, Name: "ns1/tester"})

	log.Debug("starting watcher")
	conf := make(chan *stnrv1.StunnerConfig, 1)
	defer close(conf)

	log.Debug("init config poller")
	assert.NoError(t, stunner.WatchConfig(ctx, origin, conf, true), "creating config poller")

	c2, ok := <-conf
	assert.True(t, ok, "config emitted")

	assert.Equal(t, stnrv1.ApiVersion, c2.ApiVersion, "version")
	assert.Equal(t, "all:ERROR", c2.Admin.LogLevel, "loglevel")
	assert.True(t, c2.Auth.Type == "static" || c2.Auth.Type == "ephemeral", "loglevel")
	assert.Len(t, c2.Listeners, 1, "listeners len")
	assert.Equal(t, "udp", c2.Listeners[0].Name, "listener name")
	assert.Equal(t, "TURN-UDP", c2.Listeners[0].Protocol, "listener proto")
	assert.Equal(t, 3478, c2.Listeners[0].Port, "listener port")
	assert.Len(t, c2.Listeners[0].Routes, 1, "routes len")
	assert.Equal(t, "echo-server-cluster", c2.Listeners[0].Routes[0], "route name")
	assert.Len(t, c2.Clusters, 1, "clusters len")
	assert.Equal(t, "echo-server-cluster", c2.Clusters[0].Name, "cluster name")
	assert.Equal(t, "STATIC", c2.Clusters[0].Type, "cluster proto")
	assert.Len(t, c2.Clusters[0].Endpoints, 1, "endpoints len")
	assert.Equal(t, "1.2.3.5", c2.Clusters[0].Endpoints[0], "cluster port")

	// next read yields a v1alpha1 config
	c2, ok = <-conf
	assert.True(t, ok, "config emitted")

	assert.Equal(t, stnrv1.ApiVersion, c2.ApiVersion, "version")
	assert.Equal(t, "all:ERROR", c2.Admin.LogLevel, "loglevel")
	assert.True(t, c2.Auth.Type == "static" || c2.Auth.Type == "ephemeral", "loglevel")
	assert.Len(t, c2.Listeners, 1, "listeners len")
	assert.Equal(t, "udp", c2.Listeners[0].Name, "listener name")
	assert.Equal(t, "TURN-UDP", c2.Listeners[0].Protocol, "listener proto")
	assert.Equal(t, 3478, c2.Listeners[0].Port, "listener port")
	assert.Len(t, c2.Listeners[0].Routes, 1, "routes len")
	assert.Equal(t, "echo-server-cluster", c2.Listeners[0].Routes[0], "route name")
	assert.Len(t, c2.Clusters, 1, "clusters len")
	assert.Equal(t, "echo-server-cluster", c2.Clusters[0].Name, "cluster name")
	assert.Equal(t, "STATIC", c2.Clusters[0].Type, "cluster proto")
	assert.Len(t, c2.Clusters[0].Endpoints, 1, "endpoints len")
	assert.Equal(t, "1.2.3.5", c2.Clusters[0].Endpoints[0], "cluster port")

	stunner.Close()
}

func TestStunnerConfigPatcher(t *testing.T) {
	lim := test.TimeOut(time.Second * 10)
	defer lim.Stop()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test-poller")

	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	logger := zlogger.WithName("tester")

	confChan := make(chan *stnrv1.StunnerConfig, 1)
	defer close(confChan) // must be closed after the context is cancelled

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testCDSAddr := "localhost:63479"
	log.Debugf("create server on %s", testCDSAddr)
	// rewrite node address if requested
	patcher := func(conf *stnrv1.StunnerConfig, node string) *stnrv1.StunnerConfig {
		if node != "" {
			for i := range conf.Listeners {
				if conf.Listeners[i].Addr == "STUNNER_NODE_ADDR" {
					conf.Listeners[i].Addr = node
				}
			}
		}
		return conf
	}
	srv := cdsserver.New(testCDSAddr, patcher, logger)
	assert.NotNil(t, srv, "server")
	err = srv.Start(ctx)
	assert.NoError(t, err, "start")

	log.Debug("creating a stunnerd")
	stunner := NewStunner(Options{
		LogLevel:         stunnerTestLoglevel,
		Name:             "ns1/tester",
		NodeName:         "127.1.2.3", // must be a valid IP otherwise reconcile fails
		SuppressRollback: true,
		DryRun:           true,
	})
	defer stunner.Close()

	log.Debug("starting config watcher")
	assert.NoError(t, stunner.WatchConfig(ctx, "ws://"+testCDSAddr, confChan, true), "start")

	log.Debug("starting the reconciler thread")
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case c := <-confChan:
				err := stunner.Reconcile(c)
				if err == ErrRestartRequired {
					continue
				}
				assert.NoError(t, stunner.Reconcile(c), "reconcile")
			}
		}
	}()

	for _, testCase := range []struct {
		name   string
		prep   func(c *stnrv1.StunnerConfig, t *testing.T)
		tester func(c *stnrv1.StunnerConfig) bool
	}{{
		name: "default",
		prep: func(c *stnrv1.StunnerConfig, _ *testing.T) {},
		tester: func(c *stnrv1.StunnerConfig) bool {
			return len(c.Listeners) == 1 && c.Listeners[0].Addr == "0.0.0.0"
		},
	}, {
		name: "default w/ IP",
		prep: func(c *stnrv1.StunnerConfig, _ *testing.T) { c.Listeners[0].Addr = "127.0.0.1" },
		tester: func(c *stnrv1.StunnerConfig) bool {
			return len(c.Listeners) == 1 && c.Listeners[0].Addr == "127.0.0.1"
		},
	}, {
		name: "node rewrite",
		prep: func(c *stnrv1.StunnerConfig, _ *testing.T) { c.Listeners[0].Addr = "STUNNER_NODE_ADDR" },
		tester: func(c *stnrv1.StunnerConfig) bool {
			return len(c.Listeners) == 1 && c.Listeners[0].Addr == "127.1.2.3"
		},
	}} {
		testName := fmt.Sprintf("TestStunner_NewDefaultConfig_URI:%s", testCase.name)
		t.Run(testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", testName)

			config := &stnrv1.StunnerConfig{
				ApiVersion: stnrv1.ApiVersion,
				Admin: stnrv1.AdminConfig{
					LogLevel: stunnerTestLoglevel,
				},
				Auth: stnrv1.AuthConfig{
					Credentials: map[string]string{
						"username": "user",
						"password": "pass",
					},
				},
				Listeners: []stnrv1.ListenerConfig{{
					Name: "default-listener",
					Addr: "0.0.0.0",
				}},
			}
			testCase.prep(config, t)
			assert.NoError(t, srv.UpdateConfig([]cdsserver.Config{{
				Name:      "tester",
				Namespace: "ns1",
				Config:    config,
			}}), "config server update")

			assert.Eventually(t, func() bool { return testCase.tester(stunner.GetConfig()) },
				time.Second, 10*time.Millisecond)
		})
	}
}

func TestStunnerURIParser(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	// loggerFactory := logger.NewLoggerFactory("all:TRACE")
	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, conf := range []struct {
		uri string
		su  StunnerUri
	}{
		// udp
		{"turn://user1:passwd1@1.2.3.4:3478?transport=udp", StunnerUri{"turn-udp", "1.2.3.4", "user1", "passwd1", 3478, nil}},
		{"turn://user1:passwd1@1.2.3.4?transport=udp", StunnerUri{"turn-udp", "1.2.3.4", "user1", "passwd1", 3478, nil}},
		{"turn://user1:passwd1@1.2.3.4:3478", StunnerUri{"turn-udp", "1.2.3.4", "user1", "passwd1", 3478, nil}},
		// tcp
		{"turn://user1:passwd1@1.2.3.4:3478?transport=tcp", StunnerUri{"turn-tcp", "1.2.3.4", "user1", "passwd1", 3478, nil}},
		{"turn://user1:passwd1@1.2.3.4?transport=tcp", StunnerUri{"turn-tcp", "1.2.3.4", "user1", "passwd1", 3478, nil}},
		// tls - old style
		{"turn://user1:passwd1@1.2.3.4:3478?transport=tls", StunnerUri{"turn-tls", "1.2.3.4", "user1", "passwd1", 3478, nil}},
		{"turn://user1:passwd1@1.2.3.4?transport=tls", StunnerUri{"turn-tls", "1.2.3.4", "user1", "passwd1", 443, nil}},
		// tls - RFC style
		{"turns://user1:passwd1@1.2.3.4:3478?transport=tcp", StunnerUri{"turn-tls", "1.2.3.4", "user1", "passwd1", 3478, nil}},
		{"turns://user1:passwd1@1.2.3.4?transport=tcp", StunnerUri{"turn-tls", "1.2.3.4", "user1", "passwd1", 443, nil}},
		// dtls - old style
		{"turn://user1:passwd1@1.2.3.4:3478?transport=dtls", StunnerUri{"turn-dtls", "1.2.3.4", "user1", "passwd1", 3478, nil}},
		{"turn://user1:passwd1@1.2.3.4?transport=dtls", StunnerUri{"turn-dtls", "1.2.3.4", "user1", "passwd1", 443, nil}},
		// dtls - RFC style
		{"turns://user1:passwd1@1.2.3.4:3478?transport=udp", StunnerUri{"turn-dtls", "1.2.3.4", "user1", "passwd1", 3478, nil}},
		{"turns://user1:passwd1@1.2.3.4?transport=udp", StunnerUri{"turn-dtls", "1.2.3.4", "user1", "passwd1", 443, nil}},
		// no cred
		{"turn://1.2.3.4:3478?transport=udp", StunnerUri{"turn-udp", "1.2.3.4", "", "", 3478, nil}},
		{"turn://1.2.3.4?transport=udp", StunnerUri{"turn-udp", "1.2.3.4", "", "", 3478, nil}},
		{"turn://1.2.3.4", StunnerUri{"turn-udp", "1.2.3.4", "", "", 3478, nil}},
	} {
		testName := fmt.Sprintf("TestStunnerURIParser:%s", conf.uri)
		t.Run(testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", testName)
			u, err := ParseUri(conf.uri)
			assert.NoError(t, err, "URI parser")
			assert.Equal(t, strings.ToLower(conf.su.Protocol), strings.ToLower(u.Protocol), "uri protocol")
			assert.Equal(t, conf.su.Address, u.Address, "uri address")
			assert.Equal(t, conf.su.Username, u.Username, "uri username")
			assert.Equal(t, conf.su.Password, u.Password, "uri password")
			assert.Equal(t, conf.su.Port, u.Port, "uri port")
		})
	}
}

// make sure credentials are excempt from env-substitution in ParseConfig
func TestCredentialParser(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")

	for _, testConf := range []struct {
		name               string
		config             []byte
		user, pass, secret string
	}{
		{"plain", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"static","credentials":{"password":"pass","username":"user"}}}`), "user", "pass", ""},
		// user name with $
		{"username_with_leading_$", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"static","credentials":{"password":"pass","username":"$user"}}}`), "$user", "pass", ""},
		{"username_with_trailing_$", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"static","credentials":{"password":"pass","username":"user$"}}}`), "user$", "pass", ""},
		{"username_with_$", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"static","credentials":{"password":"pass","username":"us$er"}}}`), "us$er", "pass", ""},
		// passwd with $
		{"passwd_with_leading_$", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"static","credentials":{"password":"$pass","username":"user"}}}`), "user", "$pass", ""},
		{"passwd_with_trailing_$", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"static","credentials":{"password":"pass$","username":"user"}}}`), "user", "pass$", ""},
		{"passwd_with_$", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"static","credentials":{"password":"pa$ss","username":"user"}}}`), "user", "pa$ss", ""},
		// secret with $
		{"secret_with_leading_$", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"static","credentials":{"secret":"$secret","username":"user"}}}`), "user", "", "$secret"},
		{"secret_with_trailing_$", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"static","credentials":{"secret":"secret$","username":"user"}}}`), "user", "", "secret$"},
		{"secret_with_$", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"static","credentials":{"secret":"sec$ret","username":"user"}}}`), "user", "", "sec$ret"},
	} {
		testName := fmt.Sprintf("TestCredentialParser:%s", testConf.name)
		t.Run(testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", testName)
			c, err := cdsclient.ParseConfig(testConf.config)
			assert.NoError(t, err, "parser")
			assert.Equal(t, testConf.user, c.Auth.Credentials["username"], "username")
			assert.Equal(t, testConf.pass, c.Auth.Credentials["password"], "password")
			assert.Equal(t, testConf.secret, c.Auth.Credentials["secret"], "secret")
		})
	}
}

func checkDefaultConfig(t *testing.T, c *stnrv1.StunnerConfig, proto string) {
	assert.Equal(t, "static", c.Auth.Type, "auth-type")
	assert.Equal(t, "user1", c.Auth.Credentials["username"], "username")
	assert.Equal(t, "passwd1", c.Auth.Credentials["password"], "passwd")
	assert.Len(t, c.Listeners, 1, "listeners len")
	assert.Equal(t, "1.2.3.4", c.Listeners[0].Addr, "listener addr")
	assert.Equal(t, 3478, c.Listeners[0].Port, "listener port")
	assert.Equal(t, proto, c.Listeners[0].Protocol, "listener proto")
	assert.Len(t, c.Clusters, 1, "clusters len")
	assert.Equal(t, "STATIC", c.Clusters[0].Type, "cluster type")
	assert.Len(t, c.Clusters[0].Endpoints, 1, "cluster endpoint len")
	assert.Equal(t, "0.0.0.0/0", c.Clusters[0].Endpoints[0], "endpoint")
}
