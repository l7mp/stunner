package v2

import (
	"fmt"
	"net"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ListenerConfig is a socket and the servers it feeds. Every listener has the same shape whatever
// server it feeds.
type ListenerConfig struct {
	// Name of the listener.
	Name string `json:"name,omitempty"`
	// Protocol is the transport the listener accepts clients over: UDP, TCP, TLS, DTLS or
	// STDIN. Default is UDP.
	Protocol string `json:"protocol,omitempty"`
	// Servers is the chain of servers the listener feeds, in order: each server either consumes
	// a conn or passes it on to the next one.
	Servers []string `json:"servers"`
	// Addr is the IP address the listener binds. Default (empty) is every address of the host,
	// both families on a dual-stack host.
	Addr string `json:"address,omitempty"`
	// Port is the port the listener binds. Default is 3478.
	Port int `json:"port,omitempty"`
	// PublicAddr is the address clients reach the listener at from outside (used to build ICE
	// server configurations, ignored by stunnerd). A bare host: an IP literal or a DNS name.
	PublicAddr string `json:"public_address,omitempty"`
	// PublicPort is the port clients reach the listener at from outside (ignored by stunnerd).
	PublicPort int `json:"public_port,omitempty"`
	// PublicAddrs is the list of public addresses of a dual-stack listener (ignored by
	// stunnerd).
	PublicAddrs []string `json:"public_addresses,omitempty"`
	// Cert is the base64-encoded TLS certificate of a TLS or DTLS listener.
	Cert string `json:"cert,omitempty"`
	// Key is the base64-encoded TLS key of a TLS or DTLS listener.
	Key string `json:"key,omitempty"`
	// PQCMode is the post-quantum key exchange policy of a TLS listener: "default",
	// "preferred" or "enforced".
	PQCMode string `json:"pqc_mode,omitempty"`
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

	// A STDIN listener serves the process stdio pair: it has no socket, so it ignores the
	// port, and gets no default for it.
	if proto != ProtocolSTDIN && req.Port == 0 {
		req.Port = DefaultPort
	}
	if req.Port < 0 || req.Port > 65535 {
		return fmt.Errorf("invalid port: %d", req.Port)
	}

	// Only TLS listeners take a PQC mode, the others ignore it.
	pqcMode, err := NewPQCMode(req.PQCMode)
	if err != nil {
		return err
	}
	req.PQCMode = ""
	if pqcMode != PQCModeDefault {
		req.PQCMode = pqcMode.String()
	}

	if req.PublicAddrs == nil {
		req.PublicAddrs = []string{}
	}

	if req.Servers == nil {
		req.Servers = []string{}
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
	status = append(status, fmt.Sprintf("%s://%s", strings.ToLower(req.Protocol),
		net.JoinHostPort(addr, strconv.Itoa(req.Port))))
	status = append(status, fmt.Sprintf("servers=[%s]", strings.Join(req.Servers, ",")))

	a, p := "-", "-"
	if req.PublicAddr != "" {
		a = req.PublicAddr
	}
	if req.PublicPort != 0 {
		p = strconv.Itoa(req.PublicPort)
	}
	status = append(status, fmt.Sprintf("public=%s", net.JoinHostPort(a, p)))

	c, k := "-", "-"
	if req.Cert != "" {
		c = "<SECRET>"
	}
	if req.Key != "" {
		k = "<SECRET>"
	}
	status = append(status, fmt.Sprintf("cert/key=%s/%s", c, k))
	if req.PQCMode != "" {
		status = append(status, fmt.Sprintf("pqc=%s", req.PQCMode))
	}

	return fmt.Sprintf("%q:{%s}", n, strings.Join(status, ","))
}

// ListenerStatus is the status of a listener: its config and its offload counters.
type ListenerStatus struct {
	*ListenerConfig
	Stats OffloadDirStat `json:"stats"`
}

// String stringifies the status.
func (req *ListenerStatus) String() string {
	return fmt.Sprintf("%s,offload(rx/tx): %d/%d pkts %d/%d bytes", req.ListenerConfig.String(),
		req.Stats.Rx.Pkts, req.Stats.Tx.Pkts, req.Stats.Rx.Bytes, req.Stats.Tx.Bytes)
}
