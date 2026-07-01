package client

import (
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

func decodeConfig(r []byte) ([]*stnrv1.StunnerConfig, error) {
	c := stnrv1.StunnerConfig{}
	if err := json.Unmarshal(r, &c); err != nil {
		return nil, err
	}

	// copy

	return []*stnrv1.StunnerConfig{&c}, nil
}

func decodeConfigList(r []byte) ([]*stnrv1.StunnerConfig, error) {
	l := ConfigList{}
	if err := json.Unmarshal(r, &l); err != nil {
		return nil, err
	}
	return l.Items, nil
}

// getURI tries to parse an address or an URL or a file name into an URL.
func getURI(addr string) (*url.URL, error) {
	// Bracket any unbracketed IPv6 host:port in addr (RFC 3986) so
	// url.Parse and downstream net.Dial calls handle it correctly.
	// Without this an input like "http://2001:db8::1:8080" parses
	// (last colon read as port separator) but later fails inside
	// net.Dial with "too many colons in address". Works whether or not
	// addr already carries a scheme prefix (http://, ws://, etc.) and
	// whether or not it has a path/query suffix. Fixes #213.
	addr = bracketIPv6InURL(addr)

	// default URL scheme is "http"
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") &&
		!strings.HasPrefix(addr, "ws://") && !strings.HasPrefix(addr, "wss://") &&
		!strings.HasPrefix(addr, "file://") {
		addr = "http://" + addr
	}

	url, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	return url, nil
}

// bracketIPv6InURL splits addr into [scheme://][userinfo@][host:port][path/query],
// calls bracketIPv6HostPort on the host:port portion, and rejoins. Required
// because the gateway-operator hands stunnerd CDS addresses with the
// scheme already prefixed (e.g. "http://<ipv6>:<port>") — applying
// bracketIPv6HostPort to the full string would not match the host:port
// shape and would no-op.
func bracketIPv6InURL(addr string) string {
	var scheme string
	if i := strings.Index(addr, "://"); i >= 0 {
		scheme = addr[:i+3]
		addr = addr[i+3:]
	}
	pathStart := strings.IndexAny(addr, "/?")
	var hostport, tail string
	if pathStart < 0 {
		hostport = addr
	} else {
		hostport = addr[:pathStart]
		tail = addr[pathStart:]
	}

	// Separate userinfo credentials if present
	userinfo := ""
	if idx := strings.LastIndex(hostport, "@"); idx != -1 {
		userinfo = hostport[:idx+1]
		hostport = hostport[idx+1:]
	}

	return scheme + userinfo + bracketIPv6HostPort(hostport) + tail
}

// bracketIPv6HostPort returns addr unchanged for IPv4:port, hostname:port,
// already-bracketed [ipv6]:port, and inputs that don't look like host:port
// at all. For unbracketed IPv6 host:port inputs (e.g. "::1:8080" or
// "2001:db8::1:8080"), it returns the bracketed form ("[::1]:8080",
// "[2001:db8::1]:8080") so url.Parse handles them correctly.
//
// Note: strings like "::1:8080" are syntactically ambiguous — they are
// simultaneously valid bare IPv6 addresses and IPv6 host:port pairs. In
// the CDS client this helper is always called with operator-built
// host:port input, so the function deliberately biases toward the
// host:port interpretation when both are plausible. Callers that need to
// pass a bare IPv6 should bracket it themselves.
func bracketIPv6HostPort(addr string) string {
	// Already bracketed.
	if strings.HasPrefix(addr, "[") {
		return addr
	}
	// net.SplitHostPort succeeds for IPv4:port, hostname:port, [ipv6]:port.
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	// Otherwise: candidate unbracketed IPv6:port. Split on the last colon
	// and validate that the suspected port is numeric and the host portion
	// is a valid IPv6 address. This path also rejects bare IPv6 cleanly
	// (the trailing colon makes the host portion not parse as IP), so a
	// caller passing "2001:db8::1" gets it back unchanged.
	i := strings.LastIndex(addr, ":")
	if i < 0 {
		return addr
	}
	host, port := addr[:i], addr[i+1:]
	if _, err := strconv.Atoi(port); err != nil {
		return addr
	}
	if !strings.Contains(host, ":") || net.ParseIP(host) == nil {
		return addr
	}
	return net.JoinHostPort(host, port)
}

// wsURI returns a websocket url from a HTTP URI.
func wsURI(addr, endpoint, node string) (string, error) {
	uri, err := getURI(addr)
	if err != nil {
		return "", err
	}

	uri.Scheme = "ws"
	uri.Path = endpoint
	v := url.Values{}
	v.Set("watch", "true")
	if node != "" {
		v.Set("node", node)
	}
	uri.RawQuery = v.Encode()

	return uri.String(), nil
}
