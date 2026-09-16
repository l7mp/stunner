package stunner

import (
	"net/url"
	"testing"

	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradeTurnURI(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"opaque turn", "turn:1.2.3.4:5555?transport=tcp", "turn://1.2.3.4:5555?transport=tcp"},
		{"opaque turns", "turns:1.2.3.4:5555?transport=tcp", "turns://1.2.3.4:5555?transport=tcp"},
		{"opaque no port", "turn:example.com?transport=udp", "turn://example.com?transport=udp"},
		{"opaque ipv6", "turn:[2001:db8::1]:5555", "turn://[2001:db8::1]:5555"},
		{"opaque with userinfo", "turn:user:pass@h:1", "turn://user:pass@h:1"},
		{"uppercase scheme", "TURN:1.2.3.4:5555", "TURN://1.2.3.4:5555"},
		{"already hierarchical", "turn://1.2.3.4:5555", "turn://1.2.3.4:5555"},
		{"already hierarchical turns", "turns://h:1", "turns://h:1"},
		{"non-turn scheme untouched", "http://1.2.3.4", "http://1.2.3.4"},
		{"turnfoo not matched", "turnfoo:1.2.3.4", "turnfoo:1.2.3.4"},
		{"dash untouched", "-", "-"},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, upgradeTurnURI(tc.in))
		})
	}
}

func TestParseURI(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		uri                                string
		proto, address, username, password string
		port                               int
		network                            string // expected Addr.Network(), "" to skip
		addr                               string // expected Addr.String(), "" to skip
		wantErr                            bool
	}{
		// hierarchical form, udp
		{name: "udp with creds", uri: "turn://user1:passwd1@1.2.3.4:3478?transport=udp",
			proto: "TURN-UDP", address: "1.2.3.4", username: "user1", password: "passwd1",
			port: 3478, network: "udp", addr: "1.2.3.4:3478"},
		{name: "udp no creds", uri: "turn://1.2.3.4:3478?transport=udp",
			proto: "TURN-UDP", address: "1.2.3.4", port: 3478, network: "udp"},
		{name: "udp default port", uri: "turn://1.2.3.4?transport=udp",
			proto: "TURN-UDP", address: "1.2.3.4", port: 3478, network: "udp"},
		{name: "udp default transport", uri: "turn://1.2.3.4:3478",
			proto: "TURN-UDP", address: "1.2.3.4", port: 3478, network: "udp"},
		// tcp
		{name: "tcp", uri: "turn://1.2.3.4:3478?transport=tcp",
			proto: "TURN-TCP", address: "1.2.3.4", port: 3478, network: "tcp", addr: "1.2.3.4:3478"},
		// tls (turns + tcp)
		{name: "tls explicit port", uri: "turns://1.2.3.4:5349?transport=tcp",
			proto: "TURN-TLS", address: "1.2.3.4", port: 5349, network: "tcp"},
		{name: "tls default port 443", uri: "turns://1.2.3.4?transport=tcp",
			proto: "TURN-TLS", address: "1.2.3.4", port: 443, network: "tcp"},
		// dtls (transport=dtls, or turns + udp)
		{name: "dtls via transport", uri: "turn://1.2.3.4:3478?transport=dtls",
			proto: "TURN-DTLS", address: "1.2.3.4", port: 3478, network: "udp"},
		{name: "dtls via turns+udp default port 443", uri: "turns://1.2.3.4?transport=udp",
			proto: "TURN-DTLS", address: "1.2.3.4", port: 443, network: "udp"},

		// RFC 7065 opaque form (single colon after scheme): the new capability
		{name: "rfc7065 udp", uri: "turn:1.2.3.4:5555?transport=udp",
			proto: "TURN-UDP", address: "1.2.3.4", port: 5555, network: "udp", addr: "1.2.3.4:5555"},
		{name: "rfc7065 tcp", uri: "turn:1.2.3.4:5555?transport=tcp",
			proto: "TURN-TCP", address: "1.2.3.4", port: 5555, network: "tcp"},
		{name: "rfc7065 tls", uri: "turns:1.2.3.4:5349?transport=tcp",
			proto: "TURN-TLS", address: "1.2.3.4", port: 5349, network: "tcp"},
		{name: "rfc7065 default port", uri: "turn:1.2.3.4?transport=udp",
			proto: "TURN-UDP", address: "1.2.3.4", port: 3478, network: "udp"},

		// plain transport schemes (tunnel client addresses, not TURN)
		{name: "plain udp", uri: "udp://1.2.3.4:5000",
			proto: "UDP", address: "1.2.3.4", port: 5000, network: "udp", addr: "1.2.3.4:5000"},
		{name: "plain tcp", uri: "tcp://1.2.3.4:5000",
			proto: "TCP", address: "1.2.3.4", port: 5000, network: "tcp", addr: "1.2.3.4:5000"},
		{name: "plain udp ipv6", uri: "udp://[2001:db8::1]:5000",
			proto: "UDP", address: "2001:db8::1", port: 5000, network: "udp", addr: "[2001:db8::1]:5000"},

		// IPv6, hierarchical (bracketed)
		{name: "ipv6 udp", uri: "turn://[2001:db8::1]:3478?transport=udp",
			proto: "TURN-UDP", address: "2001:db8::1", port: 3478, network: "udp", addr: "[2001:db8::1]:3478"},
		{name: "ipv6 tcp with creds", uri: "turn://user1:passwd1@[2001:db8::1]:3478?transport=tcp",
			proto: "TURN-TCP", address: "2001:db8::1", username: "user1", password: "passwd1",
			port: 3478, network: "tcp", addr: "[2001:db8::1]:3478"},
		{name: "ipv6 loopback default port", uri: "turn://[::1]?transport=udp",
			proto: "TURN-UDP", address: "::1", port: 3478, network: "udp", addr: "[::1]:3478"},
		// IPv6, RFC 7065 opaque
		{name: "ipv6 rfc7065", uri: "turn:[2001:db8::1]:5555?transport=tcp",
			proto: "TURN-TCP", address: "2001:db8::1", port: 5555, network: "tcp", addr: "[2001:db8::1]:5555"},

		// case-insensitive scheme
		{name: "uppercase scheme hierarchical", uri: "TURN://1.2.3.4:3478?transport=udp",
			proto: "TURN-UDP", address: "1.2.3.4", port: 3478, network: "udp"},
		{name: "uppercase scheme opaque", uri: "TURN:1.2.3.4:5555?transport=tcp",
			proto: "TURN-TCP", address: "1.2.3.4", port: 5555, network: "tcp"},

		// error cases
		{name: "unix scheme rejected", uri: "unix:///tmp/socket", wantErr: true},
		{name: "unbracketed ipv6 rejected", uri: "turn://2001:db8::1:3478?transport=udp", wantErr: true},
		{name: "unbracketed ipv6 opaque rejected", uri: "turn:2001:db8::1:3478?transport=udp", wantErr: true},
		{name: "invalid scheme", uri: "http://1.2.3.4:3478", wantErr: true},
		{name: "invalid transport", uri: "turn://1.2.3.4:3478?transport=sctp", wantErr: true},
		{name: "invalid port", uri: "turn://1.2.3.4:notaport?transport=udp", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, err := ParseURI(tc.uri)
			if tc.wantErr {
				assert.Error(t, err, "expected error")
				return
			}
			require.NoError(t, err, "parse")
			assert.Equal(t, tc.proto, u.Protocol, "protocol")
			assert.Equal(t, tc.address, u.Address, "address")
			assert.Equal(t, tc.username, u.Username, "username")
			assert.Equal(t, tc.password, u.Password, "password")
			assert.Equal(t, tc.port, u.Port, "port")
			if tc.network != "" {
				require.NotNil(t, u.Addr, "addr")
				assert.Equal(t, tc.network, u.Addr.Network(), "addr network")
			}
			if tc.addr != "" {
				require.NotNil(t, u.Addr, "addr")
				assert.Equal(t, tc.addr, u.Addr.String(), "addr string")
			}
		})
	}
}

// hasUnbracketedIPv6 gates the deterministic rejection of unbracketed IPv6 hosts, independent of the
// urlstrictcolons GODEBUG. url.URL.Hostname/Port is not GODEBUG-gated, so a URL with a raw authority
// that looks like an unbracketed IPv6 (what url.Parse yields under urlstrictcolons=0) is detected
// deterministically here.
func TestHasUnbracketedIPv6(t *testing.T) {
	for _, tc := range []struct {
		host string // url.URL.Host (raw authority)
		want bool
	}{
		{"2001:db8::1:3478", true}, // unbracketed IPv6 with port
		{"[2001:db8::1]:3478", false},
		{"[::1]:3478", false},
		{"[2001:db8::1]", false}, // bracketed, no port
		{"[::1]", false},
		{"1.2.3.4:3478", false},
		{"1.2.3.4", false},
		{"turn.example.com:3478", false},
		{"", false},
	} {
		t.Run(tc.host, func(t *testing.T) {
			assert.Equal(t, tc.want, hasUnbracketedIPv6(&url.URL{Host: tc.host}))
		})
	}
}

func TestURITURNServer(t *testing.T) {
	staticAuth := func(u, p string) *stnrv1.AuthConfig {
		// what a validated block looks like: type normalized, default realm injected
		return &stnrv1.AuthConfig{Type: "static", Realm: stnrv1.DefaultRealm,
			Credentials: map[string]string{"username": u, "password": p}}
	}
	for _, c := range []struct {
		name  string
		uri   string
		want  stnrv1.TURNServer
		proto stnrv1.Protocol
		err   bool
	}{
		{
			name:  "turn-udp with credentials",
			uri:   "turn://user:pass@1.2.3.4:3478?transport=udp",
			want:  stnrv1.TURNServer{Address: "1.2.3.4", Port: 3478, Auth: staticAuth("user", "pass")},
			proto: stnrv1.ProtocolTURNUDP,
		},
		{
			name:  "turn-tcp without credentials",
			uri:   "turn://1.2.3.4:3478?transport=tcp",
			want:  stnrv1.TURNServer{Address: "1.2.3.4", Port: 3478},
			proto: stnrv1.ProtocolTURNTCP,
		},
		{
			name:  "turns maps to TLS and defaults the port",
			uri:   "turns://1.2.3.4?transport=tcp",
			want:  stnrv1.TURNServer{Address: "1.2.3.4", Port: 443},
			proto: stnrv1.ProtocolTURNTLS,
		},
		{
			name:  "bracketed ipv6 round-trips to a bare host",
			uri:   "turn://[2001:db8::1]:3478?transport=udp",
			want:  stnrv1.TURNServer{Address: "2001:db8::1", Port: 3478},
			proto: stnrv1.ProtocolTURNUDP,
		},
		{
			name: "a plain transport URI is not a TURN server",
			uri:  "udp://1.2.3.4:3478",
			err:  true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			u, err := ParseURI(c.uri)
			assert.NoError(t, err, "ParseURI")

			s, proto, err := u.TURNServer()
			if c.err {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, c.want, *s)
			assert.Equal(t, c.proto, proto)

			// a credential-less URI yields a server with no auth block, not an empty one
			if c.want.Auth == nil {
				assert.Nil(t, s.Auth)
			}
		})
	}
}

func TestURITURNServerHostPortRoundTrip(t *testing.T) {
	// an IPv6 URI is bracketed, the server holds the bare host, and HostPort brackets it again
	u, err := ParseURI("turn://[2001:db8::1]:3478?transport=udp")
	assert.NoError(t, err)
	s, _, err := u.TURNServer()
	assert.NoError(t, err)
	assert.Equal(t, "2001:db8::1", s.Address, "bare host")
	assert.Equal(t, "[2001:db8::1]:3478", s.HostPort(), "re-bracketed")
}
