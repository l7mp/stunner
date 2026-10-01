package v2

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradeTURNURI(t *testing.T) {
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
			assert.Equal(t, tc.want, upgradeTURNURI(tc.in))
		})
	}
}

func TestParseURI(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		uri                                string
		proto, address, username, password string
		port                               int
		wantErr                            bool
	}{
		// hierarchical form, udp
		{name: "udp with creds", uri: "turn://user1:passwd1@1.2.3.4:3478?transport=udp",
			proto: "TURN-UDP", address: "1.2.3.4", username: "user1", password: "passwd1",
			port: 3478},
		{name: "udp no creds", uri: "turn://1.2.3.4:3478?transport=udp",
			proto: "TURN-UDP", address: "1.2.3.4", port: 3478},
		{name: "udp default port", uri: "turn://1.2.3.4?transport=udp",
			proto: "TURN-UDP", address: "1.2.3.4", port: 3478},
		{name: "udp default transport", uri: "turn://1.2.3.4:3478",
			proto: "TURN-UDP", address: "1.2.3.4", port: 3478},
		// tcp
		{name: "tcp", uri: "turn://1.2.3.4:3478?transport=tcp",
			proto: "TURN-TCP", address: "1.2.3.4", port: 3478},
		// tls (turns + tcp)
		{name: "tls explicit port", uri: "turns://1.2.3.4:5349?transport=tcp",
			proto: "TURN-TLS", address: "1.2.3.4", port: 5349},
		{name: "tls default port 443", uri: "turns://1.2.3.4?transport=tcp",
			proto: "TURN-TLS", address: "1.2.3.4", port: 443},
		{name: "tls via transport", uri: "turn://1.2.3.4?transport=tls",
			proto: "TURN-TLS", address: "1.2.3.4", port: 443},
		// dtls (transport=dtls, or turns + udp)
		{name: "dtls via transport", uri: "turn://1.2.3.4:3478?transport=dtls",
			proto: "TURN-DTLS", address: "1.2.3.4", port: 3478},
		{name: "dtls via turns+udp default port 443", uri: "turns://1.2.3.4?transport=udp",
			proto: "TURN-DTLS", address: "1.2.3.4", port: 443},

		// RFC 7065 opaque form (single colon after scheme)
		{name: "rfc7065 udp", uri: "turn:1.2.3.4:5555?transport=udp",
			proto: "TURN-UDP", address: "1.2.3.4", port: 5555},
		{name: "rfc7065 tcp", uri: "turn:1.2.3.4:5555?transport=tcp",
			proto: "TURN-TCP", address: "1.2.3.4", port: 5555},
		{name: "rfc7065 tls", uri: "turns:1.2.3.4:5349?transport=tcp",
			proto: "TURN-TLS", address: "1.2.3.4", port: 5349},
		{name: "rfc7065 default port", uri: "turn:1.2.3.4?transport=udp",
			proto: "TURN-UDP", address: "1.2.3.4", port: 3478},

		// plain transport schemes (tunnel client addresses, not TURN)
		{name: "plain udp", uri: "udp://1.2.3.4:5000",
			proto: "UDP", address: "1.2.3.4", port: 5000},
		{name: "plain tcp", uri: "tcp://1.2.3.4:5000",
			proto: "TCP", address: "1.2.3.4", port: 5000},
		{name: "plain udp ipv6", uri: "udp://[2001:db8::1]:5000",
			proto: "UDP", address: "2001:db8::1", port: 5000},

		// IPv6, hierarchical (bracketed)
		{name: "ipv6 udp", uri: "turn://[2001:db8::1]:3478?transport=udp",
			proto: "TURN-UDP", address: "2001:db8::1", port: 3478},
		{name: "ipv6 tcp with creds", uri: "turn://user1:passwd1@[2001:db8::1]:3478?transport=tcp",
			proto: "TURN-TCP", address: "2001:db8::1", username: "user1", password: "passwd1",
			port: 3478},
		{name: "ipv6 loopback default port", uri: "turn://[::1]?transport=udp",
			proto: "TURN-UDP", address: "::1", port: 3478},
		// IPv6, RFC 7065 opaque
		{name: "ipv6 rfc7065", uri: "turn:[2001:db8::1]:5555?transport=tcp",
			proto: "TURN-TCP", address: "2001:db8::1", port: 5555},

		// case-insensitive scheme
		{name: "uppercase scheme hierarchical", uri: "TURN://1.2.3.4:3478?transport=udp",
			proto: "TURN-UDP", address: "1.2.3.4", port: 3478},
		{name: "uppercase scheme opaque", uri: "TURN:1.2.3.4:5555?transport=tcp",
			proto: "TURN-TCP", address: "1.2.3.4", port: 5555},

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
			assert.Equal(t, tc.proto, u.Protocol.String(), "protocol")
			assert.Equal(t, tc.address, u.Host, "address")
			assert.Equal(t, tc.username, u.Username, "username")
			assert.Equal(t, tc.password, u.Password, "password")
			assert.Equal(t, tc.port, u.Port, "port")
		})
	}
}
