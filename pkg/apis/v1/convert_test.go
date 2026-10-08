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
				Routes: []string{"media"}},
			{Name: "ns/gw/tls", Protocol: "TURN-TLS", Port: 443, Cert: "c", Key: "k",
				PQCMode: "enforced", Routes: []string{"media"}},
			{Name: "ns/gw/proxy", Protocol: "TURN-UDP", Port: 3479, Routes: []string{"upstream"}},
			{Name: "ns/gw/tcp", Protocol: "TCP", Port: 5000,
				Routes: []string{"media", "rtp", "dns"}},
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
	require.Len(t, v2.Auths, 2, "the v1 auth is the default policy, the tunnel credentials another")
	assert.Equal(t, stnrv2.DefaultAuthName, v2.Auths[0].Name)
	assert.Equal(t, "ephemeral", v2.Auths[0].Type)
	assert.Equal(t, "example.com", v2.Auths[0].Realm)
	assert.Equal(t, "s", v2.Auths[0].Credentials["secret"])

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
	assert.Equal(t, "TURN", s.Type)
	assert.Equal(t, []string{"media"}, s.Clusters)
	s, err = v2.GetServerConfig("ns/gw/tcp")
	require.NoError(t, err)
	assert.Equal(t, "L4", s.Type)

	// a TURN permission is a filter: a TURN cluster keeps the addresses of its endpoints (a
	// prefix is no endpoint) without routing by them, and each TURN server gets a filter of its
	// own name allowing the addresses and prefixes of its clusters' endpoints, IP rules only, and
	// denying the rest
	r, err := v2.GetClusterConfig("media")
	require.NoError(t, err)
	assert.Equal(t, []string{"10.2.3.4", "10.2.3.5", "10.2.3.6"}, r.Endpoints)
	permissions := []stnrv2.FilterRule{
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.1.0.0/16", Except: []string{}},
			Ports: []stnrv2.FilterPort{}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.2.3.4/32", Except: []string{}},
			Ports: []stnrv2.FilterPort{}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.2.3.5/32", Except: []string{}},
			Ports: []stnrv2.FilterPort{}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.2.3.6/32", Except: []string{}},
			Ports: []stnrv2.FilterPort{}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "2001:db8::/32", Except: []string{}},
			Ports: []stnrv2.FilterPort{}},
		{Action: "DENY", Ports: []stnrv2.FilterPort{}},
	}
	for _, name := range []string{"ns/gw/udp", "ns/gw/tls"} {
		s, err := v2.GetServerConfig(name)
		require.NoError(t, err)
		assert.Equal(t, []string{name}, s.TURN.PermissionFilters, name)
		f, err := v2.GetFilterConfig(name)
		require.NoError(t, err)
		assert.Equal(t, permissions, f.Rules, name)
	}
	// the same filter for a TURN-* cluster: it admits every peer
	s, err = v2.GetServerConfig("ns/gw/proxy")
	require.NoError(t, err)
	assert.Equal(t, []string{"ns/gw/proxy"}, s.TURN.PermissionFilters)
	f, err := v2.GetFilterConfig("ns/gw/proxy")
	require.NoError(t, err)
	assert.Equal(t, []stnrv2.FilterRule{
		{Action: "ALLOW", Ports: []stnrv2.FilterPort{}},
		{Action: "DENY", Ports: []stnrv2.FilterPort{}},
	}, f.Rules)

	// the ports of an ORIGINAL_DST cluster's v1 endpoints convert to a filter of the cluster's
	// name, allowing each endpoint on its ports and denying the rest
	assert.Equal(t, []string{"media"}, r.Filters)
	f, err = v2.GetFilterConfig("media")
	require.NoError(t, err)
	assert.Equal(t, []stnrv2.FilterRule{
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.1.0.0/16", Except: []string{}},
			Ports: []stnrv2.FilterPort{}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.2.3.4/32", Except: []string{}},
			Ports: []stnrv2.FilterPort{{Port: 5000, EndPort: 5100}}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.2.3.5/32", Except: []string{}},
			Ports: []stnrv2.FilterPort{{Port: 6000, EndPort: 6000}}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.2.3.6/32", Except: []string{}},
			Ports: []stnrv2.FilterPort{}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "2001:db8::/32", Except: []string{}},
			Ports: []stnrv2.FilterPort{{Port: 7000, EndPort: 7100}}},
		{Action: "DENY", Ports: []stnrv2.FilterPort{}},
	}, f.Rules)
	_, err = v2.GetFilterConfig("rtp")
	assert.Error(t, err, "a ROUND_ROBIN cluster dials its single port: no filter")
	r, err = v2.GetClusterConfig("rtp")
	require.NoError(t, err)
	assert.Equal(t, []string{"10.3.0.1:5000"}, r.Endpoints)
	r, err = v2.GetClusterConfig("dns")
	require.NoError(t, err)
	assert.Equal(t, "STRICT_DNS", r.Type)
	assert.Equal(t, []string{"media.default.svc"}, r.Endpoints)

	// a cluster behind a TURN server sends to the peer, one behind an l4 server load balances, a
	// shared one keeps the first
	for name, want := range map[string]string{"dns": "ROUND_ROBIN", "rtp": "ROUND_ROBIN", "media": "ORIGINAL_DST"} {
		r, err = v2.GetClusterConfig(name)
		require.NoError(t, err)
		assert.Equal(t, want, r.RoutingPolicy, name)
	}

	// a TURN-* cluster routes every peer: it has no endpoints
	r, err = v2.GetClusterConfig("upstream")
	require.NoError(t, err)
	assert.Empty(t, r.Endpoints)

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
	assert.Equal(t, "upstream", d.Tunnel.Auth, "the tunnel names a policy of the credentials")
	tunnelAuth, err := v2.GetAuthConfig(d.Tunnel.Auth)
	require.NoError(t, err)
	assert.Equal(t, "u", tunnelAuth.Credentials["username"])

	// a filter cannot name a domain: a domain behind a TURN listener does not convert
	mixed := v1.DeepCopy()
	mixed.Listeners[0].Routes = []string{"media", "dns"}
	_, err = ConvertToV2(mixed)
	assert.Error(t, err)

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
	assert.Equal(t, stnrv2.ServerConfig{ServerSpec: stnrv2.ServerSpec{Name: "ns/gw/udp", Type: "TURN", Clusters: []string{"media"}}}, s)

	l, s = ConvertListener(ListenerConfig{Name: "plain", Protocol: "TCP", Port: 5000})
	assert.Equal(t, "TCP", l.Protocol)
	assert.Equal(t, "L4", s.Type)
}

func TestConvertCluster(t *testing.T) {
	listeners := []ListenerConfig{
		{Name: "plain", Protocol: "UDP", Addr: "10.0.0.9", Routes: []string{"shared"}},
		{Name: "turn", Protocol: "TURN-UDP", Addrs: []string{"10.0.0.1", "fd00::1"},
			Routes: []string{"media", "shared"}},
	}
	c, f, err := ConvertCluster(ClusterConfig{Name: "media", Endpoints: []string{"10.1.0.0/16",
		"10.9.0.1", "fd00::7"}}, listeners)
	assert.Nil(t, f, "no ports, no filter")
	require.NoError(t, err)
	assert.Equal(t, "ORIGINAL_DST", c.RoutingPolicy, "behind a TURN listener")
	assert.Equal(t, []string{"10.9.0.1", "fd00::7"}, c.Endpoints, "the addresses, not the prefix")

	_, _, err = ConvertCluster(ClusterConfig{Name: "mixed", Endpoints: []string{"10.1.0.0/16",
		"media.svc"}}, listeners)
	assert.Error(t, err, "a filter cannot name a domain")

	// a TURN server whose clusters have single addresses gets the filter too, ending in a DENY:
	// a cluster without endpoints, as the operator renders a backend with no ready pods, admits
	// nothing
	v2, err := ConvertToV2(&StunnerConfig{ApiVersion: ApiVersion, Admin: AdminConfig{Name: "gw"},
		Auth: AuthConfig{Type: "static", Credentials: map[string]string{"username": "u", "password": "p"}},
		Listeners: []ListenerConfig{{Name: "turn", Protocol: "TURN-UDP", Port: 3478,
			Routes: []string{"pods", "empty"}}},
		Clusters: []ClusterConfig{{Name: "pods", Endpoints: []string{"10.0.0.1"}}, {Name: "empty"}}})
	require.NoError(t, err)
	permissions, err := v2.GetFilterConfig("turn")
	require.NoError(t, err)
	require.Len(t, permissions.Rules, 2)
	assert.Equal(t, "10.0.0.1/32", permissions.Rules[0].IPBlock.CIDR)
	assert.Equal(t, stnrv2.FilterRule{Action: "DENY", Ports: []stnrv2.FilterPort{}}, permissions.Rules[1],
		"a DENY of every address, both families")
	assert.Equal(t, []string{"10.0.0.1", "fd00::1"}, c.Addrs)

	c, f, err = ConvertCluster(ClusterConfig{Name: "shared", Endpoints: []string{"10.2.0.1:<5000-5000>"}},
		listeners)
	assert.Nil(t, f, "a ROUND_ROBIN cluster dials its port")
	require.NoError(t, err)
	assert.Equal(t, "ROUND_ROBIN", c.RoutingPolicy, "the first listener routing to it wins")
	assert.Equal(t, []string{"10.2.0.1:5000"}, c.Endpoints)
	assert.Equal(t, []string{"10.0.0.9", "fd00::1"}, c.Addrs, "the first address per family")

	// an address that is no IP is kept as it is, once: it is resolved where the config is loaded
	c, _, err = ConvertCluster(ClusterConfig{Name: "env"}, []ListenerConfig{
		{Name: "a", Protocol: "TURN-UDP", Addr: "$STUNNER_ADDR", Routes: []string{"env"}},
		{Name: "b", Protocol: "TURN-TCP", Addr: "$STUNNER_ADDR", Addrs: []string{"$STUNNER_ADDRS"},
			Routes: []string{"env"}},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"$STUNNER_ADDR", "$STUNNER_ADDRS"}, c.Addrs)

	c, _, err = ConvertCluster(ClusterConfig{Name: "orphan"}, listeners)
	require.NoError(t, err)
	assert.Empty(t, c.RoutingPolicy, "no listener routes to it: the v2 default applies")
	assert.Empty(t, c.Addrs)

	_, _, err = ConvertCluster(ClusterConfig{Name: "bad", Endpoints: []string{"10.0.0.1:<70000-70000>"}}, nil)
	assert.Error(t, err)
}
