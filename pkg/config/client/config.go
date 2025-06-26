package client

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"
	"strconv"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	stnrv1a1 "github.com/l7mp/stunner/pkg/apis/v1alpha1"
	"sigs.k8s.io/yaml"
)

type ConfigSkeleton struct {
	ApiVersion string `json:"version"`
}

// EmptyConfig builds a minimal configuration. The minimal config defaults to static authentication
// with a dummy username and password and opens no listeners or clusters.
func EmptyConfig() *stnrv1.StunnerConfig {
	return &stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
		Admin:      stnrv1.AdminConfig{},
		Auth: stnrv1.AuthConfig{
			Type:  "static",
			Realm: stnrv1.DefaultRealm,
			Credentials: map[string]string{
				"username": "dummy-username",
				"password": "dummy-password",
			},
		},
		Listeners: []stnrv1.ListenerConfig{},
		Clusters:  []stnrv1.ClusterConfig{},
	}
}

// ZeroConfig builds a zero configuration used for bootstrapping STUNner.
func ZeroConfig(id string) *stnrv1.StunnerConfig {
	c := EmptyConfig()
	c.Admin = stnrv1.AdminConfig{Name: id}
	return c
}

// IsZeroConfig checks whether the given config is a bootstrap config.
func IsZeroConfig(req *stnrv1.StunnerConfig) bool {
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
func ParseConfig(c []byte) (*stnrv1.StunnerConfig, error) {
	// substitute environtment variables
	// default port: STUNNER_PUBLIC_PORT -> STUNNER_PORT
	re := regexp.MustCompile(`^[0-9]+$`)
	port, ok := os.LookupEnv("STUNNER_PORT")
	if !ok || port == "" || !re.Match([]byte(port)) {
		publicPort := stnrv1.DefaultPort
		publicPortStr, ok := os.LookupEnv("STUNNER_PUBLIC_PORT")
		if ok {
			if p, err := strconv.Atoi(publicPortStr); err == nil {
				publicPort = p
			}
		}
		os.Setenv("STUNNER_PORT", fmt.Sprintf("%d", publicPort))
	}

	// make sure credentials are not affected by environment substitution

	// parse up before env substitution is applied
	confRaw, err := parseRaw(c)
	if err != nil {
		return nil, err
	}

	// save credentials
	credRaw := make(map[string]string)
	maps.Copy(credRaw, confRaw.Auth.Credentials)

	// apply env substitution and parse again
	e := os.ExpandEnv(string(c))
	confExp, err := parseRaw([]byte(e))
	if err != nil {
		return nil, err
	}

	// restore credentials
	maps.Copy(confExp.Auth.Credentials, credRaw)

	return confExp, nil
}

func parseRaw(c []byte) (*stnrv1.StunnerConfig, error) {
	// try to parse only the config version first
	k := ConfigSkeleton{}
	if err := yaml.Unmarshal([]byte(c), &k); err != nil {
		if errJ := json.Unmarshal([]byte(c), &k); err != nil {
			return nil, fmt.Errorf("could not parse config file API version: "+
				"YAML parse error: %s, JSON parse error: %s\n",
				err.Error(), errJ.Error())
		}
	}

	s := stnrv1.StunnerConfig{}

	switch k.ApiVersion {
	case stnrv1.ApiVersion:
		if err := yaml.Unmarshal([]byte(c), &s); err != nil {
			if errJ := json.Unmarshal([]byte(c), &s); errJ != nil {
				return nil, fmt.Errorf("could not parse config file: "+
					"YAML parse error: %s, JSON parse error: %s\n",
					err.Error(), errJ.Error())
			}
		}
	case stnrv1a1.ApiVersion:
		a := stnrv1a1.StunnerConfig{}
		if err := yaml.Unmarshal([]byte(c), &a); err != nil {
			if errJ := json.Unmarshal([]byte(c), &a); errJ != nil {
				return nil, fmt.Errorf("could not parse config file: "+
					"YAML parse error: %s, JSON parse error: %s\n",
					err.Error(), errJ.Error())
			}
		}

		sv1, err := stnrv1a1.ConvertToV1(&a)
		if err != nil {
			return nil, fmt.Errorf("could not convert config to API V1: %s", err)
		}

		sv1.DeepCopyInto(&s)
	}

	return &s, nil
}

// IsConfigDeleted is a helper that allows to decide whether a config is being deleted. When a
// config is being removed (say, because the corresponding Gateway is deleted), the CDS server
// sends a validated zero-config for the client. This function is a quick helper to decide whether
// the config received is such a zero-config.
func IsConfigDeleted(conf *stnrv1.StunnerConfig) bool {
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
