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
		Resolver: resolver.NewMockResolver(map[string][]string{"media.svc": {"10.9.0.1"}}, log)})
	conf := &stnrv2.ServerConfig{Name: "turn", Type: "turn", Clusters: []string{"addr", "prefix", "dns"}}
	require.NoError(t, conf.Validate())
	require.NoError(t, rt.Registry.Add(stubObject{typ: runtime.TypeServer, conf: conf}, nil))
	s := turn.NewServer("turn", rt)
	client := &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1234}
	permits := func(ip string) bool { return s.PermissionHandler(client, net.ParseIP(ip)) }

	assert.False(t, permits("10.1.2.3"), "no clusters admit nothing")

	clusters := map[string]*object.Cluster{}
	for _, cc := range []*stnrv2.ClusterConfig{
		{Name: "addr", Endpoints: []string{"10.1.2.3:5000"}, Protocol: "UDP"},
		{Name: "prefix", Endpoints: []string{"10.2.0.0/16:6000-6100", "2001:db8::/32"}, Protocol: "UDP"},
		{Name: "dns", Type: "STRICT_DNS", Endpoints: []string{"media.svc:7000"}, Protocol: "UDP"},
	} {
		c, err := object.NewCluster(cc, rt)
		require.NoError(t, err)
		require.NoError(t, rt.Registry.Add(c, nil))
		clusters[cc.Name] = c.(*object.Cluster)
	}
	assert.True(t, permits("10.1.2.3"), "an address endpoint, its port not enforced")
	assert.False(t, permits("10.1.2.4"), "an address endpoint admits that address only")
	assert.True(t, permits("10.2.9.9"), "a prefix endpoint, its port range not enforced")
	assert.True(t, permits("2001:db8::1"), "an IPv6 prefix endpoint")
	assert.True(t, permits("10.9.0.1"), "a domain endpoint admits what the domain resolves to")
	assert.False(t, permits("10.9.0.2"), "a domain endpoint admits nothing else")
	assert.False(t, permits("192.168.0.1"), "no endpoint contains the peer")

	// a cluster change applies to the next permission
	require.NoError(t, clusters["addr"].Reconcile(&stnrv2.ClusterConfig{Name: "addr",
		Endpoints: []string{"192.168.0.0/16"}, Protocol: "UDP"}))
	assert.False(t, permits("10.1.2.3"), "the old endpoints are gone")
	assert.True(t, permits("192.168.0.1"), "the new endpoints admit")
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
		rt := runtime.New(runtime.Config{Logger: log})
		require.NoError(t, rt.Registry.Add(stubObject{typ: runtime.TypeAuth, conf: auth}, nil))
		h := turn.NewAuthHandler(rt, log.NewLogger("auth"))
		require.NotNil(t, h, auth.Type)
		for _, user := range []string{"", "user", "1700000000:user"} {
			_, _, ok := h(&pion.RequestAttributes{Username: user, Realm: auth.Realm,
				SrcAddr: &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1234}})
			assert.False(t, ok, "%s auth refuses %q", auth.Type, user)
		}
	}
}
