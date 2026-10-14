package v2

import (
	"fmt"
	"maps"
	"net"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ListenerConfig is a socket and the servers it feeds. A listener accepts connections or dials its
// peer when Connect is set.
type ListenerConfig struct {
	// Name of the listener.
	Name string `json:"name,omitempty"`
	// Protocol is the transport the listener accepts clients over: UDP, TCP, TLS, DTLS, STDIN,
	// or FIFO.
	Protocol string `json:"protocol,omitempty"`
	// Servers is the chain of servers the listener feeds, in order: each server either consumes
	// a conn or passes it on to the next one.
	Servers []string `json:"servers"`
	// Addr is the IP address the listener binds or a plain string for FIFOs. Default (empty)
	// is every address of the host, both families on a dual-stack host. A dialing listener
	// binds it as the local address.
	Addr string `json:"address,omitempty"`
	// Port is the port the listener binds, required for an accepting UDP, TCP, TLS or DTLS
	// listener. A dialing listener binds it as the local port, 0 is an ephemeral port.
	Port int `json:"port,omitempty"`
	// PublicAddr is the address clients reach the listener at from outside (used to build ICE
	// server configurations, ignored by stunnerd). A bare host: an IP literal or a DNS name.
	PublicAddr string `json:"public_address,omitempty"`
	// PublicPort is the port clients reach the listener at from outside (ignored by stunnerd).
	PublicPort int `json:"public_port,omitempty"`
	// PublicAddrs is the list of public addresses of a dual-stack listener (ignored by
	// stunnerd).
	PublicAddrs []string `json:"public_addresses,omitempty"`
	// Connect makes the listener dial its peer instead of accepting conns, and redial whenever
	// the conn closes. Empty is an accepting listener.
	Connect *ListenerConnectConfig `json:"connect,omitempty"`
	// TLS is the TLS or DTLS of an accepting TLS or DTLS listener, the others ignore it: the
	// listener presents its cert. A dialing listener takes its TLS from Connect.
	TLS *TLSConfig `json:"tls,omitempty"`
	// Filters name the filters checking a client's source before a conn is made for it.
	Filters []string `json:"filters,omitempty"`
	// Labels are key-value pairs every conn of the listener carries in its tag, telling the
	// servers what the conn is for.
	Labels map[string]string `json:"labels,omitempty"`
}

// ListenerConnectConfig is the peer a dialing listener dials. Retry and keepalive are automatic.
type ListenerConnectConfig struct {
	// Address is the host:port of the peer, the host re-resolved on every dial.
	Address string `json:"address"`
	// TLS is how a TLS or DTLS listener verifies the peer and, optionally, the certificate it
	// presents; nil is the system roots and the host of Address. Other listeners ignore it.
	TLS *TLSConfig `json:"tls,omitempty"`
}

// DeepCopy copies a connect configuration.
func (c *ListenerConnectConfig) DeepCopy() *ListenerConnectConfig {
	ret := *c
	if c.TLS != nil {
		ret.TLS = c.TLS.DeepCopy()
	}
	return &ret
}

// Validate checks a configuration and injects defaults.
func (req *ListenerConfig) Validate() error {
	if req.Name == "" {
		return fmt.Errorf("missing name in listener configuration: %s", req.String())
	}
	if req.Protocol == "" {
		req.Protocol = DefaultListenerProtocol
	}
	proto, err := NewListenerProtocol(req.Protocol)
	if err != nil {
		return err
	}
	req.Protocol = proto.String()

	// A STDIN listener serves the process stdio pair and a FIFO listener a named fifo: neither
	// has a socket, so they ignore the port. A dialing listener binds an ephemeral port by
	// default.
	if proto == ProtocolFIFO && req.Addr == "" {
		return fmt.Errorf("FIFO listener %q names no fifo in its address", req.Name)
	}
	if proto != ProtocolSTDIN && proto != ProtocolFIFO && req.Connect == nil && req.Port == 0 {
		return fmt.Errorf("missing port in %s listener %q", proto.String(), req.Name)
	}
	if req.Port < 0 || req.Port > 65535 {
		return fmt.Errorf("invalid port: %d", req.Port)
	}

	if c := req.Connect; c != nil {
		host, port, err := net.SplitHostPort(c.Address)
		if err != nil {
			return fmt.Errorf("invalid connect address %q in listener %q: %w", c.Address,
				req.Name, err)
		}
		if p, err := strconv.Atoi(port); host == "" || err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("invalid connect address %q in listener %q: want host:port",
				c.Address, req.Name)
		}
		if c.TLS != nil {
			if err := c.TLS.Validate(); err != nil {
				return fmt.Errorf("%w in the connect block of listener %q", err, req.Name)
			}
		}
	}

	if req.TLS != nil {
		if err := req.TLS.Validate(); err != nil {
			return fmt.Errorf("%w in listener %q", err, req.Name)
		}
	}

	if req.PublicAddrs == nil {
		req.PublicAddrs = []string{}
	}

	if req.Servers == nil {
		req.Servers = []string{}
	}

	if req.Filters == nil {
		req.Filters = []string{}
	}
	if req.Labels == nil {
		req.Labels = map[string]string{}
	}

	return nil
}

// FirstServer returns the server at the head of the listener's chain, or "" when it names none.
func (req *ListenerConfig) FirstServer() string {
	if len(req.Servers) == 0 {
		return ""
	}
	return req.Servers[0]
}

// ConfigName returns the name of the object to be configured.
func (req *ListenerConfig) ConfigName() string {
	return req.Name
}

// DeepEqual compares two configurations.
func (req *ListenerConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// DeepCopyInto copies a configuration.
func (req *ListenerConfig) DeepCopyInto(dst Config) {
	ret := dst.(*ListenerConfig)
	*ret = *req
	ret.Servers = slices.Clone(req.Servers)
	ret.PublicAddrs = slices.Clone(req.PublicAddrs)
	ret.Filters = slices.Clone(req.Filters)
	ret.Labels = maps.Clone(req.Labels)
	if req.Connect != nil {
		ret.Connect = req.Connect.DeepCopy()
	}
	if req.TLS != nil {
		ret.TLS = req.TLS.DeepCopy()
	}
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
	if req.Protocol == ProtocolFIFO.String() {
		status = append(status, "fifo://"+req.Addr)
	} else {
		status = append(status, fmt.Sprintf("%s://%s", strings.ToLower(req.Protocol),
			net.JoinHostPort(addr, strconv.Itoa(req.Port))))
	}
	if c := req.Connect; c != nil {
		status = append(status, "connect="+c.Address)
		if c.TLS != nil {
			status = append(status, "connect-"+c.TLS.String())
		}
	}
	status = append(status, fmt.Sprintf("servers=[%s]", strings.Join(req.Servers, ",")))

	a, p := "-", "-"
	if req.PublicAddr != "" {
		a = req.PublicAddr
	}
	if req.PublicPort != 0 {
		p = strconv.Itoa(req.PublicPort)
	}
	status = append(status, fmt.Sprintf("public=%s", net.JoinHostPort(a, p)))

	if req.TLS != nil {
		status = append(status, req.TLS.String())
	}
	if len(req.Filters) > 0 {
		status = append(status, fmt.Sprintf("filters=[%s]", strings.Join(req.Filters, ",")))
	}
	if len(req.Labels) > 0 {
		labels := []string{}
		for _, k := range slices.Sorted(maps.Keys(req.Labels)) {
			labels = append(labels, k+"="+req.Labels[k])
		}
		status = append(status, fmt.Sprintf("labels={%s}", strings.Join(labels, ",")))
	}

	return fmt.Sprintf("%q:{%s}", n, strings.Join(status, ","))
}

// ListenerStatus is the status of a listener: its config and the traffic of its clients.
type ListenerStatus struct {
	*ListenerConfig
	Stats Stats `json:"stats"`
}

// String stringifies the status.
func (req *ListenerStatus) String() string {
	return req.ListenerConfig.String() + "," + req.Stats.String()
}
