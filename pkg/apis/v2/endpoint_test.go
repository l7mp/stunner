package v2

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEndpoint(t *testing.T) {
	for _, tc := range []struct {
		ep             string
		prefix, domain string
		port, endPort  int
		hostPort       string // "" when not dialable
	}{
		{ep: "10.1.0.0/16", prefix: "10.1.0.0/16"},
		{ep: "10.1.0.7", prefix: "10.1.0.7/32"},
		{ep: "10.1.0.7:5000", prefix: "10.1.0.7/32", port: 5000, endPort: 5000, hostPort: "10.1.0.7:5000"},
		{ep: "10.2.3.4:5000-5100", prefix: "10.2.3.4/32", port: 5000, endPort: 5100},
		{ep: "10.1.0.0/16:5000", prefix: "10.1.0.0/16", port: 5000, endPort: 5000},
		{ep: "2001:db8::7", prefix: "2001:db8::7/128"},
		{ep: "[2001:db8::7]", prefix: "2001:db8::7/128"},
		{ep: "2001:db8::/32", prefix: "2001:db8::/32"},
		{ep: "::/0", prefix: "::/0"},
		{ep: "[2001:db8::7]:5000", prefix: "2001:db8::7/128", port: 5000, endPort: 5000, hostPort: "[2001:db8::7]:5000"},
		{ep: "[2001:db8::/32]:5000-6000", prefix: "2001:db8::/32", port: 5000, endPort: 6000},
		{ep: "media.default.svc", domain: "media.default.svc"},
		{ep: "media.default.svc:5000", domain: "media.default.svc", port: 5000, endPort: 5000, hostPort: "media.default.svc:5000"},
	} {
		t.Run(tc.ep, func(t *testing.T) {
			e, err := ParseEndpoint(tc.ep)
			require.NoError(t, err)
			if tc.prefix != "" {
				require.NotNil(t, e.Prefix)
				assert.Equal(t, tc.prefix, e.Prefix.String())
			} else {
				assert.Nil(t, e.Prefix)
			}
			assert.Equal(t, tc.domain, e.Domain)
			assert.Equal(t, tc.port, e.Port)
			assert.Equal(t, tc.endPort, e.EndPort)
			hostPort, ok := e.HostPort()
			assert.Equal(t, tc.hostPort, hostPort)
			assert.Equal(t, tc.hostPort != "", ok)

			// the string form parses back to the same endpoint
			again, err := ParseEndpoint(e.String())
			require.NoError(t, err)
			assert.Equal(t, e, again)
		})
	}

	for _, ep := range []string{"", "10.1.0.7:0", "10.1.0.7:70000", "10.1.0.7:x",
		"10.1.0.7:6000-5000", "10.1.0.0/40", "[]:5000", "tcp://10.1.0.7:22"} {
		_, err := ParseEndpoint(ep)
		assert.Error(t, err, "endpoint %q", ep)
	}
}

func TestEndpointContains(t *testing.T) {
	e, err := ParseEndpoint("10.1.0.0/16:5000-5100")
	require.NoError(t, err)
	ip := net.ParseIP("10.1.2.3")
	assert.True(t, e.Contains(ip), "the port range is not enforced")
	assert.False(t, e.Contains(net.ParseIP("10.2.0.1")))

	catchAll, err := ParseEndpoint("::/0")
	require.NoError(t, err)
	assert.True(t, catchAll.Contains(net.ParseIP("2001:db8::1")))

	domain, err := ParseEndpoint("media.default.svc:5000")
	require.NoError(t, err)
	assert.False(t, domain.Contains(ip), "a domain endpoint matches only through resolution")
}

func TestEndpointRestrictsPort(t *testing.T) {
	for ep, restricts := range map[string]bool{
		"10.1.0.7":           false,
		"10.1.0.7:1-65535":   false,
		"10.1.0.7:5000":      true,
		"10.1.0.7:5000-5100": true,
		"10.1.0.7:1-5000":    true,
		"media.svc:5000":     true,
		"10.1.0.0/16":        false,
	} {
		e, err := ParseEndpoint(ep)
		require.NoError(t, err)
		assert.Equal(t, restricts, e.RestrictsPort(), ep)
	}
}
