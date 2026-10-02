package v2

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validConfig returns the config of the architecture's worked example: one TURN server behind a
// UDP and a TLS listener, one L4 server tunnelling through an upstream TURN server.
func validConfig() *StunnerConfig {
	return &StunnerConfig{
		ApiVersion: ApiVersion,
		Auth: AuthConfig{
			Type:        "static",
			Credentials: map[string]string{"username": "user", "password": "pass"},
		},
		Listeners: []ListenerConfig{
			{Name: "turn-udp", Protocol: "UDP", Port: 3478, Servers: []string{"media-gw"}},
			{Name: "turn-tls", Protocol: "TLS", Port: 443, Servers: []string{"media-gw"}, Cert: "cert",
				Key: "key", PQCMode: "preferred"},
			{Name: "tunnel", Protocol: "UDP", Port: 10000, Servers: []string{"rtp-tunnel"}},
		},
		Servers: []ServerConfig{
			{Name: "media-gw", Type: "turn", Clusters: []string{"media-pods"}},
			{Name: "rtp-tunnel", Type: "l4", Clusters: []string{"media-lb"}},
		},
		Clusters: []ClusterConfig{{
			Name:      "media-pods",
			Endpoints: []string{"10.1.0.0/16", "10.2.3.4:5000-5100"},
			Protocol:  "UDP",
			Addrs:     []string{"10.0.0.1,fd00::1"},
		}, {
			Name:      "media-lb",
			Endpoints: []string{"10.1.0.7:5000", "10.1.0.8:5000"},
			Protocol:  "UDP",
			Tunnel:    &TunnelConfig{URL: "turn:turn.example.com:3478?transport=tcp"},
		}},
	}
}

func TestStunnerConfigValidate(t *testing.T) {
	c := validConfig()
	require.NoError(t, c.Validate())

	// defaults are injected, addresses normalized
	assert.Equal(t, "", c.Listeners[0].Addr, "a listener binds every address by default")
	assert.Equal(t, "STATIC", c.Clusters[0].Type)
	assert.Equal(t, "FILTER", c.Clusters[0].RoutingPolicy)
	assert.Equal(t, []string{"10.0.0.1", "fd00::1"}, c.Clusters[0].Addrs)
	assert.Equal(t, DefaultStunnerName, c.Admin.Name)

	// a copy is equal, and survives a JSON round trip
	cp := c.DeepCopy()
	assert.True(t, c.DeepEqual(cp))
	raw, err := json.Marshal(c)
	require.NoError(t, err)
	back := &StunnerConfig{}
	require.NoError(t, json.Unmarshal(raw, back))
	require.NoError(t, back.Validate())
	assert.True(t, c.DeepEqual(back))

	// server cluster order is kept: it decides attribution
	c.Servers[0].Clusters = []string{"b", "a"}
	require.NoError(t, c.Validate())
	assert.Equal(t, []string{"b", "a"}, c.Servers[0].Clusters)

	// listener server order is kept: the listener feeds the first one
	c.Listeners[0].Servers = []string{"rtp-tunnel", "media-gw"}
	require.NoError(t, c.Validate())
	assert.Equal(t, []string{"rtp-tunnel", "media-gw"}, c.Listeners[0].Servers)
	assert.Equal(t, "rtp-tunnel", c.Listeners[0].FirstServer())
	c.Listeners[0].Servers = nil
	require.NoError(t, c.Validate())
	assert.Equal(t, "", c.Listeners[0].FirstServer())
}

func TestStunnerConfigValidateRejects(t *testing.T) {
	// only what does not parse into the config structs is rejected
	for _, tc := range []struct {
		name   string
		mutate func(c *StunnerConfig)
	}{
		{"bad version", func(c *StunnerConfig) { c.ApiVersion = "v1" }},
		{"TURN listener protocol", func(c *StunnerConfig) { c.Listeners[0].Protocol = "TURN-UDP" }},
		{"invalid port", func(c *StunnerConfig) { c.Listeners[0].Port = 70000 }},
		{"unknown PQC mode", func(c *StunnerConfig) { c.Listeners[1].PQCMode = "sometimes" }},
		{"duplicate listener", func(c *StunnerConfig) { c.Listeners[2].Name = "turn-udp" }},
		{"duplicate cluster", func(c *StunnerConfig) { c.Clusters[1].Name = "media-pods" }},
		{"unknown server type", func(c *StunnerConfig) { c.Servers[0].Type = "sip" }},
		{"invalid endpoint", func(c *StunnerConfig) { c.Clusters[0].Endpoints = []string{"10.0.0.1:0"} }},
		{"missing cluster protocol", func(c *StunnerConfig) { c.Clusters[0].Protocol = "" }},
		{"unknown cluster protocol", func(c *StunnerConfig) { c.Clusters[0].Protocol = "SCTP" }},
		{"TURN cluster protocol", func(c *StunnerConfig) { c.Clusters[0].Protocol = "TURN-UDP" }},
		{"tunnel URL not a TURN URL", func(c *StunnerConfig) {
			c.Clusters[1].Tunnel.URL = "udp://turn.example.com:3478"
		}},
		{"unknown routing policy", func(c *StunnerConfig) { c.Clusters[0].RoutingPolicy = "RANDOM" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig()
			tc.mutate(c)
			assert.Error(t, c.Validate())
		})
	}
}

func TestStunnerConfigValidateAccepts(t *testing.T) {
	// what connects to what is never rejected: it runs, and fails or falls back at runtime
	for _, tc := range []struct {
		name   string
		mutate func(c *StunnerConfig)
	}{
		{"listener without a server", func(c *StunnerConfig) { c.Listeners[0].Servers = nil }},
		{"TLS without a cert", func(c *StunnerConfig) { c.Listeners[1].Cert = "" }},
		{"PQC mode on UDP", func(c *StunnerConfig) { c.Listeners[0].PQCMode = "enforced" }},
		{"domain in a STATIC cluster", func(c *StunnerConfig) {
			c.Clusters[0].Endpoints = []string{"media.default.svc"}
		}},
		{"IP in a STRICT_DNS cluster", func(c *StunnerConfig) { c.Clusters[0].Type = "STRICT_DNS" }},
		{"TCP cluster tunnelled over UDP", func(c *StunnerConfig) {
			c.Clusters[1].Protocol = "TCP"
			c.Clusters[1].Tunnel.URL = "turn:turn.example.com:3478?transport=udp"
		}},
		{"load-balancing cluster on a TURN server", func(c *StunnerConfig) {
			c.Clusters[0].RoutingPolicy = "ROUND_ROBIN"
		}},
		{"STDIN listener on a TURN server", func(c *StunnerConfig) {
			c.Listeners[0] = ListenerConfig{Name: "stdin", Protocol: "STDIN", Servers: []string{"media-gw"}}
		}},
		{"config skew", func(c *StunnerConfig) {
			c.Listeners[0].Servers = []string{"no-such-server"}
			c.Servers[0].Clusters = []string{"no-such-cluster"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig()
			tc.mutate(c)
			assert.NoError(t, c.Validate())
		})
	}
}

// TestDeepCopyIsExact pins the copy contract: a copy equals its original, nil and empty alike, and
// shares nothing with it.
func TestDeepCopyIsExact(t *testing.T) {
	hc, lic := "http://:8086", &LicenseConfig{Key: "k", HMAC: "h"}
	full := validConfig()
	full.Admin.HealthCheckEndpoint, full.Admin.LicenseConfig = &hc, lic
	validated := validConfig()
	require.NoError(t, validated.Validate())

	for name, c := range map[string]*StunnerConfig{"zero": {}, "validated": validated, "full": full} {
		cp := c.DeepCopy()
		assert.True(t, reflect.DeepEqual(c, cp), "%s: the copy equals the original", name)

		orig := c.DeepCopy()
		if cp.Admin.HealthCheckEndpoint != nil {
			*cp.Admin.HealthCheckEndpoint = "changed"
		}
		if cp.Admin.LicenseConfig != nil {
			cp.Admin.LicenseConfig.Key = "changed"
		}
		for k := range cp.Auth.Credentials {
			cp.Auth.Credentials[k] = "changed"
		}
		for i := range cp.Clusters {
			for j := range cp.Clusters[i].Endpoints {
				cp.Clusters[i].Endpoints[j] = "changed"
			}
			if cp.Clusters[i].Tunnel != nil {
				cp.Clusters[i].Tunnel.URL = "changed"
			}
		}
		assert.True(t, reflect.DeepEqual(c, orig), "%s: the copy shares nothing with the original", name)
	}
}

// TestValidateIsIdempotent pins that validating a validated config changes nothing: comparing two
// validated configs is how every config change is detected.
func TestValidateIsIdempotent(t *testing.T) {
	hc, lic := "http://:8086", &LicenseConfig{Key: "k", HMAC: "h"}
	full := validConfig()
	full.Admin.HealthCheckEndpoint, full.Admin.LicenseConfig = &hc, lic

	minimal := &StunnerConfig{ApiVersion: ApiVersion} // everything else defaulted
	for name, c := range map[string]*StunnerConfig{"minimal": minimal, "valid": validConfig(), "full": full} {
		require.NoError(t, c.Validate(), name)
		once := c.DeepCopy()
		require.NoError(t, c.Validate(), name)
		assert.True(t, reflect.DeepEqual(once, c), "%s: a second validation changes nothing", name)
	}
}
