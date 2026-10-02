package stunner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/go-logr/zapr"
	"github.com/gorilla/websocket"
	"github.com/pion/transport/v5/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/yaml"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	cdsclient "github.com/l7mp/stunner/v2/pkg/config/client"
	cdsserver "github.com/l7mp/stunner/v2/pkg/config/server"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

// var testerLogLevel = zapcore.Level(-10)
// var testerLogLevel = zapcore.DebugLevel
var testerLogLevel = zapcore.ErrorLevel

func mustReadConfig(t *testing.T, ch <-chan *stnrv2.StunnerConfig, timeout time.Duration) *stnrv2.StunnerConfig {
	t.Helper()

	select {
	case c := <-ch:
		return c
	case <-time.After(timeout):
		t.Fatalf("timeout while waiting for config update")
		return nil
	}
}

func mustNotReadConfig(t *testing.T, ch <-chan *stnrv2.StunnerConfig, timeout time.Duration) {
	t.Helper()

	select {
	case c := <-ch:
		t.Fatalf("unexpected config update received: %#v", c)
	case <-time.After(timeout):
	}
}

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
			checkDefaultConfig(t, c, "UDP")

			// patch in the vnet
			log.Debug("building virtual network")
			v, err := buildVNet(loggerFactory)
			assert.NoError(t, err, err)

			log.Debug("creating a stunnerd")
			stunner := NewStunner(Options{
				LogOptions:       LogOptions{Level: stunnerTestLoglevel},
				SuppressRollback: true,
				Net:              v.podnet,
			})

			log.Debug("starting stunnerd")
			require.NoError(t, stunner.Reconcile(c), "starting server")

			log.Debug("creating a client")
			lconn, err := v.wan.ListenPacket("udp4", "0.0.0.0:0")
			assert.NoError(t, err, "cannot create client listening socket")

			testConfig := echoTestConfig{t, v.podnet, v.wan, stunner,
				"stunner.l7mp.io:3478", lconn, "user1", "passwd1", net.IPv4(5, 6, 7, 8),
				"1.2.3.5:6678", true, true, true, loggerFactory, "", nil}
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

	checkDefaultConfig(t, c, "UDP")

	// exercise the optional public_addresses and addresses lists through the round-trip. addresses
	// is given comma-separated (as the k8s downward API delivers status.podIPs); Validate normalizes
	// it to a per-address list, which must then survive the round-trip.
	c.Listeners[0].PublicAddrs = []string{"1.2.3.4", "2001:db8::1"}
	c.Clusters[0].Addrs = []string{"1.2.3.4,2001:db8::1"}
	assert.NoError(t, c.Validate(), "re-validate after setting addrs")
	assert.Equal(t, []string{"1.2.3.4", "2001:db8::1"}, c.Clusters[0].Addrs, "addresses comma-split")

	file, err2 := yaml.Marshal(c)
	assert.NoError(t, err2, "marschal config fike")

	newConf := &stnrv2.StunnerConfig{}
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
	stunner := NewStunner(Options{LogOptions: LogOptions{Level: stunnerTestLoglevel}})

	log.Debug("starting watcher")
	conf := make(chan *stnrv2.StunnerConfig, 1)

	log.Debug("init watcher with nonexistent config file")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	url := "file://" + file
	err = stunner.WatchConfig(ctx, url, conf, false)
	assert.NoError(t, err, "creating config watcher")

	// nothing should happen here: watcher startup should not emit a config
	mustNotReadConfig(t, conf, 50*time.Millisecond)

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

	c2 := mustReadConfig(t, conf, time.Second)
	checkDefaultConfig(t, c2, "UDP")

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

	// wrong config file must not trigger an update
	mustNotReadConfig(t, conf, 50*time.Millisecond)

	log.Debug("update the config file and check")
	c2.Listeners[0].Protocol = "TCP"
	y, err = yaml.Marshal(c2)
	assert.NoError(t, err, "marshal config file")
	err = f.Truncate(0)
	assert.NoError(t, err, "truncate temp file")
	_, err = f.Seek(0, 0)
	assert.NoError(t, err, "seek temp file")
	_, err = f.Write(y)
	assert.NoError(t, err, "write config to temp file")

	c3 := mustReadConfig(t, conf, time.Second)
	checkDefaultConfig(t, c3, "TCP")

	stunner.Close()
}

const (
	testConfigV1 = `{"version":"v1","admin":{"name":"ns1/tester", "loglevel":"all:ERROR"},"auth":{"type":"static","credentials":{"password":"passwd1","username":"user1"}},"listeners":[{"name":"udp","protocol":"turn-udp","address":"1.2.3.4","port":3478,"routes":["echo-server-cluster"]}],"clusters":[{"name":"echo-server-cluster","type":"STATIC","endpoints":["1.2.3.5"]}]}`
	testConfigV2 = `{"version":"v2","admin":{"name":"ns1/tester", "loglevel":"all:ERROR"},"auth":{"type":"ephemeral","credentials":{"secret":"test-secret"}},"listeners":[{"name":"udp","protocol":"UDP","servers":["udp"],"port":3478}],"servers":[{"name":"udp","type":"turn","clusters":["echo-server-cluster"]}],"clusters":[{"name":"echo-server-cluster","type":"STATIC","endpoints":["1.2.3.5"],"protocol":"UDP","addresses":["1.2.3.4"]}]}`
)

// TestStunnerConfigFileWatcherMultiVersion feeds a v1 and a v2 config file to the watcher: both
// arrive as v2.
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
	stunner := NewStunner(Options{LogOptions: LogOptions{Level: stunnerTestLoglevel}})

	log.Debug("starting watcher")
	conf := make(chan *stnrv2.StunnerConfig, 1)

	log.Debug("init watcher with nonexistent config file")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	url := "file://" + file
	err = stunner.WatchConfig(ctx, url, conf, false)
	assert.NoError(t, err, "creating config watcher")

	// nothing should happen here: watcher startup should not emit a config
	mustNotReadConfig(t, conf, 50*time.Millisecond)

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

	c2 := mustReadConfig(t, conf, time.Second)

	checkEchoServerConfig(t, c2)

	err = f.Truncate(0)
	assert.NoError(t, err, "truncate temp file")
	_, err = f.Seek(0, 0)
	assert.NoError(t, err, "seek temp file")
	_, err = f.WriteString(testConfigV2)
	assert.NoError(t, err, "write config to temp file")

	c2 = mustReadConfig(t, conf, time.Second)

	checkEchoServerConfig(t, c2)

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

			// send v2config
			assert.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(testConfigV2)), "write config v2")

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
	stunner := NewStunner(Options{LogOptions: LogOptions{Level: stunnerTestLoglevel}, Name: "ns1/tester"})

	log.Debug("starting watcher")
	conf := make(chan *stnrv2.StunnerConfig, 1)

	log.Debug("init config poller")
	assert.NoError(t, stunner.WatchConfig(ctx, origin, conf, true), "creating config poller")

	c2 := mustReadConfig(t, conf, time.Second)

	checkEchoServerConfig(t, c2)

	// next read yields the v2 config
	c2 = mustReadConfig(t, conf, time.Second)

	checkEchoServerConfig(t, c2)

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
	testLogger := zlogger.WithName("tester")

	confChan := make(chan *stnrv2.StunnerConfig, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testCDSAddr := "localhost:63479"
	log.Debugf("create server on %s", testCDSAddr)
	// the node address of a client is its node name itself
	patcher := func(conf *stnrv2.StunnerConfig, _ string, labels map[string]string) *stnrv2.StunnerConfig {
		node, ok := labels[stnrv2.DefaultCDSNodeLabel]
		if !ok {
			return conf
		}
		for i := range conf.Clusters {
			for j, a := range conf.Clusters[i].Addrs {
				if a == "$"+stnrv2.DefaultEnvVarNodeAddr {
					conf.Clusters[i].Addrs[j] = node
				}
			}
		}
		return conf
	}
	srv := cdsserver.New(testCDSAddr, patcher, testLogger)
	assert.NotNil(t, srv, "server")
	err = srv.Start(ctx)
	assert.NoError(t, err, "start")

	log.Debug("creating a stunnerd")
	stunner := NewStunner(Options{
		LogOptions:       LogOptions{Level: stunnerTestLoglevel},
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
				if err := stunner.Reconcile(c); err != nil {
					var restarted stnrv2.ErrRestarted
					assert.True(t, errors.As(err, &restarted), "reconcile: %v", err)
				}
			}
		}
	}()

	for _, testCase := range []struct {
		name   string
		prep   func(c *stnrv2.StunnerConfig, t *testing.T)
		tester func(c *stnrv2.StunnerConfig) bool
	}{{
		name: "default",
		prep: func(c *stnrv2.StunnerConfig, _ *testing.T) {},
		tester: func(c *stnrv2.StunnerConfig) bool {
			return len(c.Clusters) == 1 && len(c.Clusters[0].Addrs) == 0
		},
	}, {
		name: "default w/ IP",
		prep: func(c *stnrv2.StunnerConfig, _ *testing.T) { c.Clusters[0].Addrs = []string{"127.0.0.1"} },
		tester: func(c *stnrv2.StunnerConfig) bool {
			return len(c.Clusters) == 1 && len(c.Clusters[0].Addrs) == 1 &&
				isHostLocal(c.Clusters[0].Addrs[0])
		},
	}, {
		name: "node rewrite",
		prep: func(c *stnrv2.StunnerConfig, _ *testing.T) {
			c.Clusters[0].Addrs = []string{"$" + stnrv2.DefaultEnvVarNodeAddr}
		},
		tester: func(c *stnrv2.StunnerConfig) bool {
			return len(c.Clusters) == 1 && len(c.Clusters[0].Addrs) == 1 &&
				c.Clusters[0].Addrs[0] == "127.1.2.3"
		},
	}} {
		testName := fmt.Sprintf("TestStunner_NewDefaultConfig_URI:%s", testCase.name)
		t.Run(testName, func(t *testing.T) {
			log.Debugf("-------------- Running test: %s -------------", testName)

			config := &stnrv2.StunnerConfig{
				ApiVersion: stnrv2.ApiVersion,
				Admin: stnrv2.AdminConfig{
					LogLevel: stunnerTestLoglevel,
				},
				Auth: stnrv2.AuthConfig{
					Credentials: map[string]string{
						"username": "user",
						"password": "pass",
					},
				},
				Listeners: []stnrv2.ListenerConfig{{
					Name:     "default-listener",
					Protocol: "UDP",
					Servers:  []string{"default-server"},
				}},
				Servers: []stnrv2.ServerConfig{{
					Name:     "default-server",
					Type:     "turn",
					Clusters: []string{"allow-any"},
				}},
				Clusters: []stnrv2.ClusterConfig{{
					Name:      "allow-any",
					Endpoints: []string{"0.0.0.0/0"},
					Protocol:  "UDP",
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
		{"secret_with_leading_$", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"ephemeral","credentials":{"secret":"$secret","username":"user"}}}`), "user", "", "$secret"},
		{"secret_with_trailing_$", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"ephemeral","credentials":{"secret":"secret$","username":"user"}}}`), "user", "", "secret$"},
		{"secret_with_$", []byte(`{"version":"v1","admin":{"name":"ns1/tester"},"auth":{"type":"ephemeral","credentials":{"secret":"sec$ret","username":"user"}}}`), "user", "", "sec$ret"},
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

func checkDefaultConfig(t *testing.T, c *stnrv2.StunnerConfig, proto string) {
	t.Helper()
	assert.Equal(t, "static", c.Auth.Type, "auth-type")
	assert.Equal(t, "user1", c.Auth.Credentials["username"], "username")
	assert.Equal(t, "passwd1", c.Auth.Credentials["password"], "passwd")
	assert.Len(t, c.Listeners, 1, "listeners len")
	assert.Empty(t, c.Listeners[0].Addr, "listener binds all interfaces")
	assert.Equal(t, 3478, c.Listeners[0].Port, "listener port")
	assert.Equal(t, proto, c.Listeners[0].Protocol, "listener proto")
	assert.Equal(t, []string{"default-server"}, c.Listeners[0].Servers, "listener server")
	assert.Len(t, c.Servers, 1, "servers len")
	assert.Equal(t, "turn", c.Servers[0].Type, "server type")
	assert.Equal(t, []string{"allow-any"}, c.Servers[0].Clusters, "server clusters")
	assert.Len(t, c.Clusters, 1, "clusters len")
	assert.Equal(t, "STATIC", c.Clusters[0].Type, "cluster type")
	assert.Equal(t, []string{"0.0.0.0/0", "::/0"}, c.Clusters[0].Endpoints, "cluster endpoints")
	assert.Equal(t, "UDP", c.Clusters[0].Protocol, "cluster proto")
	assert.Equal(t, []string{"1.2.3.4"}, c.Clusters[0].Addrs, "cluster addresses")
}

// checkEchoServerConfig checks the echo-server config of the multi-version tests, whatever
// version it was written in.
func checkEchoServerConfig(t *testing.T, c *stnrv2.StunnerConfig) {
	t.Helper()
	assert.Equal(t, stnrv2.ApiVersion, c.ApiVersion, "version")
	assert.Equal(t, "all:ERROR", c.Admin.LogLevel, "loglevel")
	assert.True(t, c.Auth.Type == "static" || c.Auth.Type == "ephemeral", "auth type")
	assert.Len(t, c.Listeners, 1, "listeners len")
	assert.Equal(t, "udp", c.Listeners[0].Name, "listener name")
	assert.Equal(t, "UDP", c.Listeners[0].Protocol, "listener proto")
	assert.Equal(t, 3478, c.Listeners[0].Port, "listener port")
	assert.Equal(t, []string{"udp"}, c.Listeners[0].Servers, "listener server")
	assert.Len(t, c.Servers, 1, "servers len")
	assert.Equal(t, "turn", c.Servers[0].Type, "server type")
	assert.Equal(t, []string{"echo-server-cluster"}, c.Servers[0].Clusters, "server clusters")
	assert.Len(t, c.Clusters, 1, "clusters len")
	assert.Equal(t, "echo-server-cluster", c.Clusters[0].Name, "cluster name")
	assert.Equal(t, "STATIC", c.Clusters[0].Type, "cluster type")
	assert.Equal(t, []string{"1.2.3.5"}, c.Clusters[0].Endpoints, "cluster endpoints")
}
