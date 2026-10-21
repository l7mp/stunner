package client

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"

	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1" //nolint:staticcheck // loads deprecated v1 configs
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"sigs.k8s.io/yaml"
)

type ConfigSkeleton struct {
	ApiVersion string `json:"version"`
}

// EmptyConfig builds a minimal configuration. The minimal config has a default authentication
// policy, static with a dummy username and password, and no listeners, servers or clusters.
func EmptyConfig() *stnrv2.StunnerConfig {
	return &stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin:      stnrv2.AdminConfig{},
		Auths: []stnrv2.AuthConfig{{
			Name:  stnrv2.DefaultAuthName,
			Type:  "static",
			Realm: stnrv2.DefaultRealm,
			Credentials: map[string]string{
				"username": "dummy-username",
				"password": "dummy-password",
			},
		}},
		Listeners: []stnrv2.ListenerConfig{},
		Servers:   []stnrv2.ServerConfig{},
		Clusters:  []stnrv2.ClusterConfig{},
	}
}

// ZeroConfig builds a zero configuration used for bootstrapping STUNner.
func ZeroConfig(id string) *stnrv2.StunnerConfig {
	c := EmptyConfig()
	c.Admin = stnrv2.AdminConfig{Name: id}
	return c
}

// IsZeroConfig checks whether the given config is a bootstrap config.
func IsZeroConfig(req *stnrv2.StunnerConfig) bool {
	c := ZeroConfig(req.Admin.Name)
	// Set defaults
	if c.Validate() != nil {
		return false
	}
	// Override loglevel
	c.Admin.LogLevel = req.Admin.LogLevel
	return req.DeepEqual(c)
}

// Substitute substitutes the environment of this process into a config with os.ExpandEnv, as a
// dataplane does with its own config: the placeholders stand for the environment of the dataplane
// pod. The credentials are taken as they are, so a password holding a "$" stays intact.
func Substitute(c *stnrv2.StunnerConfig) (*stnrv2.StunnerConfig, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	s := &stnrv2.StunnerConfig{}
	if err := json.Unmarshal([]byte(os.ExpandEnv(string(b))), s); err != nil {
		return nil, fmt.Errorf("invalid config after substituting the environment: %w", err)
	}
	for i := range s.Auths {
		s.Auths[i].Credentials = maps.Clone(c.Auths[i].Credentials)
	}
	return s, nil
}

// ParseConfig parses a YAML or JSON config of either version as it is, returning it as v2: a v1
// config is converted. The v1 config API is deprecated.
func ParseConfig(c []byte) (*stnrv2.StunnerConfig, error) {
	k := ConfigSkeleton{}
	if err := unmarshal(c, &k); err != nil {
		return nil, err
	}
	switch k.ApiVersion {
	case stnrv2.ApiVersion:
		s := stnrv2.StunnerConfig{}
		if err := unmarshal(c, &s); err != nil {
			return nil, err
		}
		return &s, nil
	case stnrv1.ApiVersion:
		s := stnrv1.StunnerConfig{}
		if err := unmarshal(c, &s); err != nil {
			return nil, err
		}
		return stnrv1.ConvertToV2(&s)
	default:
		return nil, fmt.Errorf("unsupported config API version: %q", k.ApiVersion)
	}
}

// unmarshal parses a YAML or JSON config.
func unmarshal(c []byte, v any) error {
	if err := yaml.Unmarshal(c, v); err != nil {
		if errJ := json.Unmarshal(c, v); errJ != nil {
			return fmt.Errorf("could not parse config file: YAML parse error: %s, JSON parse "+
				"error: %s", err.Error(), errJ.Error())
		}
	}
	return nil
}

// IsConfigDeleted is a helper that allows to decide whether a config is being deleted. When a
// config is being removed (say, because the corresponding Gateway is deleted), the CDS server
// sends a validated zero-config for the client. This function is a quick helper to decide whether
// the config received is such a zero-config.
func IsConfigDeleted(conf *stnrv2.StunnerConfig) bool {
	if conf == nil {
		return false
	}
	// configs compare only validated: the clients return them as parsed
	c, zeroConf := conf.DeepCopy(), ZeroConfig(conf.Admin.Name)
	if c.Validate() != nil || zeroConf.Validate() != nil {
		return false
	}
	return c.DeepEqual(zeroConf)
}
