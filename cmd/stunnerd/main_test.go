package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// TestTunnelConfig covers the mapping of the tunnel-mode arguments to a config: a plain listener
// feeding an L4 server, whose cluster reaches the peer through a tunnel presenting the "upstream"
// policy.
func TestTunnelConfig(t *testing.T) {
	tunnelConfig := func(clientArg, serverArg, peerArg, sni string, insecure bool) (*stnrv2.StunnerConfig, error) {
		server, err := stnrv2.ParseURI(serverArg)
		if err != nil {
			return nil, err
		}
		peerProto, peer, err := resolvePeer(peerArg, nil)
		if err != nil {
			return nil, err
		}
		return tunnelConfig(clientArg, server, peerProto, peer, sni, insecure)
	}

	c, err := tunnelConfig("udp://127.0.0.1:5000", "turn://user:pass@1.2.3.4:3478",
		"tcp://10.0.0.1:9000", "", false)
	require.NoError(t, err)
	assert.Equal(t, "UDP", c.Listeners[0].Protocol)
	assert.Equal(t, 5000, c.Listeners[0].Port)
	assert.Equal(t, stnrv2.ServerTypeL4.String(), c.Servers[0].Type)
	cl := c.Clusters[0]
	assert.Equal(t, "TCP", cl.Protocol, "the peer's transport")
	assert.Equal(t, []string{"10.0.0.1:9000"}, cl.Endpoints)
	assert.Equal(t, "turn:1.2.3.4:3478?transport=udp", cl.Tunnel.URL, "no credentials in the URL")
	auth, err := c.GetAuthConfig(cl.Tunnel.Auth)
	require.NoError(t, err)
	assert.Equal(t, "upstream", auth.Name)
	assert.Equal(t, "static", auth.Type)

	c, err = tunnelConfig("-", "turn://my-secret@1.2.3.4:3478", "udp://media.example.com:9000", "", false)
	require.NoError(t, err)
	assert.Equal(t, "STDIN", c.Listeners[0].Protocol)
	assert.Equal(t, stnrv2.ClusterTypeStrictDNS.String(), c.Clusters[0].Type, "a DNS peer")
	auth, err = c.GetAuthConfig(c.Clusters[0].Tunnel.Auth)
	require.NoError(t, err)
	assert.Equal(t, "ephemeral", auth.Type, "a bare secret")

	for _, args := range [][3]string{
		{"unix:///tmp/x", "turn://1.2.3.4:3478", "udp://10.0.0.1:9000"},
		{"udp://127.0.0.1:5000", "udp://1.2.3.4:3478", "udp://10.0.0.1:9000"},
		{"udp://127.0.0.1:5000", "turn://1.2.3.4:3478", "10.0.0.1:9000"},
	} {
		_, err := tunnelConfig(args[0], args[1], args[2], "", false)
		assert.Error(t, err, "%v", args)
	}
	_, err = tunnelConfig("udp://127.0.0.1:5000", "turn://1.2.3.4:3478", "udp://10.0.0.1:9000",
		"turn.example.com", false)
	assert.Error(t, err, "--sni on a non-TLS transport")
}

// TestParseK8sURI pins the k8s:// meta-URI parser of the tunnel mode.
func TestParseK8sURI(t *testing.T) {
	flags := cliopt.NewConfigFlags(false)
	ns := "media"
	flags.Namespace = &ns

	u, ok, err := parseK8sURI("k8s://stunner/udp-gateway:udp-listener", flags)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, k8sName{Namespace: "stunner", Name: "udp-gateway", Component: "udp-listener"}, u)

	u, ok, err = parseK8sURI("k8s:///media-server:5001", flags)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "media", u.Namespace, "the kubeconfig namespace")

	_, ok, err = parseK8sURI("turn://1.2.3.4:3478", flags)
	assert.NoError(t, err)
	assert.False(t, ok, "not a k8s:// URI")

	_, ok, err = parseK8sURI("k8s://stunner/udp-gateway", flags)
	assert.True(t, ok)
	assert.Error(t, err, "no component")
}
