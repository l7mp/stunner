package config

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/zapr"
	"github.com/go-openapi/testify/v2/require"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/config/client"
	"github.com/l7mp/stunner/v2/pkg/config/server"
	"github.com/l7mp/stunner/v2/pkg/logger"
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
	client1, err := client.New(testCDSAddr, "ns1/gw1", nil, logger)
	assert.NoError(t, err, "client 1")
	client2, err := client.New(testCDSAddr, "ns1/gw2", nil, logger)
	assert.NoError(t, err, "client 2")
	// nonexistent
	client3, err := client.New(testCDSAddr, "ns1/gw3", nil, logger)
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
	client1, err := client.New(testCDSAddr, "ns1/gw1", nil, logger)
	assert.NoError(t, err, "client 1")
	client2, err := client.New(testCDSAddr, "ns1/gw2", nil, logger)
	assert.NoError(t, err, "client 2")
	client3, err := client.New(testCDSAddr, "ns1/gw3", nil, logger)
	assert.NoError(t, err, "client 3")

	testLog.Debug("poll: no result")
	ch1 := make(chan *stnrv2.StunnerConfig, 8)
	defer close(ch1)
	ch2 := make(chan *stnrv2.StunnerConfig, 8)
	defer close(ch2)
	ch3 := make(chan *stnrv2.StunnerConfig, 8)
	defer close(ch3)

	go func() {
		err := client1.Poll(ctx, ch1, false)
		assert.NoError(t, err, "client 1 cancelled")
	}()
	go func() {
		err := client2.Poll(ctx, ch2, false)
		assert.NoError(t, err, "client 2 cancelled")
	}()
	go func() {
		err := client3.Poll(ctx, ch3, false)
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
	client1, err := client.New(testCDSAddr, "ns1/gw1", nil, logger)
	assert.NoError(t, err, "client 1")
	client2, err := client.New(testCDSAddr, "ns1/gw2", nil, logger)
	assert.NoError(t, err, "client 2")
	client3, err := client.New(testCDSAddr, "ns1/gw3", nil, logger)
	assert.NoError(t, err, "client 3")

	testLog.Debug("watch: no result")
	ch1 := make(chan *stnrv2.StunnerConfig, 8)
	defer close(ch1)
	ch2 := make(chan *stnrv2.StunnerConfig, 8)
	defer close(ch2)
	ch3 := make(chan *stnrv2.StunnerConfig, 8)
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
	client1, err := client.New(testCDSAddr, "ns1/gw1", nil, logger)
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
	ch1 := make(chan *stnrv2.StunnerConfig, 8)
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
	client4, err := client.NewConfigNamespaceNameAPI(testCDSAddr, "ns1", "gw1", nil, logger.NewLogger("gw-config-client"))
	assert.NoError(t, err, "client 4")

	testLog.Debug("watch: no result")
	ch1 := make(chan *stnrv2.StunnerConfig, 8)
	defer close(ch1)
	ch2 := make(chan *stnrv2.StunnerConfig, 8)
	defer close(ch2)
	ch3 := make(chan *stnrv2.StunnerConfig, 8)
	defer close(ch3)
	ch4 := make(chan *stnrv2.StunnerConfig, 8)
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
	testLog.Debug("update1: ns1/gw1 + ns2/gw1      ")
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
	lst := []*stnrv2.StunnerConfig{s1, s2}
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
	testLog.Debug("update1: ns1/gw1 + ns1/gw2      ")
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
	configNs2Gw1 := &stnrv2.StunnerConfig{}
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
	lst = []*stnrv2.StunnerConfig{s1, s2}
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
	lst = []*stnrv2.StunnerConfig{s1, s2}
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
	testLog.Debug("restart + Update1: ns1/gw1 + ns2/gw1 + ns1/gw2")
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
	lst = []*stnrv2.StunnerConfig{s1, s2, s3}
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
	lst = []*stnrv2.StunnerConfig{s1, s2}
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
	testLog.Debug("update1: ns1/gw1 + ns3/gw1      ")
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
	lst = []*stnrv2.StunnerConfig{s1, s2, s3}
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
	client1, err := client.New(testCDSAddr, "ns1/gw1", nil, logger)
	assert.NoError(t, err, "client 1")

	testLog.Debug("watch: no result")
	ch1 := make(chan *stnrv2.StunnerConfig, 8)
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

	oldC := &stnrv2.StunnerConfig{}
	err = json.Unmarshal([]byte(`{"version":"v2","admin":{"name":"stunner/udp-gateway","loglevel":"all:INFO"},"auth":{"realm":"stunner.l7mp.io","type":"static","credentials":{"username":"a","password":"b"}},"listeners":[{"name":"stunner/udp-gateway/udp-listener","protocol":"UDP","port":3478,"servers":["stunner/udp-gateway/udp-listener"]}],"servers":[{"name":"stunner/udp-gateway/udp-listener","type":"turn","clusters":["stunner/media-plane"]}],"clusters":[]}`), oldC)
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
	tcpC := &stnrv2.StunnerConfig{}
	err = json.Unmarshal([]byte(`{"version":"v2","admin":{"name":"stunner/tcp-gateway","loglevel":"all:INFO"},"auth":{"realm":"stunner.l7mp.io","type":"static","credentials":{"username":"a","password":"b"}},"listeners":[{"name":"stunner/tcp-gateway/tcp-listener","protocol":"TCP","port":3478,"servers":["stunner/tcp-gateway/tcp-listener"]}],"servers":[{"name":"stunner/tcp-gateway/tcp-listener","type":"turn","clusters":["stunner/media-plane"]}],"clusters":[{"name":"stunner/media-plane","type":"STATIC","protocol":"UDP","endpoints":["0.0.0.0/0"]}]}`), tcpC)
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
	newC := &stnrv2.StunnerConfig{}
	err = json.Unmarshal([]byte(`{"version":"v2","admin":{"name":"stunner/udp-gateway","loglevel":"all:INFO"},"auth":{"realm":"stunner.l7mp.io","type":"static","credentials":{"username":"a","password":"b"}},"listeners":[{"name":"stunner/udp-gateway/udp-listener","protocol":"UDP","port":3478,"servers":["stunner/udp-gateway/udp-listener"]}],"servers":[{"name":"stunner/udp-gateway/udp-listener","type":"turn","clusters":["stunner/media-plane"]}],"clusters":[{"name":"stunner/media-plane","type":"STATIC","protocol":"UDP","endpoints":["0.0.0.0/0"]}]}`), newC)
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
	c, err := client.New(testCDSAddr, "ns1/gw1", nil, logger)
	assert.NoError(t, err, "client")

	ch := make(chan *stnrv2.StunnerConfig, 8)
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

		testLog.Trace("adding config")
		testConf := testConfig("ns1/gw1", "realm1")
		err = srv.UpdateConfig([]server.Config{testConf})
		assert.NoError(t, err, "update")

		conf = watchConfig(ch, 50*time.Millisecond)
		assert.NotNil(t, conf)
		assert.Equal(t, *testConf.Config, *conf)

		testLog.Trace("deleting config")
		err = srv.UpdateConfig([]server.Config{})
		assert.NoError(t, err, "update")
		testCase.tester(t)

		clientCancel()
	}

	server.SuppressConfigDeletion = suppressConfigDeletion
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
	assert.Equal(t, stnrv2.NewEmptyLicenseStatus(), s, "get empty license status")

	testLog.Debug("set server-side license status")
	srv.UpdateLicenseStatus(stnrv2.LicenseStatus{
		EnabledFeatures:  []string{"a", "b", "c"},
		SubscriptionType: "test-tier",
		LastUpdated:      "never",
		LastError:        "",
	})

	testLog.Debug("get license status")
	s, err = licenseClient.LicenseStatus(ctx)
	assert.NoError(t, err, "get")
	assert.Equal(t, stnrv2.LicenseStatus{
		EnabledFeatures:  []string{"a", "b", "c"},
		SubscriptionType: "test-tier",
		LastUpdated:      "never",
		LastError:        "",
	}, s, "get license status")
}

// nodeTable is the node address table of the patcher tests, changed under the server.
type nodeTable struct{ sync.Map } // node -> address

func newNodeTable(addrs map[string]string) *nodeTable {
	n := &nodeTable{}
	for node, a := range addrs {
		n.Store(node, a)
	}
	return n
}

// patch relays a client at the address of its node: it replaces the node address marker among the
// cluster addresses, and leaves it for the client's environment on a node without an address. A
// tenant label replaces the whole auth block.
func (n *nodeTable) patch(conf *stnrv2.StunnerConfig, _ string, labels map[string]string) *stnrv2.StunnerConfig {
	if a, ok := n.Load(labels[stnrv2.DefaultCDSNodeLabel]); ok {
		for i := range conf.Clusters {
			for j, addr := range conf.Clusters[i].Addrs {
				if addr == "$"+stnrv2.DefaultEnvVarNodeAddr {
					conf.Clusters[i].Addrs[j] = a.(string)
				}
			}
		}
	}
	if tenant, ok := labels["tenant"]; ok {
		conf.Auth = stnrv2.AuthConfig{Type: "static", Realm: tenant,
			Credentials: map[string]string{"username": tenant, "password": tenant + "-pass"}}
	}
	return conf
}

// nodeAddr returns the address of a node, "" if it has none.
func (n *nodeTable) nodeAddr(node string) string {
	if a, ok := n.Load(node); ok {
		return a.(string)
	}
	return ""
}

func (n *nodeTable) set(node, addr string) {
	if addr == "" {
		n.Delete(node)
		return
	}
	n.Store(node, addr)
}

// startPatchServer starts a CDS server patching with a node table, sending deletions whatever an
// earlier test left the global switch at.
func startPatchServer(t *testing.T, nodes *nodeTable) (*server.Server, string) {
	t.Helper()
	suppress := server.SuppressConfigDeletion
	server.SuppressConfigDeletion = false
	t.Cleanup(func() { server.SuppressConfigDeletion = suppress })
	zc := zap.NewProductionConfig()
	zc.Level = zap.NewAtomicLevelAt(testerLogLevel)
	z, err := zc.Build()
	require.NoError(t, err)
	addr := getRandCDSAddr()
	srv := server.New(addr, nodes.patch, zapr.NewLogger(z).WithName("cds-server"))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, srv.Start(ctx))
	time.Sleep(20 * time.Millisecond)
	return srv, addr
}

// patchConfig is a config whose cluster relays at the node address marker, with a credential
// that looks like a variable.
func patchConfig(t *testing.T, id, realm string) server.Config {
	c := zeroConfig(id, realm)
	c.Auth.Credentials["password"] = "pass-$" + stnrv2.DefaultEnvVarNodeAddr
	c.Listeners = []stnrv2.ListenerConfig{{Name: "l1", Protocol: "UDP", Port: 3478,
		Servers: []string{"turn"}}}
	c.Servers = []stnrv2.ServerConfig{{Name: "turn", Type: "turn", Clusters: []string{"c1"}}}
	c.Clusters = []stnrv2.ClusterConfig{{Name: "c1", Endpoints: []string{"10.0.0.0/8"},
		Protocol: "UDP", Addrs: []string{"$" + stnrv2.DefaultEnvVarNodeAddr}}}
	require.NoError(t, c.Validate())
	namespace, name, _ := server.NamespacedName(id)
	return server.Config{Namespace: namespace, Name: name, Config: c}
}

// nodeLabels returns the labels of a client on a node, none for "".
func nodeLabels(node string) map[string]string {
	if node == "" {
		return nil
	}
	return map[string]string{stnrv2.DefaultCDSNodeLabel: node}
}

// watchClient starts a watcher of a config for a client with labels.
func watchClient(t *testing.T, addr, id string, labels map[string]string) chan *stnrv2.StunnerConfig {
	t.Helper()
	c, err := client.New(addr, id, labels, logger.NewLoggerFactory(stunnerLogLevel))
	require.NoError(t, err)
	ch := make(chan *stnrv2.StunnerConfig, 1024)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, c.Watch(ctx, ch, false))
	return ch
}

// relayAddrs returns the relay addresses a client has for cluster c1.
func relayAddrs(t *testing.T, c *stnrv2.StunnerConfig) []string {
	t.Helper()
	require.NotNil(t, c, "config")
	return clusterAddrs(t, c, "c1")
}

// TestCDSPatchLoad pins a single read: the config patched for the labels of the client, a field
// and a whole sub-object alike, and the marker left as it is where the patcher leaves it (a
// single read substitutes no environment).
func TestCDSPatchLoad(t *testing.T) {
	t.Setenv(stnrv2.DefaultEnvVarNodeAddr, "9.9.9.9")
	nodes := newNodeTable(map[string]string{"node1": "1.1.1.1"})
	srv, addr := startPatchServer(t, nodes)
	require.NoError(t, srv.UpdateConfig([]server.Config{patchConfig(t, "ns1/gw1", "realm1")}))

	for _, tc := range []struct {
		labels      map[string]string
		addr, realm string
	}{
		{nodeLabels("node1"), "1.1.1.1", "realm1"},
		{nodeLabels("node2"), "9.9.9.9", "realm1"}, // no node address: the pod environment
		{nil, "9.9.9.9", "realm1"},                 // no labels: the pod environment
		{map[string]string{stnrv2.DefaultCDSNodeLabel: "node1", "tenant": "t1"}, "1.1.1.1", "t1"},
	} {
		c, err := client.New(addr, "ns1/gw1", tc.labels, logger.NewLoggerFactory(stunnerLogLevel))
		require.NoError(t, err)
		conf, err := c.Load()
		require.NoError(t, err, "labels %v", tc.labels)
		assert.Equal(t, []string{tc.addr}, relayAddrs(t, conf), "labels %v", tc.labels)
		assert.Equal(t, tc.realm, conf.Auth.Realm, "labels %v: the auth block", tc.labels)
		if tc.realm == "realm1" {
			assert.Equal(t, "pass-$"+stnrv2.DefaultEnvVarNodeAddr, conf.Auth.Credentials["password"],
				"labels %v: the client never substitutes credentials", tc.labels)
		}
	}
}

// TestCDSPatchWatch pins a watch: the config patched for the labels of the client, and a marker
// the patcher leaves falling back to the client's environment, credentials excepted.
func TestCDSPatchWatch(t *testing.T) {
	t.Setenv(stnrv2.DefaultEnvVarNodeAddr, "9.9.9.9")
	nodes := newNodeTable(map[string]string{"node1": "1.1.1.1", "node2": "2.2.2.2"})
	srv, addr := startPatchServer(t, nodes)
	require.NoError(t, srv.UpdateConfig([]server.Config{patchConfig(t, "ns1/gw1", "realm1")}))

	for _, tc := range []struct{ node, want string }{
		{"node1", "1.1.1.1"},
		{"node2", "2.2.2.2"},
		{"node3", "9.9.9.9"}, // a node without an address: the pod environment
		{"", "9.9.9.9"},      // no labels: the pod environment
	} {
		c := watchConfig(watchClient(t, addr, "ns1/gw1", nodeLabels(tc.node)), time.Second)
		assert.Equal(t, []string{tc.want}, relayAddrs(t, c), "node %q", tc.node)
		assert.Equal(t, "pass-$"+stnrv2.DefaultEnvVarNodeAddr, c.Auth.Credentials["password"],
			"node %q: the client never substitutes credentials", tc.node)
	}

	c := watchConfig(watchClient(t, addr, "ns1/gw1", map[string]string{"tenant": "t1"}), time.Second)
	require.NotNil(t, c, "a tenant client")
	assert.Equal(t, "t1", c.Auth.Realm, "a whole sub-object rewritten")
	assert.Equal(t, "t1-pass", c.Auth.Credentials["password"])
}

// TestCDSRefresh pins that a client gets each config it would get exactly once: a refresh pushes
// only the clients whose node changed, nothing reaches a client whose config did not change, and a
// changed config, a deletion and a re-addition reach every client, specialized for its node.
func TestCDSRefresh(t *testing.T) {
	t.Setenv(stnrv2.DefaultEnvVarNodeAddr, "9.9.9.9")
	nodes := newNodeTable(map[string]string{"node1": "1.1.1.1", "node2": "2.2.2.2"})
	srv, addr := startPatchServer(t, nodes)
	require.NoError(t, srv.UpdateConfig([]server.Config{patchConfig(t, "ns1/gw1", "realm1")}))

	names := []string{"node1", "node2", "node3", ""}
	chs := map[string]chan *stnrv2.StunnerConfig{}
	for _, n := range names {
		chs[n] = watchClient(t, addr, "ns1/gw1", nodeLabels(n))
	}
	expect := func(step string, want map[string]string) {
		t.Helper()
		for _, n := range names {
			c := watchConfig(chs[n], 300*time.Millisecond)
			w, ok := want[n]
			if !ok {
				assert.Nil(t, c, "%s: node %q gets nothing", step, n)
				continue
			}
			if assert.NotNil(t, c, "%s: node %q gets an update", step, n) {
				assert.Equal(t, []string{w}, relayAddrs(t, c), "%s: node %q", step, n)
			}
		}
	}

	expect("initial", map[string]string{"node1": "1.1.1.1", "node2": "2.2.2.2",
		"node3": "9.9.9.9", "": "9.9.9.9"})

	srv.Refresh()
	expect("a refresh with nothing changed", map[string]string{})

	nodes.set("node1", "1.1.1.2")
	srv.Refresh()
	expect("a node address changed", map[string]string{"node1": "1.1.1.2"})

	nodes.set("node3", "3.3.3.3")
	srv.Refresh()
	expect("a node got an address", map[string]string{"node3": "3.3.3.3"})

	nodes.set("node2", "")
	srv.Refresh()
	expect("a node lost its address", map[string]string{"node2": "9.9.9.9"})

	require.NoError(t, srv.UpdateConfig([]server.Config{patchConfig(t, "ns1/gw1", "realm1")}))
	expect("the same config again", map[string]string{})

	require.NoError(t, srv.UpdateConfig([]server.Config{patchConfig(t, "ns1/gw1", "realm2")}))
	expect("a changed config", map[string]string{"node1": "1.1.1.2", "node2": "9.9.9.9",
		"node3": "3.3.3.3", "": "9.9.9.9"})

	require.NoError(t, srv.UpdateConfig([]server.Config{}))
	for _, n := range names {
		c := watchConfig(chs[n], time.Second)
		assert.True(t, client.IsConfigDeleted(c), "node %q gets the deletion", n)
	}
	srv.Refresh()
	expect("a refresh after the deletion", map[string]string{})

	require.NoError(t, srv.UpdateConfig([]server.Config{patchConfig(t, "ns1/gw1", "realm2")}))
	expect("the re-added config", map[string]string{"node1": "1.1.1.2", "node2": "9.9.9.9",
		"node3": "3.3.3.3", "": "9.9.9.9"})
}

// TestCDSNoSwallowedUpdate races config updates against node address changes and refreshes, and
// pins that no client ever goes back to an older config and that every client ends up with the
// last config, specialized for the last address of its node.
func TestCDSNoSwallowedUpdate(t *testing.T) {
	t.Setenv(stnrv2.DefaultEnvVarNodeAddr, "9.9.9.9")
	nodes := newNodeTable(map[string]string{"node1": "10.0.1.0", "node2": "10.0.2.0",
		"node3": "10.0.3.0"})
	srv, addr := startPatchServer(t, nodes)
	require.NoError(t, srv.UpdateConfig([]server.Config{patchConfig(t, "ns1/gw1", "gen-000")}))

	type watcher struct {
		node string
		mu   sync.Mutex
		gens []int
		last *stnrv2.StunnerConfig
	}
	watchers := []*watcher{}
	for _, n := range []string{"node1", "node1", "node2", "node2", "node3", "node3", ""} {
		w := &watcher{node: n}
		watchers = append(watchers, w)
		ch := watchClient(t, addr, "ns1/gw1", nodeLabels(n))
		go func() {
			for c := range ch {
				gen := -1
				_, err := fmt.Sscanf(c.Auth.Realm, "gen-%d", &gen)
				w.mu.Lock()
				if err == nil {
					w.gens = append(w.gens, gen)
				}
				w.last = c
				w.mu.Unlock()
			}
		}()
	}

	const rounds = 100
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for k := 1; k <= rounds; k++ {
			assert.NoError(t, srv.UpdateConfig([]server.Config{
				patchConfig(t, "ns1/gw1", fmt.Sprintf("gen-%03d", k))}))
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Microsecond)
		}
	}()
	go func() {
		defer wg.Done()
		for j := 1; j <= rounds; j++ {
			nodes.set(fmt.Sprintf("node%d", 1+j%3), fmt.Sprintf("10.0.%d.%d", 1+j%3, j))
			srv.Refresh()
			time.Sleep(time.Duration(rand.Intn(1000)) * time.Microsecond)
		}
	}()
	wg.Wait()

	final := map[string]string{"": "9.9.9.9"}
	for _, n := range []string{"node1", "node2", "node3"} {
		final[n] = nodes.nodeAddr(n)
	}
	for i, w := range watchers {
		assert.Eventually(t, func() bool {
			w.mu.Lock()
			defer w.mu.Unlock()
			return w.last != nil && w.last.Auth.Realm == fmt.Sprintf("gen-%03d", rounds) &&
				slices.Equal(w.last.Clusters[0].Addrs, []string{final[w.node]})
		}, 5*time.Second, 20*time.Millisecond, "watcher %d on node %q ends up with the last config", i, w.node)
		w.mu.Lock()
		assert.True(t, slices.IsSorted(w.gens), "watcher %d on node %q never goes back: %v", i, w.node, w.gens)
		w.mu.Unlock()
	}
}

// TestCDSReconnect pins that a client that was away while its config and its node changed gets
// the last config, specialized for the last address of its node, when it reconnects.
func TestCDSReconnect(t *testing.T) {
	nodes := newNodeTable(map[string]string{"node1": "1.1.1.1"})
	srv, addr := startPatchServer(t, nodes)
	require.NoError(t, srv.UpdateConfig([]server.Config{patchConfig(t, "ns1/gw1", "realm1")}))

	ch := watchClient(t, addr, "ns1/gw1", nodeLabels("node1"))
	c := watchConfig(ch, time.Second)
	assert.Equal(t, []string{"1.1.1.1"}, relayAddrs(t, c), "initial")

	conns := srv.GetConnTrack().Snapshot()
	require.Len(t, conns, 1)
	srv.RemoveClient(conns[0].Id())

	nodes.set("node1", "1.1.1.2")
	srv.Refresh()
	require.NoError(t, srv.UpdateConfig([]server.Config{patchConfig(t, "ns1/gw1", "realm2")}))

	assert.Eventually(t, func() bool {
		for {
			select {
			case c = <-ch:
				if c.Auth.Realm == "realm2" && slices.Equal(c.Clusters[0].Addrs, []string{"1.1.1.2"}) {
					return true
				}
			default:
				return false
			}
		}
	}, 5*time.Second, 50*time.Millisecond, "the reconnected client gets the last config")
}

// zeroConfig is the zero config the server stores and serves.
func zeroConfig(id, realm string) *stnrv2.StunnerConfig {
	return &stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin:      stnrv2.AdminConfig{Name: id},
		Auth: stnrv2.AuthConfig{
			Type:        "static",
			Realm:       realm,
			Credentials: map[string]string{"username": "dummy-username", "password": "dummy-password"},
		},
		Listeners: []stnrv2.ListenerConfig{},
		Servers:   []stnrv2.ServerConfig{},
		Clusters:  []stnrv2.ClusterConfig{},
	}
}

// only differ in id and realm
func testConfig(id, realm string) server.Config {
	c := zeroConfig(id, realm)
	namespace, name, _ := server.NamespacedName(id)
	_ = c.Validate() // make sure deepeq works
	return server.Config{Namespace: namespace, Name: name, Config: c}
}

// with 2 listeners
func testConfigListener(id, realm string) server.Config {
	c := zeroConfig(id, realm)
	c.Listeners = []stnrv2.ListenerConfig{
		{Name: "l1", Protocol: "TCP", Port: 3478, Servers: []string{"turn"}},
		{Name: "l2", Protocol: "UDP", Port: 3479, Servers: []string{"turn"}},
	}
	c.Servers = []stnrv2.ServerConfig{{Name: "turn", Type: "turn", Clusters: []string{"c1"}}}
	c.Clusters = []stnrv2.ClusterConfig{{Name: "c1", Endpoints: []string{"10.0.0.0/8"},
		Protocol: "UDP", Addrs: []string{"1.1.1.1"}}}
	_ = c.Validate() // make sure deepeq works
	namespace, name, _ := server.NamespacedName(id)
	return server.Config{Namespace: namespace, Name: name, Config: c}
}

// wait for some configurable time for a watch element
func watchConfig(ch chan *stnrv2.StunnerConfig, d time.Duration) *stnrv2.StunnerConfig {
	select {
	case c := <-ch:
		// fmt.Println("++++++++++++ got config ++++++++++++: ", c.String())
		return c
	case <-time.After(d):
		// fmt.Println("++++++++++++ timeout ++++++++++++")
		return nil
	}
}

func findConfById(cs []*stnrv2.StunnerConfig, id string) *stnrv2.StunnerConfig {
	for i := range cs {
		if cs[i] != nil && cs[i].Admin.Name == id {
			return cs[i]
		}
	}

	return nil
}

// clusterAddrs returns the relay addresses of a cluster of a config.
func clusterAddrs(t *testing.T, c *stnrv2.StunnerConfig, name string) []string {
	t.Helper()
	d, err := c.GetClusterConfig(name)
	assert.NoError(t, err, "cluster %s", name)
	return d.Addrs
}
