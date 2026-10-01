package v2

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Endpoint is a parsed cluster endpoint: an IP address, an IP prefix or a domain name, with an
// optional port or port range. Ports are not enforced: a TURN server admits peers by IP, as TURN
// permissions do, and an L4 server dials the single port an endpoint names. A port range is
// accepted and ignored. The network is the business of the cluster's protocol. The syntax follows
// net.SplitHostPort, with a range allowed in place of the port:
//
//	10.1.0.0/16              a prefix, any port
//	10.1.0.7:5000            an address and a port: dialable
//	10.2.3.4:5000-5100       an address and a port range
//	[2001:db8::7]:5000       an IPv6 address and a port (brackets, as for net.SplitHostPort)
//	2001:db8::/32            an IPv6 prefix, any port (no brackets needed without a port)
//	media.default.svc:5000   a domain name, resolved by the cluster
type Endpoint struct {
	// Domain is the domain name of a domain endpoint, empty for an IP endpoint.
	Domain string
	// Prefix is the IP prefix of an IP endpoint (a single address is a full-length prefix),
	// nil for a domain endpoint.
	Prefix *net.IPNet
	// Port and EndPort bound the port range; both are 0 for any port, and equal for a single
	// port.
	Port, EndPort int

	// prefixLen records whether the endpoint was written as a prefix, so that a single address
	// and its full-length prefix stringify the way they were written.
	prefixLen bool
}

// ParseEndpoint parses a cluster endpoint.
func ParseEndpoint(ep string) (*Endpoint, error) {
	e := &Endpoint{}
	full := ep

	var host, portSpec string
	if h, p, err := net.SplitHostPort(ep); err == nil {
		host, portSpec = h, p
	} else {
		// no port: a bare IPv6 address or prefix does not split (too many colons), a
		// bracketed one without a port does not either
		host = strings.TrimSuffix(strings.TrimPrefix(ep, "["), "]")
	}
	if host == "" {
		return nil, fmt.Errorf("invalid endpoint %q: empty host", full)
	}

	if portSpec != "" {
		first, last, isRange := strings.Cut(portSpec, "-")
		port, err := strconv.Atoi(first)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid port in endpoint %q", ep)
		}
		endPort := port
		if isRange {
			endPort, err = strconv.Atoi(last)
			if err != nil || endPort < port || endPort > 65535 {
				return nil, fmt.Errorf("invalid port range in endpoint %q", ep)
			}
		}
		e.Port, e.EndPort = port, endPort
	}

	if ip := net.ParseIP(host); ip != nil {
		bits := 128
		if ip.To4() != nil {
			ip, bits = ip.To4(), 32
		}
		e.Prefix = &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}
		return e, nil
	}
	if _, prefix, err := net.ParseCIDR(host); err == nil {
		e.Prefix, e.prefixLen = prefix, true
		return e, nil
	}
	if strings.ContainsAny(host, "/[]") {
		return nil, fmt.Errorf("invalid endpoint %q", ep)
	}
	e.Domain = host
	return e, nil
}

// Contains reports whether ip is in the prefix of an IP endpoint. A domain endpoint contains
// nothing here: the server using it matches the addresses the domain resolves to.
func (e *Endpoint) Contains(ip net.IP) bool {
	return e.Prefix != nil && e.Prefix.Contains(ip)
}

// RestrictsPort reports whether the endpoint names a port or a port range narrower than every
// port: a restriction no server enforces.
func (e *Endpoint) RestrictsPort() bool {
	return e.Port > 1 || (e.Port == 1 && e.EndPort < 65535)
}

// HostPort returns the endpoint as a dialable "host:port" when it names a single address or a
// domain with a single port, and false otherwise (a prefix, a port range, or no port).
func (e *Endpoint) HostPort() (string, bool) {
	if e.prefixLen || e.Port == 0 || e.Port != e.EndPort {
		return "", false
	}
	host := e.Domain
	if e.Prefix != nil {
		host = e.Prefix.IP.String()
	}
	return net.JoinHostPort(host, strconv.Itoa(e.Port)), true
}

// String renders the endpoint in the syntax ParseEndpoint takes.
func (e *Endpoint) String() string {
	host := e.Domain
	if e.Prefix != nil {
		host = e.Prefix.IP.String()
		if e.prefixLen {
			host = e.Prefix.String()
		}
	}
	switch e.Port {
	case 0:
		return host
	case e.EndPort:
		return net.JoinHostPort(host, strconv.Itoa(e.Port))
	default:
		return net.JoinHostPort(host, fmt.Sprintf("%d-%d", e.Port, e.EndPort))
	}
}
