package server

import (
	"fmt"
	"sync"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/config/client/api"
)

type ConfigList = api.V1ConfigList

type Config struct {
	Id     string
	Config *stnrv1.StunnerConfig
}

func (e Config) String() string {
	return fmt.Sprintf("id=%s: %s", e.Id, e.Config.String())
}

// UpsertConfig upserts a single config in the server.
func (s *Server) UpsertConfig(id string, c *stnrv1.StunnerConfig) {
	cpy := &stnrv1.StunnerConfig{}
	c.DeepCopyInto(cpy)
	s.configs.Upsert(id, cpy)
	s.configCh <- Config{Id: id, Config: cpy}
}

// DeleteConfig should remove a config from the client. Theoretically, this would be done by
// sending the client a zero-config. However, in order to avoid that a client being removed and
// entering the graceful shutdown cycle receive a zeroconfig and abruprly kill all listeners with
// all active connections allocated to it, currently we suppress the config update.
func (s *Server) DeleteConfig(id string) {
	s.configs.Delete(id)
	s.log.Info("suppressing config update for terminating client", "client", id)
	// s.configCh <- Config{Id: id, Config: client.ZeroConfig(id)}
}

// UpdateConfig receives a set of ids and newConfigs that represent the state-of-the-world at a
// particular instance of time and generates an update per each change.
func (s *Server) UpdateConfig(newConfigs []Config) error {
	s.log.V(4).Info("processing config updates", "num-configs", len(newConfigs))
	oldConfigs := s.configs.Snapshot()

	for _, oldC := range oldConfigs {
		found := false
		for _, newC := range newConfigs {
			if oldC.Id == newC.Id {
				if !oldC.Config.DeepEqual(newC.Config) {
					s.log.V(2).Info("updating config", "client", newC.Id, "config",
						newC.Config.String())
					s.UpsertConfig(newC.Id, newC.Config)
				} else {
					s.log.V(2).Info("config not updated", "client", newC.Id,
						"old-config", oldC.Config.String(),
						"new-config", newC.Config.String())
				}
				found = true
				break
			}
		}

		if !found {
			s.log.V(2).Info("removing config", "client", oldC.Id)
			s.DeleteConfig(oldC.Id)
		}
	}

	for _, newC := range newConfigs {
		found := false
		for _, oldC := range oldConfigs {
			if oldC.Id == newC.Id {
				found = true
				break
			}
		}

		if !found {
			s.log.V(2).Info("adding config", "client", newC.Id, "config", newC.Config)
			s.UpsertConfig(newC.Id, newC.Config)
		}
	}

	return nil
}

type ConfigStore struct {
	configs map[string]*stnrv1.StunnerConfig
	lock    sync.RWMutex
}

func NewConfigStore() *ConfigStore {
	return &ConfigStore{
		configs: make(map[string]*stnrv1.StunnerConfig),
	}
}

func (t *ConfigStore) Get(id string) *stnrv1.StunnerConfig {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.configs[id]
}

func (t *ConfigStore) Snapshot() []Config {
	t.lock.RLock()
	defer t.lock.RUnlock()
	ret := []Config{}
	for id, c := range t.configs {
		ret = append(ret, Config{Id: id, Config: c})
	}
	return ret
}

func (t *ConfigStore) Upsert(id string, c *stnrv1.StunnerConfig) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.configs[id] = c
}
func (t *ConfigStore) Delete(id string) {
	t.lock.Lock()
	defer t.lock.Unlock()
	delete(t.configs, id)
}
