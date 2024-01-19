package v1

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// ListenerConfig specifies a server socket on which STUN/TURN connections will be served.
type ListenerConfig struct {
	// Name of the listener.
	Name string `json:"name,omitempty"`
	// Protocol is the transport protocol ("UDP", "TCP", "TLS", "DTLS") or the complete L4/L7
	// protocol stack ("TURN-UDP", "TURN-TCP", "TURN-TLS", "TURN-DTLS") used by the listener.
	// The application-layer protocol on top of the transport protocol is always TURN, so "UDP"
	// and "TURN-UDP" are equivalent (and so on for the other protocols). Default is
	// "TURN-UDP".
	Protocol string `json:"protocol,omitempty"`
	// PublicAddr is the Internet-facing public IP address for the listener (ignored by
	// STUNner).
	PublicAddr string `json:"public_address,omitempty"`
	// PublicPort is the Internet-facing public port for the listener (ignored by STUNner).
	PublicPort int `json:"public_port,omitempty"`
	// Addr is the IP address for the listener. Default is localhost.
	Addr string `json:"address,omitempty"`
	// Port is the port for the listener. Default is the standard TURN port (3478).
	Port int `json:"port,omitempty"`
	// Cert is the base64-encoded TLS cert.
	Cert string `json:"cert,omitempty"`
	// Key is the base64-encoded TLS key.
	Key string `json:"key,omitempty"`
	// Routes specifies the list of Routes allowed via a listener.
	Routes []string `json:"routes,omitempty"`
}

// Validate checks a configuration and injects defaults.
func (req *ListenerConfig) Validate() error {
	if req.Name == "" {
		return fmt.Errorf("missing name in listener configuration: %s", req.String())
	}

	// Normalize
	if req.Protocol == "" {
		req.Protocol = DefaultProtocol
	}
	proto, err := NewListenerProtocol(req.Protocol)
	if err != nil {
		return err
	}
	req.Protocol = proto.String()

	if req.Addr == "" {
		req.Addr = "0.0.0.0"
	}

	if req.Port == 0 {
		req.Port = DefaultPort
	}
	if req.Port <= 0 || req.Port > 65535 {
		return fmt.Errorf("invalid port: %d", req.Port)
	}

	if proto == ListenerProtocolTURNTLS || proto == ListenerProtocolTURNDTLS ||
		proto == ListenerProtocolTLS || proto == ListenerProtocolDTLS {
		if req.Cert == "" {
			return fmt.Errorf("empty TLS cert for %s listener", proto.String())
		}
		if req.Key == "" {
			return fmt.Errorf("empty TLS key for %s listener", proto.String())
		}
	}

	if req.Routes == nil {
		req.Routes = []string{}
	}

	sort.Strings(req.Routes)
	return nil
}

// Name returns the name of the object to be configured.
func (req *ListenerConfig) ConfigName() string {
	return req.Name
}

// DeepEqual compares two configurations. Routes must be sorted in both configs!
func (req *ListenerConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// DeepCopyInto copies a configuration.
func (req *ListenerConfig) DeepCopyInto(dst Config) {
	ret := dst.(*ListenerConfig)
	*ret = *req
	ret.Routes = make([]string, len(req.Routes))
	copy(ret.Routes, req.Routes)
}

// String stringifies the configuration.
func (req *ListenerConfig) String() string {
	status := []string{}

	n := "-"
	if req.Name != "" {
		n = req.Name
	}

	addr := "0.0.0.0"
	if req.Addr != "" && req.Addr != "$STUNNER_ADDR" {
		addr = req.Addr
	}

	status = append(status, fmt.Sprintf("turn://%s:%d", addr, req.Port))

	a, p := "-", "-"
	if req.PublicAddr != "" {
		a = req.PublicAddr
	}
	if req.PublicPort != 0 {
		p = fmt.Sprintf("%d", req.PublicPort)
	}
	status = append(status, fmt.Sprintf("public=%s:%s", a, p))

	c, k := "-", "-"
	if req.Cert != "" {
		c = "<SECRET>"
	}
	if req.Key != "" {
		k = "<SECRET>"
	}
	status = append(status, fmt.Sprintf("cert/key=%s/%s", c, k))
	status = append(status, fmt.Sprintf("routes=[%s]", strings.Join(req.Routes, ",")))

	return fmt.Sprintf("%q:{%s}", n, strings.Join(status, ","))
}

// GetListenerURI is a helper that can output two types of Listener URIs: one with "://" after the
// scheme or one with only ":" (as per RFC7065).
func (req *ListenerConfig) GetListenerURI(rfc7065 bool) (string, error) {
	proto, err := NewListenerProtocol(req.Protocol)
	if err != nil {
		return "", err
	}

	service, protocol := "", ""
	switch proto {
	case ListenerProtocolTURNUDP:
		service = "turn"
		protocol = "udp"
	case ListenerProtocolTURNTCP:
		service = "turn"
		protocol = "tcp"
	case ListenerProtocolTURNDTLS:
		service = "turns"
		protocol = "udp"
	case ListenerProtocolTURNTLS:
		service = "turns"
		protocol = "tcp"
	}

	addr := req.PublicAddr
	if addr == "" {
		// Fallback to server addr
		addr = req.Addr
	}

	port := req.PublicPort
	if port == 0 {
		// Fallback to server addr
		port = req.Port
	}

	var uri string
	if rfc7065 {
		uri = fmt.Sprintf("%s:%s:%d?transport=%s", service, addr, port, protocol)
	} else {
		uri = fmt.Sprintf("%s://%s:%d?transport=%s", service, addr, port, protocol)
	}
	return uri, nil
}
