package util

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
)

var endPointMatcher = regexp.MustCompile("^(.*):<([0-9]+)-([0-9]+)>$")

// Endpoint is a pair of an IP prefix and a port range.
type Endpoint struct {
	prefix                net.IPNet
	port, endPort         int
	hasPrefixLen, hasPort bool
}

// ParseEndpoint parses an endpoint from the canonical format: "<IP>[optional slash and prefix length]:<minPort-maxPort)."
func ParseEndpoint(ep string) (*Endpoint, error) {
	// separate the ports
	var port, endPort int
	var err error
	hasPrefixLen := true
	hasPort := false

	cidr := ep
	m := endPointMatcher.FindStringSubmatch(ep)
	if len(m) != 4 {
		// no ports at the end
		port = 1
		endPort = 65535
	} else {
		port, err = strconv.Atoi(m[2])
		if err != nil {
			return nil, fmt.Errorf("invalid port in endpoint %q: %w", ep, err)
		}
		endPort, err = strconv.Atoi(m[3])
		if err != nil {
			return nil, fmt.Errorf("invalid end-port in endpoint %q: %w", ep, err)
		}
		cidr = m[1]
		hasPort = true
	}

	// is IP address a plain IP?
	if ip := net.ParseIP(cidr); ip != nil {
		hasPrefixLen = false
		if ip.To4() != nil {
			cidr += "/32"
		} else {
			cidr += "/128"
		}
	}

	// convert to an actual prefix
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint %q: %w", ep, err)
	}

	return &Endpoint{
		prefix:       *ipnet,
		port:         port,
		endPort:      endPort,
		hasPrefixLen: hasPrefixLen,
		hasPort:      hasPort,
	}, nil

}

// Contains reports whether the endppoint network includes ip.
func (ep *Endpoint) Contains(ip net.IP) bool {
	return ep.Match(ip, 0)
}

// Contains reports whether the endppoint network includes ip and port.  If port is zero then
// port-matching is disabled.
func (ep *Endpoint) Match(ip net.IP, port int) bool {
	if !ep.prefix.Contains(ip) {
		return false
	}
	if port != 0 {
		return ep.port <= port && ep.endPort >= port
	} else {
		return true
	}
}

func (ep *Endpoint) Network() string {
	return ep.prefix.Network()
}

func (ep *Endpoint) String() string {
	ip, portRange := "", ""
	if ep.hasPrefixLen {
		ip = ep.prefix.String()
	} else {
		ip = ep.prefix.IP.String()
	}
	if ep.hasPort {
		portRange = fmt.Sprintf(":<%d-%d>", ep.port, ep.endPort)
	}

	return ip + portRange
}
