package configdiscoveryclient

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"

	"sigs.k8s.io/yaml"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// ZeroConfig builds a zero configuration useful for bootstrapping STUNner. It starts with
// plaintext authentication and opens no listeners and clusters.
func ZeroConfig(id string) *v1alpha1.StunnerConfig {
	return &v1alpha1.StunnerConfig{
		ApiVersion: v1alpha1.ApiVersion,
		Admin:      v1alpha1.AdminConfig{Name: id},
		Auth: v1alpha1.AuthConfig{
			Type:  "plaintext",
			Realm: v1alpha1.DefaultRealm,
			Credentials: map[string]string{
				"username": "dummy-username",
				"password": "dummy-password",
			},
		},
		Listeners: []v1alpha1.ListenerConfig{},
		Clusters:  []v1alpha1.ClusterConfig{},
	}
}

// ParseConfig parses a raw buffer holding a configuration, substituting environment variables for
// placeholders in the configuration. Returns the new configuration or error if parsing fails.
func ParseConfig(c []byte) (*v1alpha1.StunnerConfig, error) {
	// substitute environtment variables
	// default port: STUNNER_PUBLIC_PORT -> STUNNER_PORT
	re := regexp.MustCompile(`^[0-9]+$`)
	port, ok := os.LookupEnv("STUNNER_PORT")
	if !ok || port == "" || !re.Match([]byte(port)) {
		publicPort := v1alpha1.DefaultPort
		publicPortStr, ok := os.LookupEnv("STUNNER_PUBLIC_PORT")
		if ok {
			if p, err := strconv.Atoi(publicPortStr); err == nil {
				publicPort = p
			}
		}
		os.Setenv("STUNNER_PORT", fmt.Sprintf("%d", publicPort))
	}

	e := os.ExpandEnv(string(c))

	s := v1alpha1.StunnerConfig{}
	// try YAML first
	if err := yaml.Unmarshal([]byte(e), &s); err != nil {
		// if it fails, try to json
		if errJ := json.Unmarshal([]byte(e), &s); err != nil {
			return nil, fmt.Errorf("could not parse config file: "+
				"YAML parse error: %s, JSON parse error: %s\n",
				err.Error(), errJ.Error())
		}
	}

	return &s, nil
}
