package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func noK8s(k8sName) (*stnrv2.StunnerConfig, error) {
	panic("k8s:// server resolution must not be reached")
}

func noPeerK8s(k8sName) (string, error) {
	panic("k8s:// peer resolution must not be reached")
}

// TestParseK8sURI covers the shared k8s:// meta-URI parser of the tunnel-mode server and
// peer arguments.
func TestParseK8sURI(t *testing.T) {
	for _, tc := range []struct {
		name, arg, defaultNS string
		want                 k8sName
		isK8s, wantErr       bool
	}{
		{name: "full form", arg: "k8s://media/gw:l7mp",
			want: k8sName{"media", "gw", "l7mp"}, isK8s: true},
		{name: "default namespace", arg: "k8s:///gw:l7mp", defaultNS: "stunner",
			want: k8sName{"stunner", "gw", "l7mp"}, isK8s: true},
		{name: "port number component", arg: "k8s://media/db:5432",
			want: k8sName{"media", "db", "5432"}, isK8s: true},
		{name: "not a k8s URI", arg: "udp://1.2.3.4:5000"},
		{name: "missing component", arg: "k8s://media/gw", isK8s: true, wantErr: true},
		{name: "no namespace anywhere", arg: "k8s:///gw:l7mp", isK8s: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, isK8s, err := parseK8sURI(tc.arg, tc.defaultNS)
			assert.Equal(t, tc.isK8s, isK8s, "k8s URI detection")
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.isK8s {
				assert.Equal(t, tc.want, u, "parsed URI")
			}
		})
	}
}

// TestTunnelConfig covers the CLI-to-config mapping of tunnel mode: the three turncat-shaped
// positional arguments render one plain (or stdin) listener feeding an L4 server, whose single
// cluster holds the peer and reaches it through a tunnel to the TURN server.
func TestTunnelConfig(t *testing.T) {
	t.Run("udp client with static auth", func(t *testing.T) {
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://user:pass@1.2.3.4:3478",
			"udp://10.0.0.1:9000", "", tunnelOptions{}, noK8s, noPeerK8s)
		require.NoError(t, err)

		assert.Equal(t, stnrv2.ApiVersion, c.ApiVersion, "API version")
		assert.Equal(t, "tunnel", c.Admin.Name, "admin name")
		require.NotNil(t, c.Admin.HealthCheckEndpoint, "health check endpoint")
		assert.Empty(t, *c.Admin.HealthCheckEndpoint, "health check is off")
		assert.Equal(t, "none", c.Auth.Type, "listener-side auth is off")

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
		require.NotNil(t, d.Tunnel.Auth, "tunnel auth")
		assert.Equal(t, "static", d.Tunnel.Auth.Type, "auth type")
		assert.Equal(t, "user", d.Tunnel.Auth.Credentials["username"], "username")
		assert.Equal(t, "pass", d.Tunnel.Auth.Credentials["password"], "password")
	})

	t.Run("bare secret is ephemeral auth", func(t *testing.T) {
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://my-secret@1.2.3.4:3478",
			"udp://10.0.0.1:9000", "", tunnelOptions{}, noK8s, noPeerK8s)
		require.NoError(t, err)
		auth := c.Clusters[0].Tunnel.Auth
		require.NotNil(t, auth, "server auth")
		assert.Equal(t, "ephemeral", auth.Type, "auth type")
		assert.Equal(t, "my-secret", auth.Credentials["secret"], "secret")
	})

	t.Run("no auth dials anonymously", func(t *testing.T) {
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"udp://10.0.0.1:9000", "", tunnelOptions{}, noK8s, noPeerK8s)
		require.NoError(t, err)
		assert.Nil(t, c.Clusters[0].Tunnel.Auth, "no auth block")
	})

	t.Run("tcp client", func(t *testing.T) {
		c, err := tunnelConfig("tcp://0.0.0.0:5001", "turn://1.2.3.4:3478?transport=tcp",
			"udp://10.0.0.1:9000", "", tunnelOptions{}, noK8s, noPeerK8s)
		require.NoError(t, err)
		assert.Equal(t, "TCP", c.Listeners[0].Protocol, "listener protocol")
		assert.Equal(t, "UDP", c.Clusters[0].Protocol, "cluster protocol: the peer's")
		assert.Equal(t, "turn:1.2.3.4:3478?transport=tcp", c.Clusters[0].Tunnel.URL, "tunnel URL")
	})

	t.Run("stdin client", func(t *testing.T) {
		c, err := tunnelConfig("-", "turn://1.2.3.4:3478", "udp://10.0.0.1:9000", "",
			tunnelOptions{}, noK8s, noPeerK8s)
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
			"udp://10.0.0.1:9000", "", tunnelOptions{sni: "turn.example.com", insecure: true}, noK8s, noPeerK8s)
		require.NoError(t, err)
		d := c.Clusters[0]
		assert.Equal(t, "turns:1.2.3.4:5349?transport=tcp", d.Tunnel.URL, "tunnel URL")
		assert.Equal(t, "turn.example.com", d.Tunnel.SNI, "SNI")
		assert.True(t, d.Tunnel.Insecure, "insecure")
	})

	t.Run("license and offload from the command line", func(t *testing.T) {
		lic := &stnrv2.LicenseConfig{Key: "key", HMAC: "hmac"}
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"udp://10.0.0.1:9000", "", tunnelOptions{license: lic, offload: "tc"}, noK8s, noPeerK8s)
		require.NoError(t, err)
		assert.Equal(t, lic, c.Admin.LicenseConfig, "license")
		assert.Equal(t, stnrv2.OffloadEngineTC.String(), c.Admin.OffloadEngine, "offload engine")
		assert.Empty(t, c.Admin.OffloadInterfaces, "offload on every interface")
	})

	t.Run("sni on a non-TLS transport is refused", func(t *testing.T) {
		_, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"udp://10.0.0.1:9000", "", tunnelOptions{sni: "turn.example.com"}, noK8s, noPeerK8s)
		assert.Error(t, err)
	})

	t.Run("dns peer host", func(t *testing.T) {
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"udp://media.example.com:9000", "", tunnelOptions{}, noK8s, noPeerK8s)
		require.NoError(t, err)
		r := c.Clusters[0]
		assert.Equal(t, stnrv2.ClusterTypeStrictDNS.String(), r.Type, "cluster type")
		assert.Equal(t, []string{"media.example.com:9000"}, r.Endpoints, "DNS peer")
	})

	t.Run("invalid client scheme", func(t *testing.T) {
		_, err := tunnelConfig("unix:///tmp/x", "turn://1.2.3.4:3478", "udp://10.0.0.1:9000", "",
			tunnelOptions{}, noK8s, noPeerK8s)
		assert.Error(t, err)
	})

	t.Run("tcp peer", func(t *testing.T) {
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478?transport=tcp",
			"tcp://10.0.0.1:9000", "", tunnelOptions{}, noK8s, noPeerK8s)
		require.NoError(t, err)
		assert.Equal(t, stnrv2.ClusterTypeStatic.String(), c.Clusters[0].Type, "cluster type")
		assert.Equal(t, []string{"10.0.0.1:9000"}, c.Clusters[0].Endpoints, "TCP peer")
		assert.Equal(t, "TCP", c.Clusters[0].Protocol, "a TCP peer makes a TCP cluster")
	})

	t.Run("invalid peer scheme", func(t *testing.T) {
		_, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"dtls://10.0.0.1:9000", "", tunnelOptions{}, noK8s, noPeerK8s)
		assert.Error(t, err)
	})

	t.Run("peer without a transport prefix", func(t *testing.T) {
		_, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"10.0.0.1:9000", "", tunnelOptions{}, noK8s, noPeerK8s)
		assert.Error(t, err)
	})

	t.Run("peer prefix is not a single host", func(t *testing.T) {
		_, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478",
			"udp://10.0.0.0/24:9000", "", tunnelOptions{}, noK8s, noPeerK8s)
		assert.Error(t, err)
	})

	t.Run("non-TURN server URI", func(t *testing.T) {
		_, err := tunnelConfig("udp://127.0.0.1:5000", "udp://1.2.3.4:3478",
			"udp://10.0.0.1:9000", "", tunnelOptions{}, noK8s, noPeerK8s)
		assert.Error(t, err)
	})

	t.Run("k8s peer resolution", func(t *testing.T) {
		peerFromK8s := func(u k8sName) (string, error) {
			assert.Equal(t, k8sName{"media", "db", "postgres"}, u, "parsed peer URI")
			return "tcp://10.96.0.12:5432", nil
		}
		c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478?transport=tcp",
			"k8s://media/db:postgres", "", tunnelOptions{}, noK8s, peerFromK8s)
		require.NoError(t, err)
		assert.Equal(t, []string{"10.96.0.12:5432"}, c.Clusters[0].Endpoints,
			"peer resolved from the service port")
	})

	gateway := func(serverType string) func(k8sName) (*stnrv2.StunnerConfig, error) {
		return func(u k8sName) (*stnrv2.StunnerConfig, error) {
			assert.Equal(t, k8sName{"media", "gw", "l7mp"}, u, "parsed server URI")
			return &stnrv2.StunnerConfig{
				Auth: stnrv2.AuthConfig{Type: "static", Credentials: map[string]string{
					"username": "user", "password": "pass"}},
				Listeners: []stnrv2.ListenerConfig{{
					Name:       "media/gw/l7mp",
					Protocol:   "UDP",
					PublicAddr: "5.6.7.8",
					PublicPort: 3478,
					Servers:    []string{"media/gw/l7mp"},
				}},
				Servers: []stnrv2.ServerConfig{{Name: "media/gw/l7mp", Type: serverType}},
			}, nil
		}
	}

	t.Run("k8s server resolution", func(t *testing.T) {
		c, err := tunnelConfig("udp://127.0.0.1:5000", "k8s://media/gw:l7mp",
			"udp://10.0.0.1:9000", "", tunnelOptions{}, gateway(stnrv2.ServerTypeTURN.String()), noPeerK8s)
		require.NoError(t, err)
		d := c.Clusters[0]
		assert.Equal(t, "turn:5.6.7.8:3478?transport=udp", d.Tunnel.URL,
			"tunnel transport derived from the listener")
		require.NotNil(t, d.Tunnel.Auth, "tunnel auth")
		assert.Equal(t, "static", d.Tunnel.Auth.Type, "auth type carried over")
		assert.Equal(t, "user", d.Tunnel.Auth.Credentials["username"], "username carried over")
	})

	t.Run("k8s server listener must feed a TURN server", func(t *testing.T) {
		_, err := tunnelConfig("udp://127.0.0.1:5000", "k8s://media/gw:l7mp",
			"udp://10.0.0.1:9000", "", tunnelOptions{}, gateway(stnrv2.ServerTypeL4.String()), noPeerK8s)
		assert.Error(t, err)
	})
}
