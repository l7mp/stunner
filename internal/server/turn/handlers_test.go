package turn_test

import (
	"net"
	"testing"

	pion "github.com/pion/turn/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/resolver"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server/turn"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

func TestPermissionHandler(t *testing.T) {
	log := logger.NewLoggerFactory("all:ERROR")
	rt := runtime.New(runtime.Config{Logger: log,
		Resolver: resolver.NewMockResolver(map[string][]string{}, log)})
	conf := &stnrv2.ServerConfig{ServerSpec: stnrv2.ServerSpec{Name: "turn", Type: "turn", Clusters: []string{"media"}}}
	require.NoError(t, conf.Validate())
	s, err := turn.New(conf, rt)
	require.NoError(t, err)
	client := &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1234}
	permits := func(ip string) bool { return s.PermissionHandler(client, net.ParseIP(ip)) }

	// only a matching ALLOW rule grants a permission: no filter matches no peer
	allowAll, err := object.NewFilter(&stnrv2.FilterConfig{Name: "all", Rules: []stnrv2.FilterRule{
		{Action: "ALLOW"}}})
	require.NoError(t, err)
	require.NoError(t, rt.Registry.Add(allowAll, nil))
	c, err := object.NewCluster(&stnrv2.ClusterConfig{Name: "media", Endpoints: []string{"10.1.2.3:5000"},
		Protocol: "UDP"}, rt)
	require.NoError(t, err)
	require.NoError(t, rt.Registry.Add(c, nil))
	assert.False(t, permits("10.1.2.3"), "no filters deny every peer")

	// the filters decide alone: the endpoints of the clusters are no filter
	conf.Filters = []string{"all"}
	require.NoError(t, s.Reconcile(conf))
	assert.True(t, permits("10.1.2.3"), "an endpoint")
	assert.True(t, permits("192.168.0.1"), "not an endpoint: endpoints do not filter")

	// the server's filters decide by IP: a rule with ports matches a permission on any of its
	// ports, the ports are checked on the traffic
	filter, err := object.NewFilter(&stnrv2.FilterConfig{Name: "peers", Rules: []stnrv2.FilterRule{
		{Action: "DENY", IPBlock: &stnrv2.IPBlock{CIDR: "10.1.2.0/24", Except: []string{"10.1.2.3/32"}}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.9.0.0/16"}, Ports: []stnrv2.FilterPort{{Port: 5000, EndPort: 5100}}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.1.0.0/16"}},
		{Action: "DENY", IPBlock: &stnrv2.IPBlock{CIDR: "0.0.0.0/0"}},
	}})
	require.NoError(t, err)
	require.NoError(t, rt.Registry.Add(filter, nil))
	conf.Filters = []string{"peers"}
	require.NoError(t, s.Reconcile(conf))
	assert.True(t, permits("10.1.2.3"), "the exception of the first DENY")
	assert.False(t, permits("10.1.2.4"), "the first DENY")
	assert.True(t, permits("10.9.0.1"), "an ALLOW with ports")
	assert.False(t, permits("192.168.0.1"), "the last DENY")

	// a peer no rule matches is denied, and a config change applies to the next permission
	peers, err := object.NewFilter(&stnrv2.FilterConfig{Name: "peers", Rules: []stnrv2.FilterRule{
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.0.0.0/8"}}}})
	require.NoError(t, err)
	require.NoError(t, rt.Registry.Replace(filter, peers))
	assert.True(t, permits("10.1.2.4"), "a matching ALLOW")
	assert.False(t, permits("192.168.0.1"), "no rule matches")
	conf.Filters = nil
	require.NoError(t, s.Reconcile(conf))
	assert.False(t, permits("10.1.2.4"), "no filters deny every peer")
}

// TestAuthHandlerWithoutCredentials pins that a TURN server whose auth carries no credentials
// refuses every client, an empty username included, instead of admitting the empty one.
func TestAuthHandlerWithoutCredentials(t *testing.T) {
	log := logger.NewLoggerFactory("all:ERROR")
	for _, auth := range []*stnrv2.AuthConfig{
		{Type: "static", Credentials: map[string]string{"username": "user"}},
		{Type: "static"},
		{Type: "ephemeral"},
	} {
		require.NoError(t, auth.Validate(), "missing credentials are no config error")
		auth.Name = stnrv2.DefaultAuthName
		rt := runtime.New(runtime.Config{Logger: log})
		require.NoError(t, rt.Registry.Add(stubObject{typ: runtime.TypeAuth, conf: auth}, nil))
		conf := &stnrv2.ServerConfig{ServerSpec: stnrv2.ServerSpec{Name: "turn", Type: "TURN"},
			TURN: &stnrv2.TURNServerConfig{Auth: auth.Name}}
		require.NoError(t, conf.Validate())
		s, err := turn.New(conf, rt)
		require.NoError(t, err)
		h := s.AuthHandler()
		require.NotNil(t, h, auth.Type)
		for _, user := range []string{"", "user", "1700000000:user"} {
			_, _, ok := h(&pion.RequestAttributes{Username: user, Realm: auth.Realm,
				SrcAddr: &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1234}})
			assert.False(t, ok, "%s auth refuses %q", auth.Type, user)
		}
	}
}
