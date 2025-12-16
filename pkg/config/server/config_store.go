package server

import (
	"fmt"
	"strings"
	"sync"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Number if config updates that can be on-hold.
const SubscribeChannelBufferSize = 256

// FilterFunc allows clients to filter configs.
type FilterFunc func(config *Config) bool

// PatchFunc is a callback to patch config updates for a client.
type PatchFunc func(conf *stnrv1.StunnerConfig) *stnrv1.StunnerConfig

type Subscription[T comparable] struct {
	topic   string
	ch      chan *Config
	filter  ClientFilter[T]
	patcher PatchFunc
}

type ConfigStore[T comparable] struct {
	configs       map[string]map[string]*Config // namespace -> name -> config
	subscriptions []Subscription[T]
	mu            sync.RWMutex
}

func NewConfigStore[T comparable]() *ConfigStore[T] {
	return &ConfigStore[T]{
		configs:       make(map[string]map[string]*Config),
		subscriptions: []Subscription[T]{},
	}
}

// Snapshot copies out all configs.
func (cs *ConfigStore[_]) Snapshot() []*Config {
	configs := []*Config{}

	cs.mu.RLock()
	defer cs.mu.RUnlock()
	for _, namespaceConfigs := range cs.configs {
		for _, config := range namespaceConfigs {
			configs = append(configs, config.DeepCopy())
		}
	}

	return configs
}

// Upsert sets or updates a config and notifies subscribers.
func (cs *ConfigStore[T]) Upsert(namespace, name string, stnrConfig *stnrv1.StunnerConfig) {
	// Suppress Upsert if there is no change
	if c, ok := cs.Get(namespace, name); ok {
		if c.Config.DeepEqual(stnrConfig) {
			return
		}
	}

	// Update the config
	config := &Config{
		Namespace: namespace,
		Name:      name,
		Config:    stnrConfig,
	}

	cs.mu.Lock()
	if _, exists := cs.configs[namespace]; !exists {
		cs.configs[namespace] = make(map[string]*Config)
	}
	cs.configs[namespace][name] = config

	// Get a copy of subscriptions to avoid holding the lock during notification
	subs := make([]Subscription[T], len(cs.subscriptions))
	copy(subs, cs.subscriptions)
	cs.mu.Unlock()

	// Notify subscribers
	for _, sub := range subs {
		// Check if the config matches the topic
		if matchesTopic(config, sub.topic) {
			cs.sendConfig(sub.ch, config, sub.patcher)
		}
	}
}

// Delete removes the config from the store and optionally sends the supplied config (usually a
// zero-config) to affected clients.
func (cs *ConfigStore[T]) Delete(namespace, name string, stnrConfig *stnrv1.StunnerConfig) {
	// Update the config
	cs.mu.Lock()
	if configs, exists := cs.configs[namespace]; exists {
		delete(configs, name)
		if len(cs.configs[namespace]) == 0 {
			delete(cs.configs, namespace)
		}
	}
	cs.mu.Unlock()

	if stnrConfig == nil {
		return
	}

	// Notify subscribers
	config := &Config{
		Namespace: namespace,
		Name:      name,
		Config:    stnrConfig,
	}

	// Get a copy of subscriptions to avoid holding the lock during notification
	cs.mu.Lock()
	subs := make([]Subscription[T], len(cs.subscriptions))
	copy(subs, cs.subscriptions)
	cs.mu.Unlock()

	// Notify subscribers
	for _, sub := range subs {
		// Check if the config matches the topic
		if matchesTopic(config, sub.topic) {
			sub.ch <- config
		}
	}
}

// Get retrieves a config
func (cs *ConfigStore[_]) Get(namespace, name string) (*Config, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	namespaceMap, exists := cs.configs[namespace]
	if !exists {
		return nil, false
	}

	config, exists := namespaceMap[name]
	if exists {
		config = config.DeepCopy()
	}

	return config, exists
}

// Push will re-broadcast configs to interested clients.
func (cs *ConfigStore[T]) Push(arg T) {
	configs := cs.Snapshot()

	cs.mu.Lock()
	defer cs.mu.Unlock()

	for _, sub := range cs.subscriptions {
		if sub.filter != nil && sub.filter(arg) {
			for _, config := range configs {
				if matchesTopic(config, sub.topic) {
					cs.sendConfig(sub.ch, config, sub.patcher)
				}
			}
		}
	}
}

// SubscribeAll subscribes to all config changes
func (cs *ConfigStore[T]) SubscribeAll(filter ClientFilter[T], patcher PatchFunc) chan *Config {
	return cs.subscribeToTopic("all", filter, patcher)
}

// SubscribeNamespace subscribes to all config changes in a specific namespace
func (cs *ConfigStore[T]) SubscribeNamespace(namespace string, filter ClientFilter[T], patcher PatchFunc) chan *Config {
	return cs.subscribeToTopic(fmt.Sprintf("%s/*", namespace), filter, patcher)
}

// SubscribeConfig subscribes to changes for a specific config
func (cs *ConfigStore[T]) SubscribeConfig(namespace, name string, filter ClientFilter[T], patcher PatchFunc) chan *Config {
	return cs.subscribeToTopic(fmt.Sprintf("%s/%s", namespace, name), filter, patcher)
}

// Unsubscribe removes a subscription
func (cs *ConfigStore[_]) Unsubscribe(ch chan *Config) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i, sub := range cs.subscriptions {
		if sub.ch == ch {
			// Remove by swapping with the last element and truncating
			cs.subscriptions[i] = cs.subscriptions[len(cs.subscriptions)-1]
			cs.subscriptions = cs.subscriptions[:len(cs.subscriptions)-1]
			close(ch)
			return
		}
	}
}

// Unsubscribe removes a subscription
func (cs *ConfigStore[T]) UnsubscribeAll() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for _, sub := range cs.subscriptions {
		close(sub.ch)
	}

	cs.subscriptions = []Subscription[T]{}
}

// matchesTopic checks if a config matches a topic pattern
func matchesTopic(config *Config, topic string) bool {
	switch {
	case topic == "all":
		return true

	case strings.HasSuffix(topic, "/*"):
		// Namespace pattern (e.g., "database/*")
		namespace := topic[:len(topic)-2]
		return config.Namespace == namespace

	default:
		// Specific config (e.g., "database/url")
		parts := strings.SplitN(topic, "/", 2)
		if len(parts) == 2 {
			return config.Namespace == parts[0] && config.Name == parts[1]
		}
		return false
	}
}

// subscribeToTopic creates a subscription to a topic with an optional filter
func (cs *ConfigStore[T]) subscribeToTopic(topic string, filter ClientFilter[T], patcher PatchFunc) chan *Config {
	ch := make(chan *Config, SubscribeChannelBufferSize)

	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Register the subscription
	cs.subscriptions = append(cs.subscriptions, Subscription[T]{
		topic:   topic,
		ch:      ch,
		filter:  filter,
		patcher: patcher,
	})

	// Make a copy of configs that match the topic to send initial values
	for _, namespaceConfigs := range cs.configs {
		for _, config := range namespaceConfigs {
			if matchesTopic(config, topic) {
				cs.sendConfig(ch, config, patcher)
			}
		}
	}

	return ch
}

// sendConfig patcher and sends a config to a channel.
func (cs *ConfigStore[T]) sendConfig(ch chan *Config, config *Config, patcher PatchFunc) {
	c := config.DeepCopy()
	if patcher != nil {
		c.Config = patcher(c.Config)
	}
	ch <- c
}
