package server

import (
	"fmt"
	"strings"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// ClientFilter lets a client to filter push notifications.
type ClientFilter[T comparable] func(expectedValue T) bool

func NamespacedName(id string) (string, string, bool) {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

type Config struct {
	Namespace, Name string
	Config          *stnrv1.StunnerConfig
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
