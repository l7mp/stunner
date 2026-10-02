package server

import (
	"sync"
	"testing"
	"time"

	"github.com/go-openapi/testify/v2/require"
	"github.com/stretchr/testify/assert"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func TestConfigStore_Upsert(t *testing.T) {
	store := NewConfigStore(nil)

	// Create a sample config
	stnrConfig := testConfig("config1")

	// Upsert the config
	store.Upsert("default", "config1", stnrConfig)

	// Verify the config was stored correctly
	config, exists := store.Get("default", "config1")
	assert.True(t, exists, "Config should exist")
	assert.Equal(t, "default", config.Namespace, "Namespace should match")
	assert.Equal(t, "config1", config.Name, "Name should match")
	assert.Equal(t, stnrConfig, config.Config, "Config should match")

	// Update the same config
	updatedConfig := testConfig("config1")
	store.Upsert("default", "config1", updatedConfig)

	// Verify the update
	config, exists = store.Get("default", "config1")
	assert.True(t, exists, "Config should still exist")
	assert.Equal(t, updatedConfig, config.Config, "Config should be updated")
}

func TestConfigStore_Get(t *testing.T) {
	store := NewConfigStore(nil)

	// Try to get a non-existent config
	config, exists := store.Get("nonexistent", "config")
	assert.False(t, exists, "Config should not exist")
	assert.Nil(t, config, "Config should be nil")

	// Add a config
	stnrConfig := testConfig("config1")
	store.Upsert("default", "config1", stnrConfig)

	// Get the existing config
	config, exists = store.Get("default", "config1")
	assert.True(t, exists, "Config should exist")
	assert.NotNil(t, config, "Config should not be nil")

	// Try to get a config from an existing namespace but with wrong name
	config, exists = store.Get("default", "nonexistent")
	assert.False(t, exists, "Config should not exist")
	assert.Nil(t, config, "Config should be nil")
}

func TestConfigStore_SubscribeAll(t *testing.T) {
	store := NewConfigStore(nil)

	// Create some sample configs
	config1 := testConfig("config1")
	config2 := testConfig("config2")

	// Add configs before subscription
	store.Upsert("ns1", "cfg1", config1)
	store.Upsert("ns2", "cfg2", config2)

	// Subscribe to all
	ch := store.SubscribeAll()

	// Check initial configs (should receive both)
	configs := collectConfigs(ch, 2, 100*time.Millisecond)
	assert.Len(t, configs, 2, "Should receive 2 initial configs")

	// Add another config after subscription
	config3 := testConfig("config3")
	store.Upsert("ns3", "cfg3", config3)

	// Check we receive the new config
	configs = collectConfigs(ch, 1, 100*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive the new config")
	assert.Equal(t, "ns3", configs[0].Namespace)
	assert.Equal(t, "cfg3", configs[0].Name)

	// Clean up
	store.Unsubscribe(ch)
}

func TestConfigStore_SubscribeNamespace(t *testing.T) {
	store := NewConfigStore(nil)

	// Create some sample configs in different namespaces
	config1 := testConfig("config1")
	config2 := testConfig("config2")
	config3 := testConfig("config3")
	store.Upsert("ns1", "cfg1", config1)
	store.Upsert("ns1", "cfg2", config2)
	store.Upsert("ns2", "cfg3", config3)

	// Subscribe to ns1
	ch := store.SubscribeNamespace("ns1")

	// Check initial configs (should receive 2 from ns1)
	configs := collectConfigs(ch, 2, 100*time.Millisecond)
	assert.Len(t, configs, 2, "Should receive 2 initial configs from ns1")
	for _, cfg := range configs {
		assert.Equal(t, "ns1", cfg.Namespace, "Should only receive configs from ns1")
	}

	// Add another config to ns1 and a different namespace
	config4 := testConfig("config4")
	config5 := testConfig("config5")
	store.Upsert("ns1", "cfg4", config4)
	store.Upsert("ns2", "cfg5", config5)

	// Check we receive only the ns1 update
	configs = collectConfigs(ch, 1, 500*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive only the new ns1 config")
	assert.Equal(t, "ns1", configs[0].Namespace)
	assert.Equal(t, "cfg4", configs[0].Name)

	// Clean up
	store.Unsubscribe(ch)
}

func TestConfigStore_SubscribeConfig(t *testing.T) {
	store := NewConfigStore(nil)

	// Create some configs
	config1 := testConfig("config1")
	store.Upsert("ns1", "cfg1", config1)

	// Subscribe to specific config
	ch := store.SubscribeConfig("ns1", "cfg1", nil)

	// Check initial config
	configs := collectConfigs(ch, 1, 100*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive 1 initial config")
	assert.Equal(t, "ns1", configs[0].Namespace)
	assert.Equal(t, "cfg1", configs[0].Name)

	// Update the subscribed config
	updatedConfig := testConfig("updated-config1")
	store.Upsert("ns1", "cfg1", updatedConfig)

	// Add an unrelated config
	config2 := testConfig("config2")
	store.Upsert("ns1", "cfg2", config2)

	// Check we receive only the update to the subscribed config
	configs = collectConfigs(ch, 1, 500*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive only the update to subscribed config")
	assert.Equal(t, "updated-config1", configs[0].Config.Auth.Realm)

	// Clean up
	store.Unsubscribe(ch)
}

// testRealms is a per-node realm table the tests change under the store.
type testRealms struct{ sync.Map } // node -> realm

// patch sets the realm of the client's node, if the table has one.
func (r *testRealms) patch(conf *stnrv2.StunnerConfig, _ string, labels map[string]string) *stnrv2.StunnerConfig {
	if realm, ok := r.Load(labels["node"]); ok {
		conf.Auth.Realm = realm.(string)
	}
	return conf
}

func (r *testRealms) set(node, realm string) {
	if realm == "" {
		r.Delete(node)
		return
	}
	r.Store(node, realm)
}

// TestConfigStore_Patcher pins what a subscriber to a config gets: the config patched with the
// config id and the subscriber's labels, as the patcher wants it, a whole sub-object included,
// while the stored config stays as it is and a namespace subscriber gets it unpatched.
func TestConfigStore_Patcher(t *testing.T) {
	var gotID string
	var gotLabels map[string]string
	store := NewConfigStore(func(conf *stnrv2.StunnerConfig, id string, labels map[string]string) *stnrv2.StunnerConfig {
		gotID, gotLabels = id, labels
		conf.Auth = stnrv2.AuthConfig{Type: "static", Realm: "realm-" + labels["tenant"],
			Credentials: map[string]string{"username": "u-" + labels["tenant"], "password": "p"}}
		return conf
	})
	store.Upsert("ns1", "cfg1", testConfig("config1"))

	ch := store.SubscribeConfig("ns1", "cfg1", map[string]string{"tenant": "a"})
	got := collectConfigs(ch, 1, 100*time.Millisecond)
	require.Len(t, got, 1, "initial config")
	assert.Equal(t, "ns1/cfg1", gotID, "the patcher gets the config id")
	assert.Equal(t, map[string]string{"tenant": "a"}, gotLabels, "and the client's labels")
	assert.Equal(t, "realm-a", got[0].Config.Auth.Realm, "a whole sub-object rewritten")
	assert.Equal(t, "u-a", got[0].Config.Auth.Credentials["username"])

	cfg, ok := store.Get("ns1", "cfg1")
	require.True(t, ok)
	assert.Equal(t, "config1", cfg.Config.Auth.Realm, "the stored config is untouched")
	assert.Equal(t, "realm-b", store.Patch(cfg, map[string]string{"tenant": "b"}).Config.Auth.Realm,
		"a single read is patched the same way")

	nch := store.SubscribeNamespace("ns1")
	got = collectConfigs(nch, 1, 100*time.Millisecond)
	require.Len(t, got, 1, "initial config")
	assert.Equal(t, "config1", got[0].Config.Auth.Realm, "a namespace subscriber gets it unpatched")

	store.Unsubscribe(ch)
	store.Unsubscribe(nch)
}

// TestConfigStore_Refresh pins that every subscriber gets each config it would get exactly once:
// a refresh pushes only the subscribers whose patched config changed, an unchanged upsert pushes
// nobody, a changed one everybody.
func TestConfigStore_Refresh(t *testing.T) {
	realms := &testRealms{}
	realms.set("node1", "realm-1.1.1.1")
	realms.set("node2", "realm-2.2.2.2")
	store := NewConfigStore(realms.patch)
	store.Upsert("ns1", "cfg1", testConfig("realm-default"))

	ch1 := store.SubscribeConfig("ns1", "cfg1", map[string]string{"node": "node1"})
	ch2 := store.SubscribeConfig("ns1", "cfg1", map[string]string{"node": "node2"})
	require.Len(t, collectConfigs(ch1, 1, 100*time.Millisecond), 1, "initial config 1")
	require.Len(t, collectConfigs(ch2, 1, 100*time.Millisecond), 1, "initial config 2")

	store.Refresh()
	assert.Empty(t, collectConfigs(ch1, 1, 50*time.Millisecond), "nothing changed")
	assert.Empty(t, collectConfigs(ch2, 1, 50*time.Millisecond), "nothing changed")

	realms.set("node1", "realm-1.1.1.9")
	store.Refresh()
	got := collectConfigs(ch1, 1, 100*time.Millisecond)
	require.Len(t, got, 1, "the node whose patch changed")
	assert.Equal(t, "realm-1.1.1.9", got[0].Config.Auth.Realm)
	assert.Empty(t, collectConfigs(ch2, 1, 50*time.Millisecond), "the other node")

	realms.set("node1", "")
	store.Refresh()
	got = collectConfigs(ch1, 1, 100*time.Millisecond)
	require.Len(t, got, 1, "a patch that is gone")
	assert.Equal(t, "realm-default", got[0].Config.Auth.Realm, "the config as it is")

	store.Upsert("ns1", "cfg1", testConfig("realm-default"))
	assert.Empty(t, collectConfigs(ch1, 1, 50*time.Millisecond), "an unchanged upsert")
	assert.Empty(t, collectConfigs(ch2, 1, 50*time.Millisecond), "an unchanged upsert")

	c := testConfig("realm-default")
	c.Admin.LogLevel = "all:DEBUG"
	store.Upsert("ns1", "cfg1", c)
	got = collectConfigs(ch1, 1, 100*time.Millisecond)
	require.Len(t, got, 1, "a changed upsert")
	assert.Equal(t, "all:DEBUG", got[0].Config.Admin.LogLevel)
	got = collectConfigs(ch2, 1, 100*time.Millisecond)
	require.Len(t, got, 1, "a changed upsert")
	assert.Equal(t, "realm-2.2.2.2", got[0].Config.Auth.Realm, "patched")

	store.Delete("ns1", "cfg1", testConfig("deleted"))
	assert.Len(t, collectConfigs(ch1, 1, 100*time.Millisecond), 1, "a deletion")
	assert.Len(t, collectConfigs(ch2, 1, 100*time.Millisecond), 1, "a deletion")
	store.Refresh()
	assert.Empty(t, collectConfigs(ch1, 1, 50*time.Millisecond), "a deleted config")

	store.Unsubscribe(ch1)
	store.Unsubscribe(ch2)
}

func TestConfigStore_Unsubscribe(t *testing.T) {
	store := NewConfigStore(nil)

	// Create a config
	config1 := testConfig("config1")
	store.Upsert("ns1", "cfg1", config1)

	// Subscribe
	ch := store.SubscribeAll()

	// Check we get the initial config
	configs := collectConfigs(ch, 1, 500*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive initial config")

	// Unsubscribe
	store.Unsubscribe(ch)

	// Add a new config
	config2 := testConfig("config2")
	store.Upsert("ns1", "cfg2", config2)

	// Try to read from the channel (should be closed)
	select {
	case _, open := <-ch:
		assert.False(t, open, "Channel should be closed after unsubscribe")
	default:
		t.Fatal("Channel should be closed, not blocked")
	}
}

func TestConfigStore_MultipleSubscribers(t *testing.T) {
	store := NewConfigStore(nil)

	// Create a config
	config1 := testConfig("config1")
	store.Upsert("ns1", "cfg1", config1)

	// Create multiple subscribers
	ch1 := store.SubscribeAll()
	ch2 := store.SubscribeNamespace("ns1")
	ch3 := store.SubscribeConfig("ns1", "cfg1", nil)

	// Check all subscribers get the initial config
	for _, ch := range []chan *Config{ch1, ch2, ch3} {
		configs := collectConfigs(ch, 1, 100*time.Millisecond)
		assert.Len(t, configs, 1, "Should receive initial config")
		assert.Equal(t, "config1", configs[0].Config.Auth.Realm)
	}

	// Update the config
	updatedConfig := testConfig("updated-config1")
	store.Upsert("ns1", "cfg1", updatedConfig)

	// Check all subscribers get the update
	for _, ch := range []chan *Config{ch1, ch2, ch3} {
		configs := collectConfigs(ch, 1, 500*time.Millisecond)
		assert.Len(t, configs, 1, "Should receive updated config")
		assert.Equal(t, "updated-config1", configs[0].Config.Auth.Realm)
	}

	// Clean up
	store.Unsubscribe(ch1)
	store.Unsubscribe(ch2)
	store.Unsubscribe(ch3)
}

func TestConfigStore_DeepCopy(t *testing.T) {
	// Test the DeepCopy functionality
	original := &Config{
		Namespace: "ns1",
		Name:      "cfg1",
		Config:    testConfig("config1"),
	}

	copy := original.DeepCopy()

	// Verify it's a deep copy
	assert.Equal(t, original.Namespace, copy.Namespace)
	assert.Equal(t, original.Name, copy.Name)
	assert.Equal(t, original.Config.Auth.Realm, copy.Config.Auth.Realm)

	// Modify the copy and check that original is unchanged
	copy.Config.Auth.Realm = "modified"
	assert.NotEqual(t, original.Config.Auth.Realm, copy.Config.Auth.Realm)
	assert.Equal(t, "config1", original.Config.Auth.Realm)
}

func TestConfigStore_DeepEqual(t *testing.T) {
	// Test the DeepEqual functionality
	config1 := &Config{
		Namespace: "ns1",
		Name:      "cfg1",
		Config:    testConfig("config1"),
	}

	// Same values
	config2 := &Config{
		Namespace: "ns1",
		Name:      "cfg1",
		Config:    testConfig("config1"),
	}

	// Different values
	config3 := &Config{
		Namespace: "ns1",
		Name:      "different",
		Config:    testConfig("config1"),
	}

	assert.True(t, config1.DeepEqual(config2), "Identical configs should be equal")
	assert.False(t, config1.DeepEqual(config3), "Different configs should not be equal")
}

// Helper function to collect configs from a channel
func collectConfigs(ch chan *Config, count int, timeout time.Duration) []*Config {
	configs := make([]*Config, 0, count)
	timeoutCh := time.After(timeout)

	for i := 0; i < count; i++ {
		select {
		case config, ok := <-ch:
			if !ok {
				return configs // Channel closed
			}
			if err := config.Config.Validate(); err != nil {
				// make sure deepeq works
				return []*Config{}
			}
			configs = append(configs, config)
		case <-timeoutCh:
			return configs // Timeout
		}
	}

	return configs
}

func testConfig(realm string) *stnrv2.StunnerConfig {
	c := &stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin:      stnrv2.AdminConfig{Name: "dummy/dummy"},
		Auth: stnrv2.AuthConfig{
			Type:        "static",
			Realm:       realm,
			Credentials: map[string]string{"username": "dummy-username", "password": "dummy-password"},
		},
		Listeners: []stnrv2.ListenerConfig{},
		Clusters:  []stnrv2.ClusterConfig{},
	}
	_ = c.Validate() // make sure deepeq works
	return c
}
