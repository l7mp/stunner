package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// SubscribeChannelBufferSize is the number of config updates a subscription can hold.
const SubscribeChannelBufferSize = 256

// Patcher rewrites the config the server serves a client, given the config id the client watches
// and the labels it describes itself with; the server interprets neither the labels nor the patch.
// It gets a copy of its own and may modify it.
type Patcher func(conf *stnrv2.StunnerConfig, id string, labels map[string]string) *stnrv2.StunnerConfig

type subscription struct {
	topic string
	// patch tells whether the subscriber gets its configs patched, for the labels it has
	patch  bool
	labels map[string]string
	ch     chan *Config
	done   chan struct{}
	// sent is the JSON of the config last sent per config id: what the client has
	sent map[string][]byte
}

// ConfigStore holds the configs and pushes every subscriber the config it would get, patched for
// the subscriber, whenever that differs from what the subscriber was last sent.
type ConfigStore struct {
	configs       sync.Map // config id -> *Config
	subscriptions sync.Map // channel -> *subscription
	patcher       Patcher
	// mu orders the pushes, so a subscriber never gets a config older than one it already has,
	// and guards what each subscription was sent
	mu sync.Mutex
}

// NewConfigStore creates a config store that patches the configs of its subscribers, nil for no
// patching.
func NewConfigStore(patcher Patcher) *ConfigStore {
	return &ConfigStore{patcher: patcher}
}

// Snapshot copies out all configs.
func (cs *ConfigStore) Snapshot() []*Config {
	configs := []*Config{}
	cs.configs.Range(func(_, v any) bool {
		configs = append(configs, v.(*Config).DeepCopy())
		return true
	})
	return configs
}

// Upsert sets or updates a config and pushes it to the subscribers.
func (cs *ConfigStore) Upsert(namespace, name string, stnrConfig *stnrv2.StunnerConfig) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	config := &Config{Namespace: namespace, Name: name, Config: stnrConfig}
	cs.configs.Store(config.Id(), config)
	for _, sub := range cs.subscribers(config) {
		cs.push(sub, config)
	}
}

// Delete removes a config and pushes the supplied config, usually a zero-config, to the
// subscribers; nil pushes nothing.
func (cs *ConfigStore) Delete(namespace, name string, stnrConfig *stnrv2.StunnerConfig) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	config := &Config{Namespace: namespace, Name: name, Config: stnrConfig}
	cs.configs.Delete(config.Id())
	if stnrConfig == nil {
		return
	}
	for _, sub := range cs.subscribers(config) {
		cs.push(sub, config)
	}
}

// Refresh patches every config again for every subscriber and pushes what changed: call it
// whenever what the patcher depends on may have changed.
func (cs *ConfigStore) Refresh() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	cs.configs.Range(func(_, v any) bool {
		config := v.(*Config)
		for _, sub := range cs.subscribers(config) {
			cs.push(sub, config)
		}
		return true
	})
}

// Get retrieves a config.
func (cs *ConfigStore) Get(namespace, name string) (*Config, bool) {
	v, ok := cs.configs.Load(namespace + "/" + name)
	if !ok {
		return nil, false
	}
	return v.(*Config).DeepCopy(), true
}

// Patch returns a copy of a config patched for a client with labels.
func (cs *ConfigStore) Patch(config *Config, labels map[string]string) *Config {
	c := config.DeepCopy()
	if cs.patcher != nil {
		c.Config = cs.patcher(c.Config, c.Id(), labels)
	}
	return c
}

// SubscribeAll subscribes to all config changes, unpatched.
func (cs *ConfigStore) SubscribeAll() chan *Config {
	return cs.subscribe("all", false, nil)
}

// SubscribeNamespace subscribes to all config changes in a namespace, unpatched.
func (cs *ConfigStore) SubscribeNamespace(namespace string) chan *Config {
	return cs.subscribe(fmt.Sprintf("%s/*", namespace), false, nil)
}

// SubscribeConfig subscribes to the changes of a config, patched for a client with labels.
func (cs *ConfigStore) SubscribeConfig(namespace, name string, labels map[string]string) chan *Config {
	return cs.subscribe(fmt.Sprintf("%s/%s", namespace, name), true, labels)
}

// Unsubscribe removes a subscription and closes its channel.
func (cs *ConfigStore) Unsubscribe(ch chan *Config) {
	v, ok := cs.subscriptions.LoadAndDelete(ch)
	if !ok {
		return
	}
	// release a push blocked on the subscription, then close the channel once no push can be
	// sending on it
	close(v.(*subscription).done)
	cs.mu.Lock()
	close(ch)
	cs.mu.Unlock()
}

// UnsubscribeAll removes all subscriptions.
func (cs *ConfigStore) UnsubscribeAll() {
	cs.subscriptions.Range(func(k, _ any) bool {
		cs.Unsubscribe(k.(chan *Config))
		return true
	})
}

// matchesTopic checks if a config matches a topic pattern.
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

// subscribers returns the subscriptions a config goes to.
func (cs *ConfigStore) subscribers(config *Config) []*subscription {
	ret := []*subscription{}
	cs.subscriptions.Range(func(_, v any) bool {
		if sub := v.(*subscription); matchesTopic(config, sub.topic) {
			ret = append(ret, sub)
		}
		return true
	})
	return ret
}

// subscribe creates a subscription to a topic and pushes it the matching configs.
func (cs *ConfigStore) subscribe(topic string, patch bool, labels map[string]string) chan *Config {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	sub := &subscription{
		topic:  topic,
		patch:  patch,
		labels: labels,
		ch:     make(chan *Config, SubscribeChannelBufferSize),
		done:   make(chan struct{}),
		sent:   map[string][]byte{},
	}
	cs.subscriptions.Store(sub.ch, sub)
	cs.configs.Range(func(_, v any) bool {
		if config := v.(*Config); matchesTopic(config, topic) {
			cs.push(sub, config)
		}
		return true
	})
	return sub.ch
}

// push sends a subscriber a config patched for it, unless the client already has it:
// configs are compared as the client receives them, as JSON. The caller holds mu.
func (cs *ConfigStore) push(sub *subscription, config *Config) {
	c := config.DeepCopy()
	if sub.patch {
		c = cs.Patch(config, sub.labels)
	}
	wire, err := json.Marshal(c.Config)
	if err == nil && bytes.Equal(sub.sent[c.Id()], wire) {
		return
	}
	select {
	case sub.ch <- c:
		sub.sent[c.Id()] = wire
	case <-sub.done:
	}
}
