package v2

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEndpoint(t *testing.T) {
	for _, tc := range []struct {
		ep           string
		addr, domain string
		port         int
		hostPort     string // "" when not dialable
	}{
		{ep: "10.1.0.7", addr: "10.1.0.7"},
		{ep: "10.1.0.7:5000", addr: "10.1.0.7", port: 5000, hostPort: "10.1.0.7:5000"},
		{ep: "2001:db8::7", addr: "2001:db8::7"},
		{ep: "[2001:db8::7]", addr: "2001:db8::7"},
		{ep: "[2001:db8::7]:5000", addr: "2001:db8::7", port: 5000, hostPort: "[2001:db8::7]:5000"},
		{ep: "::ffff:10.1.0.7", addr: "10.1.0.7"},
		{ep: "media.default.svc", domain: "media.default.svc"},
		{ep: "media.default.svc:5000", domain: "media.default.svc", port: 5000, hostPort: "media.default.svc:5000"},
	} {
		t.Run(tc.ep, func(t *testing.T) {
			e, err := ParseEndpoint(tc.ep)
			require.NoError(t, err)
			if tc.addr != "" {
				require.True(t, e.Addr.IsValid())
				assert.Equal(t, tc.addr, e.Addr.String())
			} else {
				assert.False(t, e.Addr.IsValid())
			}
			assert.Equal(t, tc.domain, e.Domain)
			assert.Equal(t, tc.port, e.Port)
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
		"10.1.0.7:6000-5000", "10.1.0.7:5000-5100", "10.1.0.0/16", "10.1.0.0/16:5000", "::/0",
		"0.0.0.0/0", "[]:5000", "tcp://10.1.0.7:22"} {
		_, err := ParseEndpoint(ep)
		assert.Error(t, err, "endpoint %q", ep)
	}
}
