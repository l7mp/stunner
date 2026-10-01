package client

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"
	"strconv"

	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"sigs.k8s.io/yaml"
)

type ConfigSkeleton struct {
	ApiVersion string `json:"version"`
}

// EmptyConfig builds a minimal configuration. The minimal config defaults to static authentication
// with a dummy username and password and has no listeners, servers or clusters.
func EmptyConfig() *stnrv2.StunnerConfig {
	return &stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin:      stnrv2.AdminConfig{},
		Auth: stnrv2.AuthConfig{
			Type:  "static",
			Realm: stnrv2.DefaultRealm,
			Credentials: map[string]string{
				"username": "dummy-username",
				"password": "dummy-password",
			},
		},
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

// ParseConfig parses a raw buffer holding a configuration, substituting environment variables for
// placeholders in the configuration. Returns the new configuration or error if parsing fails.
func ParseConfig(c []byte) (*stnrv2.StunnerConfig, error) {
	// substitute environtment variables
	// default port: STUNNER_PUBLIC_PORT -> STUNNER_PORT
	re := regexp.MustCompile(`^[0-9]+$`)
	port, ok := os.LookupEnv("STUNNER_PORT")
	if !ok || port == "" || !re.Match([]byte(port)) {
		publicPort := stnrv2.DefaultPort
		publicPortStr, ok := os.LookupEnv("STUNNER_PUBLIC_PORT")
		if ok {
			if p, err := strconv.Atoi(publicPortStr); err == nil {
				publicPort = p
			}
		}
		os.Setenv("STUNNER_PORT", fmt.Sprintf("%d", publicPort)) //nolint:errcheck
	}

	// save the credentials before env substitution so that substitution does not affect them;
	// only the credentials are parsed here, as other fields (say, a numeric port) may hold
	// placeholders that parse only after substitution
	credRaw := struct {
		Auth struct {
			Credentials map[string]string `json:"credentials"`
		} `json:"auth"`
	}{}
	if err := unmarshal(c, &credRaw); err != nil {
		return nil, err
	}

	// apply env substitution and parse again
	e := os.ExpandEnv(string(c))
	confExp, err := parseRaw([]byte(e))
	if err != nil {
		return nil, err
	}

	// restore credentials
	maps.Copy(confExp.Auth.Credentials, credRaw.Auth.Credentials)

	return confExp, nil
}

// parseRaw parses a v1 or a v2 config, returning it as v2: a v1 config (what the gateway operator
// renders) is converted.
func parseRaw(c []byte) (*stnrv2.StunnerConfig, error) {
	// try to parse only the config version first
	k := ConfigSkeleton{}
	if err := yaml.Unmarshal(c, &k); err != nil {
		if errJ := json.Unmarshal(c, &k); errJ != nil {
			return nil, fmt.Errorf("could not parse config file API version: "+
				"YAML parse error: %s, JSON parse error: %s", err.Error(), errJ.Error())
		}
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
	zeroConf := ZeroConfig(conf.Admin.Name)
	// zeroconfs have to be explcitly validated before deepEq (the cds client validates)
	if err := zeroConf.Validate(); err != nil {
		return false
	}
	return conf.DeepEqual(zeroConf)
}
