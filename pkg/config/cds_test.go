package server

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/config/client"
	"github.com/l7mp/stunner/pkg/config/server"
	"github.com/l7mp/stunner/pkg/logger"
)

// var testerLogLevel = zapcore.Level(-10)
// var testerLogLevel = zapcore.DebugLevel
var testerLogLevel = zapcore.ErrorLevel

// const stunnerLogLevel = "all:TRACE"
const stunnerLogLevel = "all:ERROR"

// run on random port
func getRandCDSAddr() string {
	rndPort := rand.Intn(10000) + 20000
	return fmt.Sprintf(":%d", rndPort)
}

func init() {
	// setup a fast pinger so that we get a timely error notification
	client.PingPeriod = 500 * time.Millisecond
	client.PongWait = 800 * time.Millisecond
	client.WriteWait = 200 * time.Millisecond
	client.RetryPeriod = 250 * time.Millisecond
}

func TestServerLoad(t *testing.T) {
	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	log := zlogger.WithName("tester")

	logger := logger.NewLoggerFactory(stunnerLogLevel)
	testLog := logger.NewLogger("test")

	// suppress deletions
	server.SuppressConfigDeletion = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testCDSAddr := getRandCDSAddr()
	testLog.Debugf("create server on %s", testCDSAddr)
	srv := server.New(testCDSAddr, nil, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(ctx)
	assert.NoError(t, err, "start")

	time.Sleep(20 * time.Millisecond)

	testLog.Debug("create client")
	client1, err := client.New(testCDSAddr, "ns1/gw1", "", logger)
	assert.NoError(t, err, "client 1")
	client2, err := client.New(testCDSAddr, "ns1/gw2", "", logger)
	assert.NoError(t, err, "client 2")
	// nonexistent
	client3, err := client.New(testCDSAddr, "ns1/gw3", "", logger)
	assert.NoError(t, err, "client 3")

	testLog.Debug("load: error")
	c, err := client1.Load()
	assert.Error(t, err, "load")
	assert.Nil(t, c, "conf")
	c, err = client2.Load()
	assert.Error(t, err, "load")
	assert.Nil(t, c, "conf")
	c, err = client3.Load()
	assert.Error(t, err, "load")
	assert.Nil(t, c, "conf")

	c1 := testConfig("ns1/gw1", "realm1")
	c2 := testConfig("ns1/gw2", "realm1")
	err = srv.UpdateConfig([]server.Config{c1, c2})
	assert.NoError(t, err, "update")

	cs := srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 2, "snapshot len")
	ns, name, _ := server.NamespacedName("ns1/gw1")
	sc1, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get 2")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq")
	ns, name, _ = server.NamespacedName("ns1/gw2")
	sc2, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc2, "get 2")
	assert.NoError(t, sc2.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c2.DeepEqual(sc2), "deepeq")
	ns, name, _ = server.NamespacedName("ns1/gw3")
	sc3, ok := srv.GetConfigStore().Get(ns, name)
	assert.False(t, ok, "get 3")
	assert.Nil(t, sc3, "get 3")

	testLog.Debug("load: config ok")
	c, err = client1.Load()
	assert.NoError(t, err, "load")
	assert.True(t, c.DeepEqual(sc1.Config), "deepeq")
	c, err = client2.Load()
	assert.NoError(t, err, "load")
	assert.True(t, c.DeepEqual(sc2.Config), "deepeq")
	c, err = client3.Load()
	assert.Error(t, err, "load")
	assert.Nil(t, c, "conf")

	testLog.Debug("remove 2 configs")
	err = srv.UpdateConfig([]server.Config{})
	assert.NoError(t, err, "update")

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 0, "snapshot len")

	testLog.Debug("load: no result")
	_, err = client1.Load()
	assert.Error(t, err, "load")
	_, err = client2.Load()
	assert.Error(t, err, "load")
	_, err = client3.Load()
	assert.Error(t, err, "load")
	assert.Nil(t, c, "conf")
}

func TestServerPoll(t *testing.T) {
	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	log := zlogger.WithName("tester")

	logger := logger.NewLoggerFactory(stunnerLogLevel)
	testLog := logger.NewLogger("test")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testCDSAddr := getRandCDSAddr()
	testLog.Debugf("create server on %s", testCDSAddr)
	srv := server.New(testCDSAddr, nil, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(ctx)
	assert.NoError(t, err, "start")

	time.Sleep(20 * time.Millisecond)

	testLog.Debug("create client")
	client1, err := client.New(testCDSAddr, "ns1/gw1", "", logger)
	assert.NoError(t, err, "client 1")
	client2, err := client.New(testCDSAddr, "ns1/gw2", "", logger)
	assert.NoError(t, err, "client 2")
	client3, err := client.New(testCDSAddr, "ns1/gw3", "", logger)
	assert.NoError(t, err, "client 3")

	testLog.Debug("poll: no result")
	ch1 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch1)
	ch2 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch2)
	ch3 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch3)

	go func() {
		err = client1.Poll(ctx, ch1, false)
		assert.NoError(t, err, "client 1 cancelled")
	}()
	go func() {
		err = client2.Poll(ctx, ch2, false)
		assert.NoError(t, err, "client 2 cancelled")
	}()
	go func() {
		err = client3.Poll(ctx, ch2, false)
		assert.NoError(t, err, "client 3 cancelled")
	}()

	s := watchConfig(ch1, 10*time.Millisecond)
	assert.Nil(t, s, "config 1")
	s = watchConfig(ch2, 10*time.Millisecond)
	assert.Nil(t, s, "config 2")
	s = watchConfig(ch3, 10*time.Millisecond)
	assert.Nil(t, s, "config 3")

	testLog.Debug("poll: one result")
	c1 := testConfig("ns1/gw1", "realm1")
	c2 := testConfig("ns1/gw2", "realm1")
	err = srv.UpdateConfig([]server.Config{c1, c2})
	assert.NoError(t, err, "update")

	cs := srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 2, "snapshot len")
	ns, name, _ := server.NamespacedName("ns1/gw1")
	sc1, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get 1")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq")
	ns, name, _ = server.NamespacedName("ns1/gw2")
	sc2, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 2")
	assert.NotNil(t, sc2, "get 2")
	assert.NoError(t, sc2.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c2.DeepEqual(sc2), "deepeq")
	ns, name, _ = server.NamespacedName("ns2/gw1")
	sc3, ok := srv.GetConfigStore().Get(ns, name)
	assert.False(t, ok, "get 3")
	assert.Nil(t, sc3, "get 3")

	// poll should have fed the configs to the channels
	s = watchConfig(ch1, 100*time.Millisecond)
	assert.NotNil(t, s, "config 1")
	assert.True(t, s.DeepEqual(sc1.Config), "deepeq 1")
	s = watchConfig(ch2, 100*time.Millisecond)
	assert.NotNil(t, s, "config 2")
	assert.True(t, s.DeepEqual(sc2.Config), "deepeq 2")
	s = watchConfig(ch3, 100*time.Millisecond)
	assert.Nil(t, s, "config 3")

	testLog.Debug("remove 2 configs")
	err = srv.UpdateConfig([]server.Config{})
	assert.NoError(t, err, "update")

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 0, "snapshot len")

	testLog.Debug("poll: zeroconfig")
	s = watchConfig(ch1, 10*time.Millisecond)
	assert.Nil(t, s, "config")
	s = watchConfig(ch2, 10*time.Millisecond)
	assert.Nil(t, s, "config")
	s = watchConfig(ch3, 10*time.Millisecond)
	assert.Nil(t, s, "config")
}

func TestServerWatch(t *testing.T) {
	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	log := zlogger.WithName("tester")

	logger := logger.NewLoggerFactory(stunnerLogLevel)
	testLog := logger.NewLogger("test")

	serverCtx, serverCancel := context.WithCancel(context.Background())

	suppressConfigDeletion := server.SuppressConfigDeletion
	server.SuppressConfigDeletion = false // false by default
	testCDSAddr := getRandCDSAddr()
	testLog.Debugf("create server on %s", testCDSAddr)
	srv := server.New(testCDSAddr, nil, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(serverCtx)
	assert.NoError(t, err, "start")

	testLog.Debug("create client")
	client1, err := client.New(testCDSAddr, "ns1/gw1", "", logger)
	assert.NoError(t, err, "client 1")
	client2, err := client.New(testCDSAddr, "ns1/gw2", "", logger)
	assert.NoError(t, err, "client 2")
	client3, err := client.New(testCDSAddr, "ns1/gw3", "", logger)
	assert.NoError(t, err, "client 3")

	testLog.Debug("watch: no result")
	ch1 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch1)
	ch2 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch2)
	ch3 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch3)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	err = client1.Watch(clientCtx, ch1, false)
	assert.NoError(t, err, "client 1 watch")
	err = client2.Watch(clientCtx, ch2, false)
	assert.NoError(t, err, "client 2 watch")
	err = client3.Watch(clientCtx, ch3, false)
	assert.NoError(t, err, "client 3 watch")

	s := watchConfig(ch1, 150*time.Millisecond)
	assert.Nil(t, s, "config 1")
	s = watchConfig(ch2, 150*time.Millisecond)
	assert.Nil(t, s, "config 2")
	s = watchConfig(ch3, 150*time.Millisecond)
	assert.Nil(t, s, "config 3")

	testLog.Debug("poll: one result")
	c1 := testConfig("ns1/gw1", "realm1")
	c2 := testConfig("ns1/gw2", "realm1")
	err = srv.UpdateConfig([]server.Config{c1, c2})
	assert.NoError(t, err, "update")

	cs := srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 2, "snapshot len")
	ns, name, _ := server.NamespacedName("ns1/gw1")
	sc1, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get 1")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq")
	ns, name, _ = server.NamespacedName("ns1/gw2")
	sc2, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc2, "get 2")
	assert.NoError(t, sc2.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c2.DeepEqual(sc2), "deepeq")
	ns, name, _ = server.NamespacedName("ns1/gw3")
	sc3, ok := srv.GetConfigStore().Get(ns, name)
	assert.False(t, ok, "get 3")
	assert.Nil(t, sc3, "get 3")

	// poll should have fed the configs to the channels
	s = watchConfig(ch1, 500*time.Millisecond)
	assert.NotNil(t, s, "config 1")
	assert.True(t, s.DeepEqual(sc1.Config), "deepeq 1")
	s = watchConfig(ch2, 500*time.Millisecond)
	assert.NotNil(t, s, "config 2")
	assert.True(t, s.DeepEqual(sc2.Config), "deepeq 2")
	s = watchConfig(ch3, 500*time.Millisecond)
	assert.Nil(t, s, "config 3")

	testLog.Debug("update: conf 1 and conf 3")
	c1 = testConfig("ns1/gw1", "realm-new")
	c3 := testConfig("ns1/gw3", "realm3")
	err = srv.UpdateConfig([]server.Config{c1, c2, c3})
	assert.NoError(t, err, "update")

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 3, "snapshot len")
	ns, name, _ = server.NamespacedName("ns1/gw1")
	sc1, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get 1")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq 1")
	ns, name, _ = server.NamespacedName("ns1/gw2")
	sc2, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 2")
	assert.NotNil(t, sc2, "get 2")
	assert.NoError(t, sc2.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c2.DeepEqual(sc2), "deepeq 2")
	ns, name, _ = server.NamespacedName("ns1/gw3")
	sc3, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 3")
	assert.NotNil(t, sc3, "get 3")
	assert.NoError(t, sc3.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c3.DeepEqual(sc3), "deepeq 3")

	// poll should have fed the configs to the channels
	s = watchConfig(ch1, 500*time.Millisecond)
	assert.NotNil(t, s, "config 1")
	assert.True(t, s.DeepEqual(sc1.Config), "deepeq 1")
	s = watchConfig(ch2, 500*time.Millisecond)
	assert.Nil(t, s, "config 2")
	s = watchConfig(ch3, 500*time.Millisecond)
	assert.NotNil(t, s, "config 3")
	assert.True(t, s.DeepEqual(sc3.Config), "deepeq 3")

	testLog.Debug("restarting server")
	serverCancel()

	// let the server shut down and restart
	time.Sleep(50 * time.Millisecond)
	serverCtx, serverCancel = context.WithCancel(context.Background())
	defer serverCancel()
	srv = server.New(testCDSAddr, nil, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(serverCtx)
	assert.NoError(t, err, "start")

	err = srv.UpdateConfig([]server.Config{c1, c2, c3})
	assert.NoError(t, err, "update")

	// obtain the initial configs: this may take a while
	s = watchConfig(ch1, 5000*time.Millisecond)
	assert.NotNil(t, s, "config 1")
	assert.True(t, s.DeepEqual(sc1.Config), "deepeq 1")
	s = watchConfig(ch2, 500*time.Millisecond)
	assert.NotNil(t, s, "config 2")
	assert.True(t, s.DeepEqual(sc2.Config), "deepeq 2")
	s = watchConfig(ch3, 500*time.Millisecond)
	assert.NotNil(t, s, "config 3")
	assert.True(t, s.DeepEqual(sc3.Config), "deepeq 3")

	testLog.Debug("remove 1 config (the 2nd)")
	err = srv.UpdateConfig([]server.Config{c1, c3})
	assert.NoError(t, err, "update")

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 2, "snapshot len")
	ns, name, _ = server.NamespacedName("ns1/gw1")
	sc1, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get 1")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq 1")
	ns, name, _ = server.NamespacedName("ns1/gw2")
	sc2, ok = srv.GetConfigStore().Get(ns, name)
	assert.False(t, ok, "get 2")
	assert.Nil(t, sc2, "get 2")
	ns, name, _ = server.NamespacedName("ns1/gw3")
	sc3, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 3")
	assert.NotNil(t, sc3, "get 3")
	assert.NoError(t, sc3.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c3.DeepEqual(sc3), "deepeq 3")

	s = watchConfig(ch1, 50*time.Millisecond)
	assert.Nil(t, s, "config 1")
	s = watchConfig(ch2, 50*time.Millisecond)
	assert.NotNil(t, s, "config 2") // should be a zeroconfig
	assert.True(t, client.IsConfigDeleted(s))
	s = watchConfig(ch3, 50*time.Millisecond)
	assert.Nil(t, s, "config 3")

	testLog.Debug("remove remaining 2 configs")
	err = srv.UpdateConfig([]server.Config{})
	assert.NoError(t, err, "update")

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 0, "snapshot len")

	testLog.Debug("poll: deleted config")
	s = watchConfig(ch1, 10*time.Millisecond)
	assert.NotNil(t, s, "config")
	assert.True(t, client.IsConfigDeleted(s))
	s = watchConfig(ch2, 10*time.Millisecond)
	assert.Nil(t, s, "config")
	s = watchConfig(ch3, 10*time.Millisecond)
	assert.NotNil(t, s, "config")
	assert.True(t, client.IsConfigDeleted(s))

	server.SuppressConfigDeletion = suppressConfigDeletion // reset
}

// config already available when watcher joins
func TestServerWatchBootstrap(t *testing.T) {
	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	log := zlogger.WithName("tester")

	logger := logger.NewLoggerFactory(stunnerLogLevel)
	testLog := logger.NewLogger("test")

	// switch config deletions on
	suppressConfigDeletion := server.SuppressConfigDeletion
	server.SuppressConfigDeletion = false

	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	testCDSAddr := getRandCDSAddr()
	testLog.Debugf("create server on %s", testCDSAddr)
	srv := server.New(testCDSAddr, nil, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(serverCtx)
	assert.NoError(t, err, "start")

	testLog.Debug("create client")
	client1, err := client.New(testCDSAddr, "ns1/gw1", "", logger)
	assert.NoError(t, err, "client 1")

	testLog.Debug("bootstrap")
	c1 := testConfig("ns1/gw1", "realm1")
	c2 := testConfig("ns1/gw2", "realm1")
	err = srv.UpdateConfig([]server.Config{c1, c2})
	assert.NoError(t, err, "update")

	cs := srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 2, "snapshot len")
	ns, name, _ := server.NamespacedName("ns1/gw1")
	sc1, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get 1")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq")
	ns, name, _ = server.NamespacedName("ns1/gw2")
	sc2, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 2")
	assert.NotNil(t, sc2, "get 2")
	assert.NoError(t, sc2.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq")
	ns, name, _ = server.NamespacedName("ns1/gw3")
	sc3, ok := srv.GetConfigStore().Get(ns, name)
	assert.False(t, ok, "get 3")
	assert.Nil(t, sc3, "get 3")

	testLog.Debug("watch: 1 result")
	ch1 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch1)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	err = client1.Watch(clientCtx, ch1, false)
	assert.NoError(t, err, "client 1 watch")

	s := watchConfig(ch1, 1500*time.Millisecond)
	assert.NotNil(t, s, "config 1")
	assert.True(t, s.DeepEqual(sc1.Config), "deepeq 1")
	// only 1 config
	s = watchConfig(ch1, 150*time.Millisecond)
	assert.Nil(t, s, "config 1")

	testLog.Debug("update: conf 1 and conf 2")
	c1 = testConfig("ns1/gw1", "realm-new")
	c2 = testConfig("ns1/gw2", "realm3")
	err = srv.UpdateConfig([]server.Config{c1, c2})
	assert.NoError(t, err, "update")

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 2, "snapshot len")
	ns, name, _ = server.NamespacedName("ns1/gw1")
	sc1, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get 1")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq 1")
	ns, name, _ = server.NamespacedName("ns1/gw2")
	sc2, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 2")
	assert.NotNil(t, sc2, "get 2")
	assert.NoError(t, sc2.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c2.DeepEqual(sc2), "deepeq 2")

	s = watchConfig(ch1, 500*time.Millisecond)
	assert.NotNil(t, s, "config 1")
	assert.True(t, s.DeepEqual(c1.Config), "deepeq 1")

	testLog.Debug("remove remaining 2 configs")
	err = srv.UpdateConfig([]server.Config{})
	assert.NoError(t, err, "update")

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 0, "snapshot len")

	testLog.Debug("poll: no config")
	s = watchConfig(ch1, 100*time.Millisecond)
	assert.NotNil(t, s, "config")
	assert.True(t, client.IsConfigDeleted(s))

	server.SuppressConfigDeletion = suppressConfigDeletion
}

// test APIs
func TestServerAPI(t *testing.T) {
	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	log := zlogger.WithName("tester")

	logger := logger.NewLoggerFactory(stunnerLogLevel)
	testLog := logger.NewLogger("test")

	serverCtx, serverCancel := context.WithCancel(context.Background())

	testCDSAddr := getRandCDSAddr()
	testLog.Debugf("create server on %s", testCDSAddr)
	srv := server.New(testCDSAddr, nil, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(serverCtx)
	assert.NoError(t, err, "start")

	testLog.Debug("create client")
	client1, err := client.NewAllConfigsAPI(testCDSAddr, logger.NewLogger("all-config-client"))
	assert.NoError(t, err, "client 1")
	client2, err := client.NewConfigsNamespaceAPI(testCDSAddr, "ns1", logger.NewLogger("ns-config-client-ns1"))
	assert.NoError(t, err, "client 2")
	client3, err := client.NewConfigsNamespaceAPI(testCDSAddr, "ns2", logger.NewLogger("ns-config-client-ns2"))
	assert.NoError(t, err, "client 3")
	client4, err := client.NewConfigNamespaceNameAPI(testCDSAddr, "ns1", "gw1", "", logger.NewLogger("gw-config-client"))
	assert.NoError(t, err, "client 4")

	testLog.Debug("watch: no result")
	ch1 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch1)
	ch2 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch2)
	ch3 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch3)
	ch4 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch4)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	err = client1.Watch(clientCtx, ch1, false)
	assert.NoError(t, err, "client 1 watch")
	err = client2.Watch(clientCtx, ch2, false)
	assert.NoError(t, err, "client 2 watch")
	err = client3.Watch(clientCtx, ch3, false)
	assert.NoError(t, err, "client 3 watch")
	err = client4.Watch(clientCtx, ch4, false)
	assert.NoError(t, err, "client 4 watch")

	s := watchConfig(ch1, 50*time.Millisecond)
	assert.Nil(t, s, "config 1")
	s = watchConfig(ch2, 50*time.Millisecond)
	assert.Nil(t, s, "config 2")
	s = watchConfig(ch3, 50*time.Millisecond)
	assert.Nil(t, s, "config 3")
	s = watchConfig(ch4, 50*time.Millisecond)
	assert.Nil(t, s, "config 4")

	testLog.Debug("--------------------------------")
	testLog.Debug("Update1: ns1/gw1 + ns2/gw1      ")
	testLog.Debug("--------------------------------")
	testLog.Debug("poll: one result")
	c1 := testConfig("ns1/gw1", "realm1")
	c2 := testConfig("ns2/gw1", "realm1")
	err = srv.UpdateConfig([]server.Config{c1, c2})
	assert.NoError(t, err, "update")

	cs := srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 2, "snapshot len")
	ns, name, _ := server.NamespacedName("ns1/gw1")
	sc1, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get 1")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq 1")
	ns, name, _ = server.NamespacedName("ns2/gw1")
	sc2, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 2")
	assert.NotNil(t, sc2, "get 2")
	assert.NoError(t, sc2.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c2.DeepEqual(sc2), "deepeq 2")

	testLog.Debug("load")

	// all-configs should result sc1 and sc2
	scs, err := client1.Get(clientCtx)
	assert.NoError(t, err, "load 1")
	assert.Len(t, scs, 2, "load 1")
	co := findConfById(scs, "ns1/gw1")
	assert.NotNil(t, co, "c1")
	assert.NoError(t, co.Validate(), "valid") // validate needed for deepequal to pass
	assert.True(t, co.DeepEqual(sc1.Config), "deepeq")
	co = findConfById(scs, "ns2/gw1")
	assert.NotNil(t, co, "c2")
	assert.NoError(t, co.Validate(), "valid") // validate needed for deepequal to pass
	assert.True(t, co.DeepEqual(sc2.Config), "deepeq")

	// ns1 client should yield 1 config
	scs, err = client2.Get(clientCtx)
	assert.NoError(t, err, "load 2")
	assert.Len(t, scs, 1, "load 2")
	assert.NoError(t, scs[0].Validate(), "valid") // validate needed for deepequal to pass
	assert.True(t, scs[0].DeepEqual(sc1.Config), "deepeq")

	// ns2 client should yield 1 config
	scs, err = client3.Get(clientCtx)
	assert.NoError(t, err, "load 3")
	assert.Len(t, scs, 1, "load 3")
	assert.NoError(t, scs[0].Validate(), "valid") // validate needed for deepequal to pass
	assert.True(t, scs[0].DeepEqual(sc2.Config), "deepeq")

	// ns1/gw1 client should yield 1 config
	scs, err = client4.Get(clientCtx)
	assert.NoError(t, err, "load 4")
	assert.Len(t, scs, 1, "load 4")
	assert.NoError(t, scs[0].Validate(), "valid") // validate needed for deepequal to pass
	assert.True(t, scs[0].DeepEqual(sc1.Config), "deepeq")

	// two configs from client1 watch
	s1 := watchConfig(ch1, 50*time.Millisecond)
	assert.NotNil(t, s1)
	s2 := watchConfig(ch1, 50*time.Millisecond)
	assert.NotNil(t, s2)
	s3 := watchConfig(ch1, 50*time.Millisecond)
	assert.Nil(t, s3)
	lst := []*stnrv1.StunnerConfig{s1, s2}
	assert.NotNil(t, findConfById(lst, "ns1/gw1"))
	assert.True(t, findConfById(lst, "ns1/gw1").DeepEqual(sc1.Config), "deepeq 1")
	assert.NotNil(t, findConfById(lst, "ns2/gw1"))
	assert.True(t, findConfById(lst, "ns2/gw1").DeepEqual(sc2.Config), "deepeq 1")

	// 1 config from client2 watch
	s = watchConfig(ch2, 50*time.Millisecond)
	assert.NotNil(t, s)
	assert.True(t, s.DeepEqual(sc1.Config))
	s = watchConfig(ch2, 50*time.Millisecond)
	assert.Nil(t, s)

	// 1 config from client3 watch
	s = watchConfig(ch3, 50*time.Millisecond)
	assert.NotNil(t, s, "config 3")
	assert.True(t, s.DeepEqual(sc2.Config))
	s = watchConfig(ch3, 50*time.Millisecond)
	assert.Nil(t, s)

	// 1 config from client4 watch
	s = watchConfig(ch4, 50*time.Millisecond)
	assert.NotNil(t, s)
	assert.True(t, s.DeepEqual(sc1.Config))
	s = watchConfig(ch4, 50*time.Millisecond)
	assert.Nil(t, s)

	testLog.Debug("--------------------------------")
	testLog.Debug("Update1: ns1/gw1 + ns1/gw2      ")
	testLog.Debug("--------------------------------")
	testLog.Debug("update: conf 1 and conf 3")
	c1 = testConfig("ns1/gw1", "realm-new")
	c3 := testConfig("ns1/gw2", "realm3")
	err = srv.UpdateConfig([]server.Config{c1, c2, c3})
	assert.NoError(t, err, "update")

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 3, "snapshot len")
	ns, name, _ = server.NamespacedName("ns1/gw1")
	sc1, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get 1")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq")
	ns, name, _ = server.NamespacedName("ns2/gw1")
	sc2, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 2")
	assert.NotNil(t, sc2, "get 2")
	assert.NoError(t, sc2.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c2.DeepEqual(sc2), "deepeq")
	configNs2Gw1 := &stnrv1.StunnerConfig{}
	co.DeepCopyInto(configNs2Gw1)
	ns, name, _ = server.NamespacedName("ns1/gw2")
	sc3, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 3")
	assert.NotNil(t, sc3, "get 3")
	assert.NoError(t, sc3.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c3.DeepEqual(sc3), "deepeq")

	// all-configs should result sc1 and sc2 and sc3
	scs, err = client1.Get(clientCtx)
	assert.NoError(t, err, "load 1")
	assert.Len(t, scs, 3, "load 1")
	co = findConfById(scs, "ns1/gw1")
	assert.NotNil(t, co, "c1")
	assert.NoError(t, co.Validate(), "valid") // validate needed for deepequal to pass
	assert.True(t, co.DeepEqual(sc1.Config), "deepeq")
	co = findConfById(scs, "ns2/gw1")
	assert.NotNil(t, co, "c2")
	assert.NoError(t, co.Validate(), "valid") // validate needed for deepequal to pass
	assert.True(t, co.DeepEqual(sc2.Config), "deepeq")
	co = findConfById(scs, "ns1/gw2")
	assert.NotNil(t, co, "c3")
	assert.NoError(t, co.Validate(), "valid") // validate needed for deepequal to pass
	assert.True(t, co.DeepEqual(sc3.Config), "deepeq")

	// ns1 client should yield 2 configs
	scs, err = client2.Get(clientCtx)
	assert.NoError(t, err, "load 2")
	assert.Len(t, scs, 2, "load 2")
	ssc1 := findConfById(scs, "ns1/gw1")
	assert.NotNil(t, sc1)
	assert.NoError(t, ssc1.Validate(), "valid") // validate needed for deepequal to pass
	assert.True(t, ssc1.DeepEqual(sc1.Config), "deepeq")
	ssc2 := findConfById(scs, "ns1/gw2")
	assert.NotNil(t, sc2)
	assert.NoError(t, ssc2.Validate(), "valid") // validate needed for deepequal to pass
	assert.True(t, ssc2.DeepEqual(sc3.Config), "deepeq")

	// ns2 client should yield 1 config
	scs, err = client3.Get(clientCtx)
	assert.NoError(t, err, "load 3")
	assert.Len(t, scs, 1, "load 3")
	assert.NoError(t, scs[0].Validate(), "valid") // validate needed for deepequal to pass
	assert.True(t, scs[0].DeepEqual(configNs2Gw1), "deepeq")

	// ns1/gw1 client should yield 1 config
	scs, err = client4.Get(clientCtx)
	assert.NoError(t, err, "load 4")
	assert.Len(t, scs, 1, "load 4")
	assert.NoError(t, scs[0].Validate(), "valid") // validate needed for deepequal to pass
	assert.True(t, scs[0].DeepEqual(sc1.Config), "deepeq")

	// 2 configs from client1 watch
	s1 = watchConfig(ch1, 1500*time.Millisecond)
	assert.NotNil(t, s1)
	s2 = watchConfig(ch1, 150*time.Millisecond)
	assert.NotNil(t, s2)
	s3 = watchConfig(ch1, 150*time.Millisecond)
	assert.Nil(t, s3)
	lst = []*stnrv1.StunnerConfig{s1, s2}
	assert.NotNil(t, findConfById(lst, "ns1/gw1"))
	assert.True(t, findConfById(lst, "ns1/gw1").DeepEqual(sc1.Config), "deepeq")
	assert.NotNil(t, findConfById(lst, "ns1/gw2"))
	assert.True(t, findConfById(lst, "ns1/gw2").DeepEqual(sc3.Config), "deepeq")

	// 2 configs from client2 watch
	s1 = watchConfig(ch2, 1500*time.Millisecond)
	assert.NotNil(t, s1)
	s2 = watchConfig(ch2, 150*time.Millisecond)
	assert.NotNil(t, s2)
	s3 = watchConfig(ch2, 50*time.Millisecond)
	assert.Nil(t, s3)
	lst = []*stnrv1.StunnerConfig{s1, s2}
	assert.NotNil(t, findConfById(lst, "ns1/gw1"))
	assert.True(t, findConfById(lst, "ns1/gw1").DeepEqual(sc1.Config), "deepeq")
	assert.NotNil(t, findConfById(lst, "ns1/gw2"))
	assert.True(t, findConfById(lst, "ns1/gw2").DeepEqual(sc3.Config), "deepeq")

	// 0 config from client3 watch
	s = watchConfig(ch3, 50*time.Millisecond)
	assert.Nil(t, s, "config 3")

	// 1 config from client4 watch
	s = watchConfig(ch4, 50*time.Millisecond)
	assert.NotNil(t, s)
	assert.True(t, s.DeepEqual(sc1.Config), "deepeq")

	testLog.Debug("--------------------------------")
	testLog.Debug("Restart + Update1: ns1/gw1 + ns2/gw1 + ns1/gw2")
	testLog.Debug("--------------------------------")
	testLog.Debug("restarting server")
	serverCancel()
	// let the server shut down and restart
	time.Sleep(50 * time.Millisecond)
	serverCtx, serverCancel = context.WithCancel(context.Background())
	defer serverCancel()
	srv = server.New(testCDSAddr, nil, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(serverCtx)
	assert.NoError(t, err, "start")
	err = srv.UpdateConfig([]server.Config{c1, c2, c3})
	assert.NoError(t, err, "update")

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 3, "snapshot len")
	ns, name, _ = server.NamespacedName("ns1/gw1")
	sc1, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get 1")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq")
	ns, name, _ = server.NamespacedName("ns2/gw1")
	sc2, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 2")
	assert.NotNil(t, sc2, "get 2")
	assert.NoError(t, sc2.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c2.DeepEqual(sc2), "deepeq")
	ns, name, _ = server.NamespacedName("ns1/gw2")
	sc3, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 3")
	assert.NotNil(t, sc3, "get 3")
	assert.NoError(t, sc3.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c3.DeepEqual(sc3), "deepeq")

	// all-configs should result sc1 and sc2 and sc3
	scs, err = client1.Get(clientCtx)
	assert.NoError(t, err, "load 1")
	assert.Len(t, scs, 3, "load 1")
	co = findConfById(scs, "ns1/gw1")
	assert.NotNil(t, co, "c1")
	assert.NoError(t, co.Validate(), "valid") // cds config store does not validate
	assert.True(t, co.DeepEqual(sc1.Config), "deepeq")
	co = findConfById(scs, "ns2/gw1")
	assert.NotNil(t, co, "c2")
	assert.NoError(t, co.Validate(), "valid") // cds config store does not validate
	assert.True(t, co.DeepEqual(sc2.Config), "deepeq")
	co = findConfById(scs, "ns1/gw2")
	assert.NotNil(t, co, "c3")
	assert.NoError(t, co.Validate(), "valid") // cds config store does not validate
	assert.True(t, co.DeepEqual(sc3.Config), "deepeq")

	// ns1 client should yield 2 configs
	scs, err = client2.Get(clientCtx)
	assert.NoError(t, err, "load 2")
	assert.Len(t, scs, 2, "load 2")
	assert.NotNil(t, findConfById(scs, "ns1/gw1"))
	assert.NoError(t, findConfById(scs, "ns1/gw1").Validate(), "valid") // cds config store does not validate
	assert.True(t, findConfById(scs, "ns1/gw1").DeepEqual(sc1.Config), "deepeq")
	assert.NotNil(t, findConfById(scs, "ns1/gw2"))
	assert.NoError(t, findConfById(scs, "ns1/gw2").Validate(), "valid") // cds config store does not validate
	assert.True(t, findConfById(scs, "ns1/gw2").DeepEqual(sc3.Config), "deepeq")

	// ns2 client should yield 1 config
	scs, err = client3.Get(clientCtx)
	assert.NoError(t, err, "load 3")
	assert.Len(t, scs, 1, "load 3")
	assert.NoError(t, scs[0].Validate(), "valid") // cds config store does not validate
	assert.True(t, scs[0].DeepEqual(sc2.Config), "deepeq")

	// ns1/gw1 client should yield 1 config
	scs, err = client4.Get(clientCtx)
	assert.NoError(t, err, "load 4")
	assert.Len(t, scs, 1, "load 4")
	assert.NoError(t, scs[0].Validate(), "valid") // cds config store does not validate
	assert.True(t, scs[0].DeepEqual(sc1.Config), "deepeq")

	// 3 configs from client1 watch
	s1 = watchConfig(ch1, 5000*time.Millisecond)
	assert.NotNil(t, s1)
	s2 = watchConfig(ch1, 100*time.Millisecond)
	assert.NotNil(t, s2)
	s3 = watchConfig(ch1, 100*time.Millisecond)
	assert.NotNil(t, s2)
	s4 := watchConfig(ch1, 100*time.Millisecond)
	assert.Nil(t, s4)
	lst = []*stnrv1.StunnerConfig{s1, s2, s3}
	assert.NotNil(t, findConfById(lst, "ns1/gw1"))
	assert.True(t, findConfById(lst, "ns1/gw1").DeepEqual(sc1.Config), "deepeq")
	assert.NotNil(t, findConfById(lst, "ns1/gw2"))
	assert.True(t, findConfById(lst, "ns2/gw1").DeepEqual(sc2.Config), "deepeq")
	assert.NotNil(t, findConfById(lst, "ns2/gw1"))
	assert.True(t, findConfById(lst, "ns1/gw2").DeepEqual(sc3.Config), "deepeq")

	// 2 configs from client2 watch
	s1 = watchConfig(ch2, 50*time.Millisecond)
	assert.NotNil(t, s1)
	s2 = watchConfig(ch2, 50*time.Millisecond)
	assert.NotNil(t, s2)
	s3 = watchConfig(ch2, 50*time.Millisecond)
	assert.Nil(t, s3)
	lst = []*stnrv1.StunnerConfig{s1, s2}
	assert.NotNil(t, findConfById(lst, "ns1/gw1"))
	assert.True(t, findConfById(lst, "ns1/gw1").DeepEqual(sc1.Config), "deepeq")
	assert.NotNil(t, findConfById(lst, "ns1/gw2"))
	assert.True(t, findConfById(lst, "ns1/gw2").DeepEqual(sc3.Config), "deepeq")

	// 1 config from client3 watch
	s = watchConfig(ch3, 50*time.Millisecond)
	assert.NotNil(t, s, "config 3")
	assert.True(t, s.DeepEqual(sc2.Config))
	s = watchConfig(ch3, 50*time.Millisecond)
	assert.Nil(t, s)

	// 1 config from client4 watch
	s = watchConfig(ch4, 50*time.Millisecond)
	assert.NotNil(t, s)
	assert.True(t, s.DeepEqual(sc1.Config))
	s = watchConfig(ch4, 50*time.Millisecond)
	assert.Nil(t, s)

	// switch config deletions on
	suppressConfigDeletion := server.SuppressConfigDeletion
	server.SuppressConfigDeletion = false // false by default

	testLog.Debug("--------------------------------")
	testLog.Debug("Update1: ns1/gw1 + ns3/gw1      ")
	testLog.Debug("--------------------------------")
	testLog.Debug("update: conf 1, remove conf 3, and add conf 4")
	c1 = testConfig("ns1/gw1", "realm-newer")
	c4 := testConfig("ns3/gw1", "realm4")
	err = srv.UpdateConfig([]server.Config{c1, c2, c4})
	assert.NoError(t, err, "update")

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 3, "snapshot len")
	ns, name, _ = server.NamespacedName("ns1/gw1")
	sc1, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get 1")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq")
	ns, name, _ = server.NamespacedName("ns2/gw1")
	sc2, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 2")
	assert.NotNil(t, sc2, "get 2")
	assert.NoError(t, sc2.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c2.DeepEqual(sc2), "deepeq")
	ns, name, _ = server.NamespacedName("ns3/gw1")
	sc4, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 3")
	assert.NotNil(t, sc3, "get 3")
	assert.NoError(t, sc3.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c4.DeepEqual(sc4), "deepeq")

	// all-configs should result sc1 and sc2 and sc4
	scs, err = client1.Get(clientCtx)
	assert.NoError(t, err, "load 1")
	assert.Len(t, scs, 3, "load 1")
	co = findConfById(scs, "ns1/gw1")
	assert.NotNil(t, co, "c1")
	assert.NoError(t, co.Validate(), "valid") // cds config store does not validate
	assert.True(t, co.DeepEqual(sc1.Config), "deepeq")
	co = findConfById(scs, "ns2/gw1")
	assert.NotNil(t, co, "c2")
	assert.NoError(t, co.Validate(), "valid") // cds config store does not validate
	assert.True(t, co.DeepEqual(sc2.Config), "deepeq")
	co = findConfById(scs, "ns3/gw1")
	assert.NotNil(t, co, "c4")
	assert.NoError(t, co.Validate(), "valid") // cds config store does not validate
	assert.True(t, co.DeepEqual(sc4.Config), "deepeq")

	// ns1 client should yield 1 config
	scs, err = client2.Get(clientCtx)
	assert.NoError(t, err, "load 2")
	assert.Len(t, scs, 1, "load 2")
	assert.NoError(t, scs[0].Validate(), "valid") // cds config store does not validate
	assert.True(t, scs[0].DeepEqual(sc1.Config), "deepeq")

	// ns2 client should yield 1 config
	scs, err = client3.Get(clientCtx)
	assert.NoError(t, err, "load 3")
	assert.Len(t, scs, 1, "load 3")
	assert.NoError(t, scs[0].Validate(), "valid") // cds config store does not validate
	assert.True(t, scs[0].DeepEqual(sc2.Config), "deepeq")

	// ns1/gw1 client should yield 1 config
	scs, err = client4.Get(clientCtx)
	assert.NoError(t, err, "load 4")
	assert.Len(t, scs, 1, "load 4")
	assert.NoError(t, scs[0].Validate(), "valid") // cds config store does not validate
	assert.True(t, scs[0].DeepEqual(sc1.Config), "deepeq")

	// 2 configs from client1 watch
	s1 = watchConfig(ch1, 5000*time.Millisecond)
	assert.NotNil(t, s1)
	s2 = watchConfig(ch1, 500*time.Millisecond)
	assert.NotNil(t, s2)
	s3 = watchConfig(ch1, 500*time.Millisecond)
	assert.NotNil(t, s3)
	lst = []*stnrv1.StunnerConfig{s1, s2, s3}
	assert.NotNil(t, findConfById(lst, "ns1/gw1"))
	assert.True(t, findConfById(lst, "ns1/gw1").DeepEqual(sc1.Config), "deepeq")
	assert.NotNil(t, findConfById(lst, "ns3/gw1"))
	assert.True(t, findConfById(lst, "ns3/gw1").DeepEqual(sc4.Config), "deepeq")
	assert.NotNil(t, findConfById(lst, "ns1/gw2"))
	assert.True(t, client.IsConfigDeleted(findConfById(lst, "ns1/gw2")), "deepeq")

	// 1 config from client2 watch (removed config never updated)
	s1 = watchConfig(ch2, 50*time.Millisecond)
	assert.NotNil(t, s1)
	s2 = watchConfig(ch2, 50*time.Millisecond)
	assert.NotNil(t, s2)
	// we do not know the order
	assert.True(t, s1.DeepEqual(sc1.Config) || s2.DeepEqual(sc1.Config), "config-deepeq")
	assert.True(t, client.IsConfigDeleted(s1) || client.IsConfigDeleted(s2), "deleted") // deleted
	// assert.True(t, s1.DeepEqual(sc1), "deepeq")
	// assert.True(t, client.IsConfigDeleted(s2), "deepeq") // deleted!

	// no config from client3 watch
	s = watchConfig(ch3, 50*time.Millisecond)
	assert.Nil(t, s, "config 3")

	// 1 config from client4 watch
	s = watchConfig(ch4, 50*time.Millisecond)
	assert.NotNil(t, s)
	assert.True(t, s.DeepEqual(sc1.Config), "deepeq")

	server.SuppressConfigDeletion = suppressConfigDeletion // reset
}

func TestClientReconnect(t *testing.T) {
	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	log := zlogger.WithName("tester")

	logger := logger.NewLoggerFactory(stunnerLogLevel)
	testLog := logger.NewLogger("test")

	// switch config deletions on
	suppressConfigDeletion := server.SuppressConfigDeletion
	server.SuppressConfigDeletion = true

	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	testCDSAddr := getRandCDSAddr()
	testLog.Debugf("create server on %s", testCDSAddr)
	srv := server.New(testCDSAddr, nil, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(serverCtx)
	assert.NoError(t, err, "start")

	testLog.Debug("create client")
	client1, err := client.New(testCDSAddr, "ns1/gw1", "", logger)
	assert.NoError(t, err, "client 1")

	testLog.Debug("watch: no result")
	ch1 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch1)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	err = client1.Watch(clientCtx, ch1, false)
	assert.NoError(t, err, "client 1 watch")

	s := watchConfig(ch1, 150*time.Millisecond)
	assert.Nil(t, s, "config 1")

	testLog.Debug("update")
	c1 := testConfig("ns1/gw1", "realm1")
	err = srv.UpdateConfig([]server.Config{c1})
	assert.NoError(t, err, "update")

	cs := srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 1, "snapshot len")
	ns, name, _ := server.NamespacedName("ns1/gw1")
	sc1, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get 1")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, c1.DeepEqual(sc1), "deepeq")

	// poll should have fed the config to the channels
	s = watchConfig(ch1, 500*time.Millisecond)
	assert.NotNil(t, s, "config 1")
	assert.True(t, s.DeepEqual(sc1.Config), "deepeq 1")

	log.Info("killing the connection of the watcher", "id", "ns1/gw1")
	conns := srv.GetConnTrack()
	assert.NotNil(t, conns)
	snapshot := conns.Snapshot()
	assert.Len(t, snapshot, 1)
	connId := snapshot[0].Id()
	srv.RemoveClient(connId)

	// after 2 pong-waits, client should have reconnected
	time.Sleep(client.RetryPeriod)
	time.Sleep(client.RetryPeriod)

	// watcher should receive its config
	s = watchConfig(ch1, 1500*time.Millisecond)
	assert.NotNil(t, s, "config 1")
	assert.True(t, s.DeepEqual(sc1.Config), "deepeq 1")

	server.SuppressConfigDeletion = suppressConfigDeletion // reset
}

// test server config update mechanism
func TestServerUpdate(t *testing.T) {
	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	log := zlogger.WithName("tester")

	logger := logger.NewLoggerFactory(stunnerLogLevel)
	testLog := logger.NewLogger("test")

	// switch config deletions off
	suppressConfigDeletion := server.SuppressConfigDeletion
	server.SuppressConfigDeletion = true

	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	testCDSAddr := getRandCDSAddr()
	testLog.Debugf("create server on %s", testCDSAddr)
	srv := server.New(testCDSAddr, nil, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(serverCtx)
	assert.NoError(t, err, "start")

	oldC, err := client.ParseConfig([]byte(`{"version":"v1","admin":{"name":"stunner/udp-gateway","logLevel":"all:INFO","health-check":"http://:8086"},"auth":{"realm":"stunner.l7mp.io","type":"static","credentials":{"username":"a","password":"b"}},"listeners":[{"name": "stunner/udp-gateway/udp-listener", "protocol":"turn-udp","address":"0.0.0.0","port":3478,"routes":["stunner/media-plane"]}],"clusters":[]}`))
	assert.NoError(t, oldC.Validate(), "validate")
	assert.NoError(t, err, "parse 1")

	testLog.Debug("upsert stunner/udp-gateway")
	srv.UpsertConfig("stunner/udp-gateway", oldC)

	cs := srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 1, "snapshot len")
	ns, name, _ := server.NamespacedName("stunner/udp-gateway")
	sc1, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get")
	assert.NotNil(t, sc1, "get")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, sc1.Config.DeepEqual(oldC), "deepeq")

	// reapply - no change
	testLog.Debug("re-apply stunner/udp-gateway")
	srv.UpsertConfig("stunner/udp-gateway", oldC)
	time.Sleep(20 * time.Millisecond) // let the server process

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 1, "snapshot len")
	ns, name, _ = server.NamespacedName("stunner/udp-gateway")
	sc1, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get 1")
	assert.NotNil(t, sc1, "get")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, sc1.Config.DeepEqual(oldC), "deepeq")

	// add another config
	tcpC, err := client.ParseConfig([]byte(`{"version":"v1","admin":{"name":"stunner/tcp-gateway","logLevel":"all:INFO","health-check":"http://:8086"},"auth":{"realm":"stunner.l7mp.io","type":"static","credentials":{"username":"a","password":"b"}},"listeners":[{"name": "stunner/tcp-gateway/tcp-listener", "protocol":"turn-tcp","address":"0.0.0.0","port":3478,"routes":["stunner/media-plane"]}],"clusters":[{"name":"stunner/media-plane", "type":"STATIC","protocol":"UDP","endpoints":["0.0.0.0/0"]}]}`))
	assert.NoError(t, tcpC.Validate(), "validate")
	assert.NoError(t, err, "parse")

	testLog.Debug("upsert stunner/tcp-gateway")
	srv.UpsertConfig("stunner/tcp-gateway", tcpC)
	time.Sleep(20 * time.Millisecond) // let the server process

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 2, "snapshot len")
	ns, name, _ = server.NamespacedName("stunner/udp-gateway")
	sc1, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get udp")
	assert.NotNil(t, sc1, "get")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, sc1.Config.DeepEqual(oldC), "deepeq")
	ns, name, _ = server.NamespacedName("stunner/tcp-gateway")
	sc2, ok := srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get tcp")
	assert.NotNil(t, sc2, "get")
	assert.NoError(t, sc2.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, sc2.Config.DeepEqual(tcpC), "deepeq")

	// add a cluster
	newC, err := client.ParseConfig([]byte(`{"version":"v1","admin":{"name":"stunner/udp-gateway","logLevel":"all:INFO","health-check":"http://:8086"},"auth":{"realm":"stunner.l7mp.io","type":"static","credentials":{"username":"a","password":"b"}},"listeners":[{"name": "stunner/udp-gateway/udp-listener", "protocol":"turn-udp","address":"0.0.0.0","port":3478,"routes":["stunner/media-plane"]}],"clusters":[{"name": "stunner/media-plane", "type":"STATIC","protocol":"UDP","endpoints":["0.0.0.0/0"]}]}`))
	assert.NoError(t, err, "parse 1")
	assert.NoError(t, newC.Validate(), "validate")
	assert.False(t, oldC.DeepEqual(newC), "deepeq")

	// process in a single go
	testLog.Debug("modify stunner/udp-gateway using UpdateConfig")
	err = srv.UpdateConfig([]server.Config{{
		Namespace: "stunner",
		Name:      "udp-gateway",
		Config:    newC,
	}, {
		Namespace: "stunner",
		Name:      "tcp-gateway",
		Config:    tcpC,
	}})
	assert.NoError(t, err, "parse 1")

	time.Sleep(20 * time.Millisecond) // let the server process

	cs = srv.GetConfigStore().Snapshot()
	assert.Len(t, cs, 2, "snapshot len")
	ns, name, _ = server.NamespacedName("stunner/udp-gateway")
	sc1, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get udp")
	assert.NotNil(t, sc1, "get")
	assert.NoError(t, sc1.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, sc1.Config.DeepEqual(newC), "deepeq")
	ns, name, _ = server.NamespacedName("stunner/tcp-gateway")
	sc2, ok = srv.GetConfigStore().Get(ns, name)
	assert.True(t, ok, "get tcp")
	assert.NotNil(t, sc2, "get")
	assert.NoError(t, sc2.Config.Validate(), "valid") // cds config store does not validate
	assert.True(t, sc2.Config.DeepEqual(tcpC), "deepeq")

	server.SuppressConfigDeletion = suppressConfigDeletion // reset
}

// Test various combinations of server-side "drop-delete" (server.SuppressConfigDeletion=true) and
// client-side "drop-delete" (client.Watch(..., suppressDelete=true)).
func TestDeleteConfigAPI(t *testing.T) {
	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	log := zlogger.WithName("tester")

	logger := logger.NewLoggerFactory(stunnerLogLevel)
	testLog := logger.NewLogger("test")

	suppressConfigDeletion := server.SuppressConfigDeletion
	server.SuppressConfigDeletion = false
	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	testCDSAddr := getRandCDSAddr()
	testLog.Debugf("create server on %s", testCDSAddr)
	srv := server.New(testCDSAddr, nil, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(serverCtx)
	assert.NoError(t, err, "start")

	testLog.Debug("create client")
	c, err := client.New(testCDSAddr, "ns1/gw1", "", logger)
	assert.NoError(t, err, "client")

	ch := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch)

	for _, testCase := range []struct {
		name                         string
		serverDropDel, clientDropDel bool
		tester                       func(t *testing.T)
	}{
		{
			name:          "server sends delete - client handles delete",
			serverDropDel: false,
			clientDropDel: false,
			tester: func(t *testing.T) {
				conf := watchConfig(ch, 50*time.Millisecond)
				assert.NotNil(t, conf, "config")
				assert.True(t, client.IsConfigDeleted(conf))
			},
		},
		{
			name:          "server suppresses delete - client handles delete",
			serverDropDel: true,
			clientDropDel: false,
			tester: func(t *testing.T) {
				conf := watchConfig(ch, 50*time.Millisecond)
				assert.Nil(t, conf, "config")
			},
		},
		{
			name:          "server sends delete - client suppresses delete",
			serverDropDel: false,
			clientDropDel: true,
			tester: func(t *testing.T) {
				conf := watchConfig(ch, 50*time.Millisecond)
				assert.Nil(t, conf, "config")
			},
		},
		{
			name:          "server suppresses delete - client suppresses delete",
			serverDropDel: true,
			clientDropDel: true,
			tester: func(t *testing.T) {
				conf := watchConfig(ch, 50*time.Millisecond)
				assert.Nil(t, conf, "config")
			},
		},
	} {
		testLog.Debugf("------------------------- %s ----------------------", testCase.name)

		server.SuppressConfigDeletion = testCase.serverDropDel

		clientCtx, clientCancel := context.WithCancel(context.Background())
		err = c.Watch(clientCtx, ch, testCase.clientDropDel)
		assert.NoError(t, err, "client watch")

		conf := watchConfig(ch, 25*time.Millisecond)
		assert.Nil(t, conf, "noconfig")

		testLog.Trace("Adding config")
		testConf := testConfig("ns1/gw1", "realm1")
		err = srv.UpdateConfig([]server.Config{testConf})
		assert.NoError(t, err, "update")

		conf = watchConfig(ch, 50*time.Millisecond)
		assert.NotNil(t, conf)
		assert.Equal(t, *testConf.Config, *conf)

		testLog.Trace("Deleting config")
		err = srv.UpdateConfig([]server.Config{})
		assert.NoError(t, err, "update")
		testCase.tester(t)

		clientCancel()
	}

	server.SuppressConfigDeletion = suppressConfigDeletion
}

func TestServerLoadWithNodeName(t *testing.T) {
	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	log := zlogger.WithName("tester")

	logger := logger.NewLoggerFactory(stunnerLogLevel)
	testLog := logger.NewLogger("test")

	// suppress deletions
	suppressConfigDeletion := server.SuppressConfigDeletion
	server.SuppressConfigDeletion = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testCDSAddr := getRandCDSAddr()
	testLog.Debugf("create server on %s", testCDSAddr)
	patcher := func(conf *stnrv1.StunnerConfig, node string) *stnrv1.StunnerConfig {
		// rewrite the realm to the node name
		if node != "" {
			conf.Auth.Realm = node
		}
		return conf
	}
	srv := server.New(testCDSAddr, patcher, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(ctx)
	assert.NoError(t, err, "start")

	time.Sleep(20 * time.Millisecond)

	testLog.Debug("create client")
	client1, err := client.New(testCDSAddr, "ns1/gw1", "node1", logger)
	assert.NoError(t, err, "client 1")
	client2, err := client.New(testCDSAddr, "ns1/gw2", "", logger)
	assert.NoError(t, err, "client 2")

	testLog.Debug("load: error")
	c, err := client1.Load()
	assert.Error(t, err, "load")
	assert.Nil(t, c, "conf")
	c, err = client2.Load()
	assert.Error(t, err, "load")
	assert.Nil(t, c, "conf")

	c1 := testConfig("ns1/gw1", "realm1")
	c2 := testConfig("ns1/gw2", "realm1")
	err = srv.UpdateConfig([]server.Config{c1, c2})
	assert.NoError(t, err, "update")

	testLog.Debug("load: config ok")
	c, err = client1.Load()
	assert.NoError(t, err, "load")
	assert.Equal(t, "node1", c.Auth.Realm, "node name 1")
	c.Auth.Realm = "realm1" // reset
	assert.True(t, c.DeepEqual(c1.Config), "deepeq")
	c, err = client2.Load()
	assert.NoError(t, err, "load")
	assert.Equal(t, "realm1", c.Auth.Realm, "node name 1") // node node: no patch
	assert.True(t, c.DeepEqual(c2.Config), "deepeq")

	server.SuppressConfigDeletion = suppressConfigDeletion
}

func TestServerWatchWithNodeName(t *testing.T) {
	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	log := zlogger.WithName("tester")

	logger := logger.NewLoggerFactory(stunnerLogLevel)
	testLog := logger.NewLogger("test")

	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	testCDSAddr := getRandCDSAddr()
	testLog.Debugf("create server on %s", testCDSAddr)
	patcher := func(conf *stnrv1.StunnerConfig, node string) *stnrv1.StunnerConfig {
		// rewrite the realm to the node name
		if node != "" {
			conf.Auth.Realm = node
		}
		return conf
	}
	srv := server.New(testCDSAddr, patcher, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(serverCtx)
	assert.NoError(t, err, "start")

	testLog.Debug("create client")
	client1, err := client.New(testCDSAddr, "ns1/gw1", "", logger)
	assert.NoError(t, err, "client 1")
	client2, err := client.New(testCDSAddr, "ns1/gw2", "node2", logger)
	assert.NoError(t, err, "client 2")

	testLog.Debug("watch: no result")
	ch1 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch1)
	ch2 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch2)

	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()
	err = client1.Watch(clientCtx, ch1, false)
	assert.NoError(t, err, "client 1 watch")
	err = client2.Watch(clientCtx, ch2, false)
	assert.NoError(t, err, "client 2 watch")

	testLog.Debug("poll")
	c1 := testConfig("ns1/gw1", "realm1")
	c2 := testConfig("ns1/gw2", "realm1")
	err = srv.UpdateConfig([]server.Config{c1, c2})
	assert.NoError(t, err, "update")

	// poll should have fed the configs to the channels
	s := watchConfig(ch1, 500*time.Millisecond)
	assert.NotNil(t, s, "config 1")
	assert.Equal(t, "realm1", s.Auth.Realm, "node name 1")
	assert.True(t, s.DeepEqual(c1.Config), "deepeq 1")
	s = watchConfig(ch2, 500*time.Millisecond)
	assert.NotNil(t, s, "config 2")
	assert.Equal(t, "node2", s.Auth.Realm, "node name 2")
	s.Auth.Realm = "realm1" // reset
	assert.True(t, s.DeepEqual(c2.Config), "deepeq 2")
}

func TestLicenseLoad(t *testing.T) {
	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	log := zlogger.WithName("tester")

	logger := logger.NewLoggerFactory(stunnerLogLevel)
	testLog := logger.NewLogger("test")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testCDSAddr := getRandCDSAddr()
	testLog.Debugf("create server on %s", testCDSAddr)
	srv := server.New(testCDSAddr, nil, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(ctx)
	assert.NoError(t, err, "start")

	testLog.Debug("create client")
	licenseClient, err := client.NewLicenseStatusClient(testCDSAddr, logger.NewLogger("license-client"))
	assert.NoError(t, err, "new client")

	testLog.Debug("get empty license status")
	s, err := licenseClient.LicenseStatus(ctx)
	assert.NoError(t, err, "get")
	assert.Equal(t, stnrv1.NewEmptyLicenseStatus(), s, "get empty license status")

	testLog.Debug("set server-side license status")
	s = stnrv1.LicenseStatus{
		EnabledFeatures:  []string{"a", "b", "c"},
		SubscriptionType: "test-tier",
		LastUpdated:      "never",
		LastError:        "",
	}
	srv.UpdateLicenseStatus(s)

	testLog.Debug("get license status")
	s2, err := licenseClient.LicenseStatus(ctx)
	assert.NoError(t, err, "get")
	assert.Equal(t, s, s2, "get license status")
}

// the relevant parts from Kubernetes corev1

type nodeAddressType string

const (
	nodeHostName    nodeAddressType = "Hostname"
	nodeInternalIP  nodeAddressType = "InternalIP"
	nodeExternalIP  nodeAddressType = "ExternalIP"
	nodeInternalDNS nodeAddressType = "InternalDNS"
	nodeExternalDNS nodeAddressType = "ExternalDNS"
)

type nodeAddress struct {
	aType   nodeAddressType
	address string
}

type node struct {
	name      string
	addresses []nodeAddress
}

func TestServerPatcher(t *testing.T) {
	testNodes := map[string]node{
		"node1": node{
			name: "node1",
			addresses: []nodeAddress{
				{aType: nodeExternalDNS, address: "node1.com"},
				{aType: nodeInternalIP, address: "1.2.3.5"},
				{aType: nodeExternalIP, address: "1.2.3.4"},
			},
		},
		"node2": node{
			name: "node2",
			addresses: []nodeAddress{
				{aType: nodeInternalDNS, address: "node2.com"},
				{aType: nodeInternalIP, address: "1.2.3.5"},
			},
		},
	}

	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	assert.NoError(t, err, "logger created")
	zlogger := zapr.NewLogger(z)
	log := zlogger.WithName("tester")

	logger := logger.NewLoggerFactory(stunnerLogLevel)
	testLog := logger.NewLogger("test")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testLog.Debug("create server")
	patcher := func(conf *stnrv1.StunnerConfig, node string) *stnrv1.StunnerConfig {
		if conf == nil {
			return conf
		}
		if n, ok := testNodes[node]; ok {
			for _, a := range n.addresses {
				if a.aType == nodeExternalIP {
					c := conf.DeepCopy()
					for i := range c.Listeners {
						c.Listeners[i].Addr = a.address
					}
					testLog.Tracef("patching ready: %s", c.String())
					return c
				}
			}
		}
		testLog.Tracef("not patching config: %s", conf.String())
		return conf
	}
	testCDSAddr := getRandCDSAddr()
	srv := server.New(testCDSAddr, patcher, log)
	assert.NotNil(t, srv, "server")
	err = srv.Start(ctx)
	assert.NoError(t, err, "start")

	time.Sleep(20 * time.Millisecond)

	c := testConfigListener("ns1/gw1", "realm1")
	err = srv.UpdateConfig([]server.Config{c})
	assert.NoError(t, err, "update")

	testLog.Debug("client 1")
	client1, err := client.New(testCDSAddr, "ns1/gw1", "node1", logger)
	assert.NoError(t, err, "client")
	c1, err := client1.Load()
	assert.NoError(t, err, "load")
	// should update listener address
	assert.Len(t, c1.Listeners, 2, "listeners")
	assert.Equal(t, "1.2.3.4", c1.Listeners[0].Addr, "listeners")
	assert.Equal(t, "1.2.3.4", c1.Listeners[1].Addr, "listeners")

	testLog.Debug("client 2")
	client2, err := client.New(testCDSAddr, "ns1/gw1", "node2", logger)
	assert.NoError(t, err, "client")
	c2, err := client2.Load()
	assert.NoError(t, err, "load")
	// no external ip on node2: no patch
	assert.Equal(t, c.Config, c2, "deepeq")

	testLog.Debug("client 3")
	client3, err := client.New(testCDSAddr, "ns1/gw1", "node3", logger)
	assert.NoError(t, err, "client")
	c3, err := client3.Load()
	assert.NoError(t, err, "load")
	// no node for config: no patch
	assert.Equal(t, c.Config, c3, "deepeq")

	testLog.Debug("firing watchers")
	ch1 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch1)
	err = client1.Watch(ctx, ch1, false)
	assert.NoError(t, err, "client watch")
	s1 := watchConfig(ch1, 100*time.Millisecond)
	assert.NotNil(t, s1, "watch-config")
	// patched
	assert.Len(t, s1.Listeners, 2, "listeners")
	assert.Equal(t, "1.2.3.4", s1.Listeners[0].Addr, "listeners")
	assert.Equal(t, "1.2.3.4", s1.Listeners[1].Addr, "listeners")

	ch2 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch2)
	err = client2.Watch(ctx, ch2, false)
	assert.NoError(t, err, "client watch")
	s2 := watchConfig(ch2, 100*time.Millisecond)
	assert.NotNil(t, s2, "watch-config")
	// no external ip on node2: no patch
	assert.Equal(t, c.Config, s2, "deepeq")

	ch3 := make(chan *stnrv1.StunnerConfig, 8)
	defer close(ch3)
	err = client3.Watch(ctx, ch3, false)
	assert.NoError(t, err, "client watch")
	s3 := watchConfig(ch3, 100*time.Millisecond)
	assert.NotNil(t, s3, "watch-config")
	// no node for config: no patch
	assert.Equal(t, c.Config, s3, "deepeq")

	testLog.Debug("add an external address on node2 and broadcast")
	testNodes["node2"].addresses[1].aType = nodeExternalIP
	srv.PushNodeConfig("node2")

	// no update on client 1
	s1 = watchConfig(ch1, 20*time.Millisecond)
	assert.Nil(t, s1, "watch-config")

	// update client 2
	s2 = watchConfig(ch2, 100*time.Millisecond)
	assert.NotNil(t, s2, "watch-config")
	// check the new external ip
	assert.Len(t, s2.Listeners, 2, "listeners")
	assert.Equal(t, "1.2.3.5", s2.Listeners[0].Addr, "listeners")
	assert.Equal(t, "1.2.3.5", s2.Listeners[1].Addr, "listeners")

	// no update on client 3
	s3 = watchConfig(ch3, 20*time.Millisecond)
	assert.Nil(t, s3, "watch-config")

	testLog.Debug("create node3")
	testNodes["node3"] = node{
		name: "node3",
		addresses: []nodeAddress{
			{aType: nodeExternalIP, address: "1.2.3.6"},
		},
	}
	srv.PushNodeConfig("node3")

	// no update on client 1
	s1 = watchConfig(ch1, 20*time.Millisecond)
	assert.Nil(t, s1, "watch-config")

	// no update on client 2
	s2 = watchConfig(ch2, 20*time.Millisecond)
	assert.Nil(t, s2, "watch-config")

	// update client 3
	s3 = watchConfig(ch3, 100*time.Millisecond)
	assert.NotNil(t, s3, "watch-config")
	// check the new external ip
	assert.Len(t, s3.Listeners, 2, "listeners")
	assert.Equal(t, "1.2.3.6", s3.Listeners[0].Addr, "listeners")
	assert.Equal(t, "1.2.3.6", s3.Listeners[1].Addr, "listeners")
}

// only differ in id and realm
func testConfig(id, realm string) server.Config {
	c := client.ZeroConfig(id)
	c.Auth.Realm = realm
	namespace, name, _ := server.NamespacedName(id)
	_ = c.Validate() // make sure deepeq works
	return server.Config{Namespace: namespace, Name: name, Config: c}
}

// with 2 listeners
func testConfigListener(id, realm string) server.Config {
	c := client.ZeroConfig(id)
	c.Auth.Realm = realm
	c.Listeners = []stnrv1.ListenerConfig{
		{Name: "l1", Protocol: "TCP", Addr: "1.1.1.1", Port: 3478},
		{Name: "l2", Protocol: "UDP", Addr: "1.1.1.2", Port: 3479},
	}
	_ = c.Validate() // make sure deepeq works
	namespace, name, _ := server.NamespacedName(id)
	return server.Config{Namespace: namespace, Name: name, Config: c}
}

// wait for some configurable time for a watch element
func watchConfig(ch chan *stnrv1.StunnerConfig, d time.Duration) *stnrv1.StunnerConfig {
	select {
	case c := <-ch:
		// fmt.Println("++++++++++++ got config ++++++++++++: ", c.String())
		return c
	case <-time.After(d):
		// fmt.Println("++++++++++++ timeout ++++++++++++")
		return nil
	}
}

func findConfById(cs []*stnrv1.StunnerConfig, id string) *stnrv1.StunnerConfig {
	for i := range cs {
		if cs[i] != nil && cs[i].Admin.Name == id {
			return cs[i]
		}
	}

	return nil
}
