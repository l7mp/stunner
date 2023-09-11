package stunner

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/pion/transport/test"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/yaml"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
	cdsclient "github.com/l7mp/stunner/pkg/config/client"
	"github.com/l7mp/stunner/pkg/logger"
)

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
		"udp://user1:passwd1@1.2.3.4:3478?transport=udp",
		"udp://user1:passwd1@1.2.3.4:3478",
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
				LogLevel:         stunnerTestLoglevel,
				SuppressRollback: true,
				Net:              v.podnet,
			})

			log.Debug("starting stunnerd")
			assert.NoError(t, stunner.Reconcile(*c), "starting server")

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

	checkDefaultConfig(t, c, "UDP")

	file, err2 := yaml.Marshal(c)
	assert.NoError(t, err2, "marschal config fike")

	newConf := &v1alpha1.StunnerConfig{}
	err = yaml.Unmarshal(file, newConf)
	assert.NoError(t, err, "unmarschal config from file")

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
	// we just need the filename for now so we remove the fle first
	file := f.Name()
	assert.NoError(t, os.Remove(file), "removing temp config file")

	log.Debug("creating a stunnerd")
	stunner := NewStunner(Options{LogLevel: stunnerTestLoglevel})

	log.Debug("starting watcher")
	conf := make(chan v1alpha1.StunnerConfig, 1)
	defer close(conf)

	log.Debug("init watcher with nonexistent config file")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = stunner.WatchConfig(ctx, file, conf)
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
	defer os.Remove(file)

	y, err := yaml.Marshal(c)
	assert.NoError(t, err, "marshal config file")
	err = f.Truncate(0)
	assert.NoError(t, err, "truncate temp file")
	_, err = f.Seek(0, 0)
	assert.NoError(t, err, "seek temp file")
	_, err = f.Write(y)
	assert.NoError(t, err, "write config to temp file")

	// wait a bit so that the watcher has time to react
	time.Sleep(50 * time.Millisecond)

	// first read should yield a zeroconfig
	c2, ok := <-conf
	assert.True(t, ok, "zeroconfig emitted")
	checkZeroConfig(t, &c2, stunner.GetId())

	// second read yields the real config
	c2, ok = <-conf
	assert.True(t, ok, "config emitted")
	checkDefaultConfig(t, &c2, "UDP")

	log.Debug("write a wrong config file (WatchConfig does not validate)")
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

	// wait a bit so that the watcher has time to react
	time.Sleep(50 * time.Millisecond)

	c3 := <-conf
	checkDefaultConfig(t, &c3, "dummy")

	log.Debug("update the config file and check")
	c3.Listeners[0].Protocol = "TCP"
	y, err = yaml.Marshal(c3)
	assert.NoError(t, err, "marshal config file")
	err = f.Truncate(0)
	assert.NoError(t, err, "truncate temp file")
	_, err = f.Seek(0, 0)
	assert.NoError(t, err, "seek temp file")
	_, err = f.Write(y)
	assert.NoError(t, err, "write config to temp file")

	// wait a bit so that the watcher has time to react
	time.Sleep(50 * time.Millisecond)

	// read back result
	c4 := <-conf
	checkDefaultConfig(t, &c4, "TCP")

	stunner.Close()
}

func checkDefaultConfig(t *testing.T, c *v1alpha1.StunnerConfig, proto string) {
	assert.Equal(t, "plaintext", c.Auth.Type, "auth-type")
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

func checkZeroConfig(t *testing.T, c *v1alpha1.StunnerConfig, id string) {
	assert.True(t, c.DeepEqual(cdsclient.ZeroConfig(id)), "zeroconfig ok")
}
