package stunner

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"

	// "github.com/pion/logging"
	// "github.com/pion/turn/v2"
	"sigs.k8s.io/yaml"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// NewDefaultStunnerConfig builds a default configuration from a STUNner URI. Example: the URI
// `turn://user:pass@127.0.0.1:3478?transport=udp` will be parsed into a STUNner configuration with
// a server running on the localhost at UDP port 3478, with plain-text authentication using the
// username/password pair `user:pass`.
func NewDefaultConfig(uri string) (*v1alpha1.StunnerConfig, error) {
	u, err := ParseUri(uri)
	if err != nil {
		return nil, fmt.Errorf("Invalid URI '%s': %s", uri, err)
	}

	if u.Username == "" || u.Password == "" {
		return nil, fmt.Errorf("Username/password must be set: '%s'", uri)
	}

	c := &v1alpha1.StunnerConfig{
		ApiVersion: v1alpha1.ApiVersion,
		Admin: v1alpha1.AdminConfig{
			LogLevel: v1alpha1.DefaultLogLevel,
		},
		Auth: v1alpha1.AuthConfig{
			Type:  "plaintext",
			Realm: v1alpha1.DefaultRealm,
			Credentials: map[string]string{
				"username": u.Username,
				"password": u.Password,
			},
		},
		Listeners: []v1alpha1.ListenerConfig{{
			Name:     "default-listener",
			Protocol: u.Protocol,
			Addr:     u.Address,
			Port:     u.Port,
			Routes:   []string{"allow-any"},
		}},
		Clusters: []v1alpha1.ClusterConfig{{
			Name:      "allow-any",
			Type:      "STATIC",
			Endpoints: []string{"0.0.0.0/0"},
		}},
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}

	return c, nil
}

// LoadConfig loads a configuration from a file, substituting environment variables for
// placeholders in the configuration file. Returns the new configuration or error if load fails
func LoadConfig(config string) (*v1alpha1.StunnerConfig, error) {
	c, err := os.ReadFile(config)
	if err != nil {
		return nil, fmt.Errorf("could not read config: %s\n", err.Error())
	}

	// substitute environtment variables
	// default port: STUNNER_PUBLIC_PORT -> STUNNER_PORT
	re := regexp.MustCompile(`^[0-9]+$`)
	port, ok := os.LookupEnv("STUNNER_PORT")
	if !ok || (ok && port == "") || (ok && !re.Match([]byte(port))) {
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
	if err = yaml.Unmarshal([]byte(e), &s); err != nil {
		// if it fails, try to json
		if errJ := json.Unmarshal([]byte(e), &s); err != nil {
			return nil, fmt.Errorf("could not parse config file at '%s': "+
				"YAML parse error: %s, JSON parse error: %s\n",
				config, err.Error(), errJ.Error())
		}
	}

	return &s, nil
}

// GetConfig returns the configuration of the running STUNner daemon
func (s *Stunner) GetConfig() *v1alpha1.StunnerConfig {
	s.log.Tracef("GetConfig")

	// singletons, but we want to avoid panics when GetConfig is called on an uninitialized
	// STUNner object
	adminConf := v1alpha1.AdminConfig{}
	if len(s.adminManager.Keys()) > 0 {
		adminConf = *s.GetAdmin().GetConfig().(*v1alpha1.AdminConfig)
	}

	authConf := v1alpha1.AuthConfig{}
	if len(s.authManager.Keys()) > 0 {
		authConf = *s.GetAuth().GetConfig().(*v1alpha1.AuthConfig)
	}

	listeners := s.listenerManager.Keys()
	clusters := s.clusterManager.Keys()

	c := v1alpha1.StunnerConfig{
		ApiVersion: s.version,
		Admin:      adminConf,
		Auth:       authConf,
		Listeners:  make([]v1alpha1.ListenerConfig, len(listeners)),
		Clusters:   make([]v1alpha1.ClusterConfig, len(clusters)),
	}

	for i, name := range listeners {
		c.Listeners[i] = *s.GetListener(name).GetConfig().(*v1alpha1.ListenerConfig)
	}

	for i, name := range clusters {
		c.Clusters[i] = *s.GetCluster(name).GetConfig().(*v1alpha1.ClusterConfig)
	}

	return &c
}
