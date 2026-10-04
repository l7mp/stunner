package v2

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// Endpoint is a parsed cluster endpoint: an IP address or a domain name, with an optional port.
// The syntax follows net.SplitHostPort:
//
//	10.1.0.7                 an address
//	10.1.0.7:5000            an address and a port: dialable
//	[2001:db8::7]:5000       an IPv6 address and a port (brackets, as for net.SplitHostPort)
//	2001:db8::7              an IPv6 address (no brackets needed without a port)
//	media.default.svc:5000   a domain name, resolved by the cluster
type Endpoint struct {
	// Domain is the domain name of a domain endpoint, empty for an IP endpoint.
	Domain string
	// Addr is the address of an IP endpoint, invalid for a domain endpoint.
	Addr netip.Addr
	// Port is the port of the endpoint, 0 for none.
	Port int
}

// ParseEndpoint parses a cluster endpoint.
func ParseEndpoint(ep string) (*Endpoint, error) {
	e := &Endpoint{}

	var host, portSpec string
	if h, p, err := net.SplitHostPort(ep); err == nil {
		host, portSpec = h, p
	} else {
		// no port: a bare IPv6 address does not split (too many colons), a bracketed one
		// without a port does not either
		host = strings.TrimSuffix(strings.TrimPrefix(ep, "["), "]")
	}
	if host == "" {
		return nil, fmt.Errorf("invalid endpoint %q: empty host", ep)
	}

	if portSpec != "" {
		port, err := strconv.Atoi(portSpec)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid port in endpoint %q (port ranges are filters)", ep)
		}
		e.Port = port
	}

	if a, err := netip.ParseAddr(host); err == nil {
		e.Addr = a.Unmap()
		return e, nil
	}
	if strings.Contains(host, "/") {
		return nil, fmt.Errorf("invalid endpoint %q: an endpoint is an address, prefixes are "+
			"filters", ep)
	}
	if strings.ContainsAny(host, "[]") {
		return nil, fmt.Errorf("invalid endpoint %q", ep)
	}
	e.Domain = host
	return e, nil
}

// HostPort returns the endpoint as a dialable "host:port", and false when it has no port.
func (e *Endpoint) HostPort() (string, bool) {
	if e.Port == 0 {
		return "", false
	}
	return e.String(), true
}

// String renders the endpoint in the syntax ParseEndpoint takes.
func (e *Endpoint) String() string {
	host := e.Domain
	if e.Addr.IsValid() {
		host = e.Addr.String()
	}
	if e.Port == 0 {
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(e.Port))
}
