package v2

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// URI is a parsed STUN/TURN server URI: the transport, the server and the credentials it carries.
// Parsing resolves nothing.
type URI struct {
	// Protocol is the transport: TURN-UDP, TURN-TCP, TURN-TLS or TURN-DTLS for a "turn" or
	// "turns" URI, else the plain transport the scheme names (UDP, TCP, ...).
	Protocol Protocol
	// Host is the IP address or domain name of the server.
	Host string
	// Port is the port of the server: the one given, else 3478, or 443 over (D)TLS.
	Port int
	// Username and Password are the credentials of the URI's userinfo, if any.
	Username, Password string
}

// ParseURI parses a STUN/TURN server URI. It accepts the ICE server form of RFC 7065
// ("turn:host:port?transport=tcp", "turns:host:port?transport=udp"), the hierarchical form
// ("turn://user:pass@host:port?transport=udp") and plain transport schemes ("udp://host:port").
// IPv6 hosts must be bracketed.
func ParseURI(uri string) (*URI, error) {
	// RFC 7065 TURN URIs use an opaque form ("turn:host:port?...") that net/url does not split into
	// host and port: upgrade them to "turn://host:port?..." so url.Parse fills Host and User.
	u, err := url.Parse(upgradeTURNURI(uri))
	if err != nil {
		return nil, fmt.Errorf("invalid URI %q: %w", uri, err)
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && ip.To4() == nil && u.Port() != "" &&
		u.Host != net.JoinHostPort(u.Hostname(), u.Port()) {
		return nil, fmt.Errorf("invalid URI %q: IPv6 host must be bracketed, e.g. [%s]", uri,
			u.Hostname())
	}

	proto, err := uriProtocol(u)
	if err != nil {
		return nil, err
	}
	ret := &URI{Protocol: proto, Host: u.Hostname(), Username: u.User.Username(), Port: 3478}
	if password, found := u.User.Password(); found {
		ret.Password = password
	}
	if proto == ProtocolTURNTLS || proto == ProtocolTURNDTLS {
		ret.Port = 443
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid port in URI %q", uri)
		}
		ret.Port = port
	}
	if ret.Host == "" {
		return nil, fmt.Errorf("invalid URI %q: empty host", uri)
	}
	return ret, nil
}

// String renders a TURN URI in the ICE server form of RFC 7065, without credentials:
// "turn:host:port?transport=udp", with "turns" over TLS and DTLS. Other URIs render as
// "protocol://host:port".
func (u *URI) String() string {
	switch u.Protocol {
	case ProtocolTURNUDP:
		return "turn:" + u.HostPort() + "?transport=udp"
	case ProtocolTURNTCP:
		return "turn:" + u.HostPort() + "?transport=tcp"
	case ProtocolTURNTLS:
		return "turns:" + u.HostPort() + "?transport=tcp"
	case ProtocolTURNDTLS:
		return "turns:" + u.HostPort() + "?transport=udp"
	default:
		return strings.ToLower(u.Protocol.String()) + "://" + u.HostPort()
	}
}

// NewURIFromListener returns the TURN URI of a listener feeding a TURN server: the listener's
// transport makes the TURN transport, the public address and port are preferred over the listen
// address and port, and a missing or placeholder address is "0.0.0.0".
func NewURIFromListener(l *ListenerConfig) (*URI, error) {
	proto, err := NewListenerProtocol(l.Protocol)
	if err != nil {
		return nil, err
	}
	turnProto, ok := map[Protocol]Protocol{
		ProtocolUDP:  ProtocolTURNUDP,
		ProtocolTCP:  ProtocolTURNTCP,
		ProtocolTLS:  ProtocolTURNTLS,
		ProtocolDTLS: ProtocolTURNDTLS,
	}[proto]
	if !ok {
		return nil, fmt.Errorf("no TURN URI for a %s listener", proto.String())
	}

	addr := l.PublicAddr
	if addr == "" {
		addr = l.Addr
	}
	if addr == "" || addr == DefaultNodeAddressPlaceholder {
		addr = "0.0.0.0"
	}
	port := l.PublicPort
	if port == 0 {
		port = l.Port
	}
	return &URI{Protocol: turnProto, Host: addr, Port: port}, nil
}

// HostPort returns the server as "host:port", bracketing an IPv6 host.
func (u *URI) HostPort() string { return net.JoinHostPort(u.Host, strconv.Itoa(u.Port)) }

// upgradeTURNURI rewrites an RFC 7065 TURN URI ("turn:host..." / "turns:host...") to the
// hierarchical "turn://host..." form. URIs already using "//", or carrying no TURN scheme, are
// returned unchanged.
func upgradeTURNURI(uri string) string {
	lower := strings.ToLower(uri)
	for _, scheme := range []string{"turn", "turns"} {
		prefix := scheme + ":"
		if strings.HasPrefix(lower, prefix) && !strings.HasPrefix(lower, prefix+"//") {
			return uri[:len(prefix)] + "//" + uri[len(prefix):]
		}
	}
	return uri
}

// uriProtocol derives the transport of a parsed URI: a "turn"/"turns" scheme maps to a TURN
// transport through its "?transport=" query as in RFC 7065, any other scheme names a plain
// transport. An empty scheme is "turn".
func uriProtocol(u *url.URL) (Protocol, error) {
	scheme := strings.ToLower(u.Scheme)
	if scheme == "" {
		scheme = "turn"
	}
	if scheme != "turn" && scheme != "turns" {
		p, err := NewProtocol(scheme)
		if err != nil {
			return ProtocolUnknown, fmt.Errorf("invalid scheme %q in URI %q", scheme, u.String())
		}
		return p, nil
	}

	transport := "udp"
	if q := u.Query()["transport"]; len(q) > 0 {
		transport = strings.ToLower(q[0])
	}
	switch {
	case transport == "tls":
		return ProtocolTURNTLS, nil
	case transport == "dtls":
		return ProtocolTURNDTLS, nil
	case scheme == "turn" && transport == "udp":
		return ProtocolTURNUDP, nil
	case scheme == "turn" && transport == "tcp":
		return ProtocolTURNTCP, nil
	case scheme == "turns" && transport == "udp":
		return ProtocolTURNDTLS, nil
	case scheme == "turns" && transport == "tcp":
		return ProtocolTURNTLS, nil
	default:
		return ProtocolUnknown, fmt.Errorf("invalid transport %q in URI %q", transport, u.String())
	}
}
