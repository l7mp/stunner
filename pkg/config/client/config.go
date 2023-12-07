package client

import (
	"encoding/json"
	"fmt"
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

// ZeroConfig builds a zero configuration useful for bootstrapping STUNner. The minimal config
// defaults to static authentication with a dummy username and password and opens no listeners or
// clusters.
func ZeroConfig(id string) *stnrv1.StunnerConfig {
	return &stnrv1.StunnerConfig{
		ApiVersion: stnrv1.ApiVersion,
		Admin:      stnrv1.AdminConfig{Name: id},
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

	e := os.ExpandEnv(string(c))

	// try to parse only the config version first
	k := ConfigSkeleton{}
	if err := yaml.Unmarshal([]byte(e), &k); err != nil {
		if errJ := json.Unmarshal([]byte(e), &k); err != nil {
			return nil, fmt.Errorf("could not parse config file API version: "+
				"YAML parse error: %s, JSON parse error: %s\n",
				err.Error(), errJ.Error())
		}
	}

	s := stnrv1.StunnerConfig{}

	switch k.ApiVersion {
	case stnrv1.ApiVersion:
		if err := yaml.Unmarshal([]byte(e), &s); err != nil {
			if errJ := json.Unmarshal([]byte(e), &s); err != nil {
				return nil, fmt.Errorf("could not parse config file: "+
					"YAML parse error: %s, JSON parse error: %s\n",
					err.Error(), errJ.Error())
			}
		}
	case stnrv1a1.ApiVersion:
		a := stnrv1a1.StunnerConfig{}
		if err := yaml.Unmarshal([]byte(e), &a); err != nil {
			if errJ := json.Unmarshal([]byte(e), &a); err != nil {
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
