package server

import (
	"fmt"
	"strings"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func NamespacedName(id string) (string, string, bool) {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

type Config struct {
	Namespace, Name string
	Config          *stnrv2.StunnerConfig
}

func (c *Config) String() string {
	return fmt.Sprintf("id=%s/%s: %s", c.Namespace, c.Name, c.Config.String())
}

func (c *Config) Id() string {
	return fmt.Sprintf("%s/%s", c.Namespace, c.Name)
}

func (c *Config) DeepCopy() *Config {
	d := &Config{}
	*d = *c
	d.Config = c.Config.DeepCopy()
	return d
}

func (c *Config) DeepEqual(d *Config) bool {
	return c.Namespace == d.Namespace && d.Name == c.Name && c.Config.DeepEqual(d.Config)
}

// parseLabels parses the label query parameters of a client, key=value each.
func parseLabels(params *[]string) map[string]string {
	if params == nil {
		return nil
	}
	ret := map[string]string{}
	for _, l := range *params {
		k, v, _ := strings.Cut(l, "=")
		ret[k] = v
	}
	return ret
}
