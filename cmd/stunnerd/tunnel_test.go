package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// TestTunnelConfig covers the CLI-to-config mapping of tunnel mode: the three turncat-shaped
// positional arguments render one plain (or stdin) listener feeding an L4 server, whose single
// cluster holds the peer and reaches it through a tunnel to the TURN server.
func TestTunnelConfig(t *testing.T) {
	t.Run("udp client with static auth", func(t *testing.T) {
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://user:pass@1.2.3.4:3478",
			"udp://10.0.0.1:9000", tunnelOptions{})
		require.NoError(t, err)

		assert.Equal(t, stnrv2.ApiVersion, c.ApiVersion, "API version")
		assert.Equal(t, "tunnel", c.Admin.Name, "admin name")
		require.NotNil(t, c.Admin.HealthCheckEndpoint, "health check endpoint")
		assert.Empty(t, *c.Admin.HealthCheckEndpoint, "health check is off")
		require.Len(t, c.Auths, 1, "the policy the tunnel presents, nothing else")
		assert.Equal(t, "upstream", c.Auths[0].Name)
		assert.Empty(t, c.Servers[0].Auth, "the plain listener authenticates nobody")

		require.Len(t, c.Listeners, 1)
		l := c.Listeners[0]
		assert.Equal(t, "tunnel-listener", l.Name, "listener name")
		assert.Equal(t, "UDP", l.Protocol, "listener protocol")
		assert.Equal(t, "127.0.0.1", l.Addr, "listener address")
		assert.Equal(t, 5000, l.Port, "listener port")
		assert.Equal(t, []string{"tunnel-server"}, l.Servers, "listener server")

		require.Len(t, c.Servers, 1)
		s := c.Servers[0]
		assert.Equal(t, "tunnel-server", s.Name, "server name")
		assert.Equal(t, stnrv2.ServerTypeL4.String(), s.Type, "server type")
		assert.Equal(t, []string{"tunnel-cluster"}, s.Clusters, "server clusters")

		require.Len(t, c.Clusters, 1)
		d := c.Clusters[0]
		assert.Equal(t, "tunnel-cluster", d.Name, "cluster name")
		assert.Equal(t, stnrv2.ClusterTypeStatic.String(), d.Type, "cluster type")
		assert.Equal(t, []string{"10.0.0.1:9000"}, d.Endpoints, "cluster endpoints")
		assert.Equal(t, "UDP", d.Protocol, "cluster protocol: the peer's")
		require.NotNil(t, d.Tunnel, "tunnel")
		assert.Equal(t, "turn:1.2.3.4:3478?transport=udp", d.Tunnel.URL, "tunnel URL")
		assert.Equal(t, "upstream", d.Tunnel.Auth, "the tunnel names the upstream policy")
		auth, err := c.GetAuthConfig("upstream")
		require.NoError(t, err, "upstream policy")
		assert.Equal(t, "static", auth.Type, "auth type")
		assert.Equal(t, "user", auth.Credentials["username"], "username")
		assert.Equal(t, "pass", auth.Credentials["password"], "password")
	})

	t.Run("bare secret is ephemeral auth", func(t *testing.T) {
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://my-secret@1.2.3.4:3478",
			"udp://10.0.0.1:9000", tunnelOptions{})
		require.NoError(t, err)
		auth, err := c.GetAuthConfig(c.Clusters[0].Tunnel.Auth)
		require.NoError(t, err, "upstream policy")
		assert.Equal(t, "ephemeral", auth.Type, "auth type")
		assert.Equal(t, "my-secret", auth.Credentials["secret"], "secret")
	})

	t.Run("no auth dials anonymously", func(t *testing.T) {
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"udp://10.0.0.1:9000", tunnelOptions{})
		require.NoError(t, err)
		assert.Empty(t, c.Clusters[0].Tunnel.Auth, "no policy")
		assert.Empty(t, c.Auths, "no policy")
	})

	t.Run("tcp client", func(t *testing.T) {
		c, err := tunnelConfig("tcp://0.0.0.0:5001", "turn://1.2.3.4:3478?transport=tcp",
			"udp://10.0.0.1:9000", tunnelOptions{})
		require.NoError(t, err)
		assert.Equal(t, "TCP", c.Listeners[0].Protocol, "listener protocol")
		assert.Equal(t, "UDP", c.Clusters[0].Protocol, "cluster protocol: the peer's")
		assert.Equal(t, "turn:1.2.3.4:3478?transport=tcp", c.Clusters[0].Tunnel.URL, "tunnel URL")
	})

	t.Run("stdin client", func(t *testing.T) {
		c, err := tunnelConfig("-", "turn://1.2.3.4:3478", "udp://10.0.0.1:9000",
			tunnelOptions{})
		require.NoError(t, err)
		l := c.Listeners[0]
		assert.Equal(t, "STDIN", l.Protocol, "listener protocol")
		assert.Empty(t, l.Addr, "no listener address")
		assert.Zero(t, l.Port, "no listener port")
		assert.Equal(t, []string{"tunnel-server"}, l.Servers, "listener server")
		assert.Equal(t, []string{"10.0.0.1:9000"}, c.Clusters[0].Endpoints, "peer")
	})

	t.Run("sni and insecure on a TLS transport", func(t *testing.T) {
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:5349?transport=tls",
			"udp://10.0.0.1:9000", tunnelOptions{sni: "turn.example.com", insecure: true})
		require.NoError(t, err)
		d := c.Clusters[0]
		assert.Equal(t, "turns:1.2.3.4:5349?transport=tcp", d.Tunnel.URL, "tunnel URL")
		assert.Equal(t, "turn.example.com", d.Tunnel.SNI, "SNI")
		assert.True(t, d.Tunnel.Insecure, "insecure")
	})

	t.Run("license and offload from the command line", func(t *testing.T) {
		lic := &stnrv2.LicenseConfig{Key: "key", HMAC: "hmac"}
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"udp://10.0.0.1:9000", tunnelOptions{license: lic, offload: "tc"})
		require.NoError(t, err)
		assert.Equal(t, lic, c.Admin.LicenseConfig, "license")
		assert.Equal(t, stnrv2.OffloadEngineTC.String(), c.Admin.OffloadEngine, "offload engine")
		assert.Empty(t, c.Admin.OffloadInterfaces, "offload on every interface")
	})

	t.Run("sni on a non-TLS transport is refused", func(t *testing.T) {
		_, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"udp://10.0.0.1:9000", tunnelOptions{sni: "turn.example.com"})
		assert.Error(t, err)
	})

	t.Run("dns peer host", func(t *testing.T) {
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"udp://media.example.com:9000", tunnelOptions{})
		require.NoError(t, err)
		r := c.Clusters[0]
		assert.Equal(t, stnrv2.ClusterTypeStrictDNS.String(), r.Type, "cluster type")
		assert.Equal(t, []string{"media.example.com:9000"}, r.Endpoints, "DNS peer")
	})

	t.Run("invalid client scheme", func(t *testing.T) {
		_, err := tunnelConfig("unix:///tmp/x", "turn://1.2.3.4:3478", "udp://10.0.0.1:9000",
			tunnelOptions{})
		assert.Error(t, err)
	})

	t.Run("tcp peer", func(t *testing.T) {
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478?transport=tcp",
			"tcp://10.0.0.1:9000", tunnelOptions{})
		require.NoError(t, err)
		assert.Equal(t, stnrv2.ClusterTypeStatic.String(), c.Clusters[0].Type, "cluster type")
		assert.Equal(t, []string{"10.0.0.1:9000"}, c.Clusters[0].Endpoints, "TCP peer")
		assert.Equal(t, "TCP", c.Clusters[0].Protocol, "a TCP peer makes a TCP cluster")
	})

	t.Run("invalid peer scheme", func(t *testing.T) {
		_, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"dtls://10.0.0.1:9000", tunnelOptions{})
		assert.Error(t, err)
	})

	t.Run("peer without a transport prefix", func(t *testing.T) {
		_, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"10.0.0.1:9000", tunnelOptions{})
		assert.Error(t, err)
	})

	t.Run("peer prefix is not a single host", func(t *testing.T) {
		_, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"udp://10.0.0.0/24:9000", tunnelOptions{})
		assert.Error(t, err)
	})

	t.Run("non-TURN server URI", func(t *testing.T) {
		_, err := tunnelConfig("udp://127.0.0.1:5000", "udp://1.2.3.4:3478",
			"udp://10.0.0.1:9000", tunnelOptions{})
		assert.Error(t, err)
	})

	t.Run("k8s:// URIs are not accepted", func(t *testing.T) {
		_, err := tunnelConfig("udp://127.0.0.1:5000", "k8s://media/gw:l7mp",
			"udp://10.0.0.1:9000", tunnelOptions{})
		assert.Error(t, err, "server")
		_, err = tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"k8s://media/db:postgres", tunnelOptions{})
		assert.Error(t, err, "peer")
	})
}
