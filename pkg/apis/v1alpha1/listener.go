package v1alpha1

import (
	"fmt"
	"net"
	"reflect"
	"sort"
)

// ListenerConfig specifies a particular listener for the STUNner deamon
type ListenerConfig struct {
	// Name is the name of the listener
	Name string `json:"name,omitempty"`
	// Protocol is the transport protocol used by the listener ("UDP", "TCP", "TLS", "DTLS")
	Protocol string `json:"protocol,omitempty"`
	// Addr is the IP address for the listener
	Addr string `json:"address,omitempty"`
	// Port is the port for the listener
	Port int `json:"port,omitempty"`
	// MinRelayPort is the smallest relay port assigned for the relay connections spawned by
	// the listener
	MinRelayPort int `json:"min_relay_port,omitempty"`
	// MaxRelayPort is the highest relay port assigned for the relay connections spawned by the
	// listener
	MaxRelayPort int `json:"max_relay_port,omitempty"`
	// Cert is the TLS cert
	Cert string `json:"cert,omitempty"`
	// Key is the TLS key
	Key string `json:"key,omitempty"`
	// Routes specifies the list of Routes allowed via a listener
	Routes []string `json:"routes,omitempty"`
}

// Validate checks a configuration and injects defaults
func (req *ListenerConfig) Validate() error {
	if req.Name == "" {
		return fmt.Errorf("missing name in listener configuration: %s", req.String())
	}

	if req.Protocol == "" {
		req.Protocol = DefaultProtocol
	}
	_, err := NewListenerProtocol(req.Protocol)
	if err != nil {
		return err
	}

	if req.Addr == "" {
		req.Addr = "0.0.0.0"
	}
	if net.ParseIP(req.Addr) == nil {
		return fmt.Errorf("invalid listener address %s in listener configuration: %q",
			req.Addr, req.String())
	}

	if req.Port == 0 {
		req.Port = DefaultPort
	}
	if req.MinRelayPort == 0 {
		req.MinRelayPort = DefaultMinRelayPort
	}
	if req.MaxRelayPort == 0 {
		req.MaxRelayPort = DefaultMaxRelayPort
	}
	for _, p := range []int{req.Port, req.MinRelayPort, req.MaxRelayPort} {
		if p <= 0 || p > 65535 {
			return fmt.Errorf("invalid port: %d", p)
		}
	}

	sort.Strings(req.Routes)

	return nil
}

// Name returns the name of the object to be configured
func (req *ListenerConfig) ConfigName() string {
	return req.Name
}

// DeepEqual compares two configurations
func (req *ListenerConfig) DeepEqual(other Config) bool {
	// routes must be sorted in both configs!
	return reflect.DeepEqual(req, other)
}

// String stringifies the configuration
func (l *ListenerConfig) String() string {
	return fmt.Sprintf("%s://%s:%d", l.Protocol, l.Addr, l.Port)
}
