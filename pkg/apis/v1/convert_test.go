package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func TestConvertToV2(t *testing.T) {
	quota := 5
	v1 := &StunnerConfig{
		ApiVersion: ApiVersion,
		Admin: AdminConfig{
			Name:          "gw",
			UserQuota:     quota,
			LicenseConfig: &LicenseConfig{Key: "key", HMAC: "hmac"},
		},
		Auth: AuthConfig{
			Type:        "ephemeral",
			Realm:       "example.com",
			Credentials: map[string]string{"secret": "s"},
		},
		Listeners: []ListenerConfig{
			{Name: "ns/gw/udp", Protocol: "TURN-UDP", Addr: "10.0.0.1", Port: 3478,
				Addrs: []string{"10.0.0.1", "fd00::1"}, PublicAddr: "1.2.3.4", PublicPort: 3478,
				Routes: []string{"media", "dns"}},
			{Name: "ns/gw/tls", Protocol: "TURN-TLS", Port: 443, Cert: "c", Key: "k",
				PQCMode: "enforced", Routes: []string{"media"}},
			{Name: "ns/gw/proxy", Protocol: "TURN-UDP", Port: 3479, Routes: []string{"upstream"}},
			{Name: "ns/gw/tcp", Protocol: "TCP", Port: 5000,
				Routes: []string{"media", "rtp"}},
		},
		Clusters: []ClusterConfig{
			{Name: "media", Endpoints: []string{"10.1.0.0/16", "10.2.3.4:<5000-5100>",
				"10.2.3.5:<6000-6000>", "10.2.3.6:<1-65535>", "2001:db8::/32:<7000-7100>"}},
			{Name: "dns", Type: "STRICT_DNS", Endpoints: []string{"media.default.svc"}},
			{Name: "rtp", Protocol: "TCP", Endpoints: []string{"10.3.0.1:<5000-5000>"}},
			{Name: "upstream", Protocol: "TURN-TCP",
				TURNServer: &TURNServer{Address: "turn.example.com", Port: 3478,
					Auth: &AuthConfig{Type: "static",
						Credentials: map[string]string{"username": "u", "password": "p"}}}},
		},
	}

	v2, err := ConvertToV2(v1)
	require.NoError(t, err)
	require.NoError(t, v2.Validate())
	assert.Equal(t, stnrv2.ApiVersion, v2.ApiVersion)

	// the input is left alone
	assert.Equal(t, "TURN-UDP", v1.Listeners[0].Protocol)

	// admin and auth carry over
	assert.Equal(t, "gw", v2.Admin.Name)
	assert.Equal(t, quota, v2.Admin.UserQuota)
	require.NotNil(t, v2.Admin.LicenseConfig)
	assert.Equal(t, "key", v2.Admin.LicenseConfig.Key)
	assert.Equal(t, "ephemeral", v2.Auth.Type)
	assert.Equal(t, "example.com", v2.Auth.Realm)
	assert.Equal(t, "s", v2.Auth.Credentials["secret"])

	// listeners keep their port and feed a server of their own name; a v1 address is a relay
	// address, so the listener binds every address
	l, err := v2.GetListenerConfig("ns/gw/udp")
	require.NoError(t, err)
	assert.Equal(t, "UDP", l.Protocol)
	assert.Equal(t, []string{"ns/gw/udp"}, l.Servers)
	assert.Equal(t, "", l.Addr)
	assert.Equal(t, 3478, l.Port)
	assert.Equal(t, "1.2.3.4", l.PublicAddr)
	l, err = v2.GetListenerConfig("ns/gw/tls")
	require.NoError(t, err)
	assert.Equal(t, "TLS", l.Protocol)
	assert.Equal(t, "enforced", l.PQCMode)

	// TURN-* listeners get TURN servers, the others L4 servers; the clusters carry over in order
	s, err := v2.GetServerConfig("ns/gw/udp")
	require.NoError(t, err)
	assert.Equal(t, "turn", s.Type)
	assert.Equal(t, []string{"dns", "media"}, s.Clusters, "v1 validation sorts the clusters")
	s, err = v2.GetServerConfig("ns/gw/tcp")
	require.NoError(t, err)
	assert.Equal(t, "l4", s.Type)

	// v1 cluster endpoints convert to the v2 syntax
	r, err := v2.GetClusterConfig("media")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"10.1.0.0/16", "10.2.3.4:5000-5100", "10.2.3.5:6000",
		"10.2.3.6", "[2001:db8::/32]:7000-7100"}, r.Endpoints)
	r, err = v2.GetClusterConfig("dns")
	require.NoError(t, err)
	assert.Equal(t, "STRICT_DNS", r.Type)
	assert.Equal(t, []string{"media.default.svc"}, r.Endpoints)

	// a cluster behind a TURN server filters, one behind an l4 server load balances, a shared one
	// keeps the first
	for name, want := range map[string]string{"dns": "FILTER", "rtp": "ROUND_ROBIN", "media": "FILTER"} {
		r, err = v2.GetClusterConfig(name)
		require.NoError(t, err)
		assert.Equal(t, want, r.RoutingPolicy, name)
	}

	// a TURN-* cluster admits every peer: its endpoints are the catch-all prefixes
	r, err = v2.GetClusterConfig("upstream")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"0.0.0.0/0", "::/0"}, r.Endpoints)

	// a cluster relays at the addresses of the listeners routing to it, the first per family
	d, err := v2.GetClusterConfig("media")
	require.NoError(t, err)
	assert.Equal(t, "UDP", d.Protocol)
	assert.Nil(t, d.Tunnel)
	assert.Equal(t, []string{"10.0.0.1", "fd00::1"}, d.Addrs)

	// a TURN-* cluster becomes a UDP cluster tunnelled through the cluster's TURN server
	d, err = v2.GetClusterConfig("upstream")
	require.NoError(t, err)
	assert.Equal(t, "UDP", d.Protocol)
	require.NotNil(t, d.Tunnel)
	assert.Equal(t, "turn:turn.example.com:3478?transport=tcp", d.Tunnel.URL)
	require.NotNil(t, d.Tunnel.Auth)
	assert.Equal(t, "u", d.Tunnel.Auth.Credentials["username"])

	// an invalid v1 config does not convert
	v1.ApiVersion = "v0"
	_, err = ConvertToV2(v1)
	assert.Error(t, err)
}

func TestConvertListener(t *testing.T) {
	l, s := ConvertListener(ListenerConfig{Name: "ns/gw/udp", Protocol: "TURN-UDP", Addr: "10.0.0.1",
		Port: 3478, PublicAddr: "1.2.3.4", Routes: []string{"media"}})
	assert.Equal(t, stnrv2.ListenerConfig{Name: "ns/gw/udp", Protocol: "UDP", Port: 3478,
		PublicAddr: "1.2.3.4", Servers: []string{"ns/gw/udp"}}, l, "the address moves to the clusters")
	assert.Equal(t, stnrv2.ServerConfig{Name: "ns/gw/udp", Type: "turn", Clusters: []string{"media"}}, s)

	l, s = ConvertListener(ListenerConfig{Name: "plain", Protocol: "TCP", Port: 5000})
	assert.Equal(t, "TCP", l.Protocol)
	assert.Equal(t, "l4", s.Type)
}

func TestConvertCluster(t *testing.T) {
	listeners := []ListenerConfig{
		{Name: "plain", Protocol: "UDP", Addr: "10.0.0.9", Routes: []string{"shared"}},
		{Name: "turn", Protocol: "TURN-UDP", Addrs: []string{"10.0.0.1", "fd00::1"},
			Routes: []string{"media", "shared"}},
	}
	c, err := ConvertCluster(ClusterConfig{Name: "media", Endpoints: []string{"10.1.0.0/16"}},
		listeners)
	require.NoError(t, err)
	assert.Equal(t, "FILTER", c.RoutingPolicy, "behind a TURN listener")
	assert.Equal(t, []string{"10.0.0.1", "fd00::1"}, c.Addrs)

	c, err = ConvertCluster(ClusterConfig{Name: "shared", Endpoints: []string{"10.2.0.1:<5000-5000>"}},
		listeners)
	require.NoError(t, err)
	assert.Equal(t, "ROUND_ROBIN", c.RoutingPolicy, "the first listener routing to it wins")
	assert.Equal(t, []string{"10.2.0.1:5000"}, c.Endpoints)
	assert.Equal(t, []string{"10.0.0.9", "fd00::1"}, c.Addrs, "the first address per family")

	c, err = ConvertCluster(ClusterConfig{Name: "orphan"}, listeners)
	require.NoError(t, err)
	assert.Empty(t, c.RoutingPolicy, "no listener routes to it: the v2 default applies")
	assert.Empty(t, c.Addrs)

	_, err = ConvertCluster(ClusterConfig{Name: "bad", Endpoints: []string{"10.0.0.1:<70000-70000>"}}, nil)
	assert.Error(t, err)
}
