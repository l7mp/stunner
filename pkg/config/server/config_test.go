package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/config/client"
)

func TestConfigStore_Upsert(t *testing.T) {
	store := NewConfigStore[string]()

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
	store := NewConfigStore[string]()

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
	store := NewConfigStore[string]()

	// Create some sample configs
	config1 := testConfig("config1")
	config2 := testConfig("config2")

	// Add configs before subscription
	store.Upsert("ns1", "cfg1", config1)
	store.Upsert("ns2", "cfg2", config2)

	// Subscribe to all
	ch := store.SubscribeAll(nil, nil)

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
	store := NewConfigStore[string]()

	// Create some sample configs in different namespaces
	config1 := testConfig("config1")
	config2 := testConfig("config2")
	config3 := testConfig("config3")
	store.Upsert("ns1", "cfg1", config1)
	store.Upsert("ns1", "cfg2", config2)
	store.Upsert("ns2", "cfg3", config3)

	// Subscribe to ns1
	ch := store.SubscribeNamespace("ns1", nil, nil)

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
	store := NewConfigStore[string]()

	// Create some configs
	config1 := testConfig("config1")
	store.Upsert("ns1", "cfg1", config1)

	// Subscribe to specific config
	ch := store.SubscribeConfig("ns1", "cfg1", nil, nil)

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

func TestConfigStore_Patcher(t *testing.T) {
	store := NewConfigStore[string]()

	// Create a sample config
	originalConfig := testConfig("config1")
	store.Upsert("ns1", "cfg1", originalConfig)

	// Create a patcher function
	patcher := func(conf *stnrv1.StunnerConfig) *stnrv1.StunnerConfig {
		conf.Auth.Realm = "patched-" + conf.Auth.Realm
		return conf
	}

	// Subscribe with the patcher
	ch := store.SubscribeConfig("ns1", "cfg1", nil, patcher)

	// Check the initial config is patched
	configs := collectConfigs(ch, 1, 100*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive 1 initial config")
	assert.Equal(t, "patched-config1", configs[0].Config.Auth.Realm, "Config should be patched")

	// Update the config
	updatedConfig := testConfig("updated-config1")
	store.Upsert("ns1", "cfg1", updatedConfig)

	// Check the update is also patched
	configs = collectConfigs(ch, 1, 100*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive 1 update")
	assert.Equal(t, "patched-updated-config1", configs[0].Config.Auth.Realm, "Updated config should be patched")

	// Clean up
	store.Unsubscribe(ch)
}

func TestConfigStore_Push(t *testing.T) {
	store := NewConfigStore[string]()

	// Create a config
	config1 := testConfig("config1")
	store.Upsert("ns1", "cfg1", config1)

	// create the filter
	filter1 := func(node string) bool { return node == "node1" }
	filter3 := func(node string) bool { return node == "node3" }

	// Subscribe to specific config
	ch1 := store.SubscribeConfig("ns1", "cfg1", filter1, nil)
	ch2 := store.SubscribeConfig("ns1", "cfg1", filter1, nil)
	ch3 := store.SubscribeConfig("ns1", "cfg2", filter3, nil)

	// Check initial config
	configs := collectConfigs(ch1, 1, 100*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive 1 initial config")
	assert.Equal(t, "ns1", configs[0].Namespace)
	assert.Equal(t, "cfg1", configs[0].Name)

	configs = collectConfigs(ch2, 1, 100*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive 1 initial config")
	assert.Equal(t, "ns1", configs[0].Namespace)
	assert.Equal(t, "cfg1", configs[0].Name)

	configs = collectConfigs(ch3, 1, 10*time.Millisecond)
	assert.Len(t, configs, 0, "Should receive no initial config")

	// push node update to node1 listeners (ch1 and ch2)
	store.Push("node1")

	// first two clients should get an update
	configs = collectConfigs(ch1, 1, 100*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive 1 config")
	assert.Equal(t, "ns1", configs[0].Namespace)
	assert.Equal(t, "cfg1", configs[0].Name)

	configs = collectConfigs(ch2, 1, 100*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive 1 config")
	assert.Equal(t, "ns1", configs[0].Namespace)
	assert.Equal(t, "cfg1", configs[0].Name)

	configs = collectConfigs(ch3, 1, 10*time.Millisecond)
	assert.Len(t, configs, 0, "Should receive no initial config")

	// push node update to node3 -> goes to client3, no config yet
	store.Push("node3")

	configs = collectConfigs(ch1, 1, 10*time.Millisecond)
	assert.Len(t, configs, 0, "Should receive 1 config")
	configs = collectConfigs(ch2, 1, 10*time.Millisecond)
	assert.Len(t, configs, 0, "Should receive 1 config")
	configs = collectConfigs(ch3, 1, 10*time.Millisecond)
	assert.Len(t, configs, 0, "Should receive no initial config")

	// Update the subscribed config
	config2 := testConfig("config3")
	store.Upsert("ns1", "cfg2", config2)

	// client3 should get an initial config
	configs = collectConfigs(ch1, 1, 10*time.Millisecond)
	assert.Len(t, configs, 0, "Should receive no config")
	configs = collectConfigs(ch2, 1, 10*time.Millisecond)
	assert.Len(t, configs, 0, "Should receive no config")
	configs = collectConfigs(ch3, 1, 10*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive 1 initial config")
	assert.Equal(t, "ns1", configs[0].Namespace)
	assert.Equal(t, "cfg2", configs[0].Name)

	// push node update to node3 -> goes to client3
	store.Push("node3")

	configs = collectConfigs(ch1, 1, 10*time.Millisecond)
	assert.Len(t, configs, 0, "Should receive 1 config")
	configs = collectConfigs(ch2, 1, 10*time.Millisecond)
	assert.Len(t, configs, 0, "Should receive 1 config")
	configs = collectConfigs(ch3, 1, 100*time.Millisecond)
	assert.Len(t, configs, 1, "Should receive no initial config")
	assert.Equal(t, "ns1", configs[0].Namespace)
	assert.Equal(t, "cfg2", configs[0].Name)

	// Clean up
	store.Unsubscribe(ch1)
	store.Unsubscribe(ch2)
	store.Unsubscribe(ch3)
}

func TestConfigStore_Unsubscribe(t *testing.T) {
	store := NewConfigStore[string]()

	// Create a config
	config1 := testConfig("config1")
	store.Upsert("ns1", "cfg1", config1)

	// Subscribe
	ch := store.SubscribeAll(nil, nil)

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
	store := NewConfigStore[string]()

	// Create a config
	config1 := testConfig("config1")
	store.Upsert("ns1", "cfg1", config1)

	// Create multiple subscribers
	ch1 := store.SubscribeAll(nil, nil)
	ch2 := store.SubscribeNamespace("ns1", nil, nil)
	ch3 := store.SubscribeConfig("ns1", "cfg1", nil, nil)

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

func testConfig(realm string) *stnrv1.StunnerConfig {
	c := client.ZeroConfig("dummy/dummy")
	c.Auth.Realm = realm
	_ = c.Validate() // make sure deepeq works
	return c
}
