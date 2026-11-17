package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cliopt "k8s.io/cli-runtime/pkg/genericclioptions"
)

// TestAddresses pins the k8s:// address syntax: the namespace defaults to the kubeconfig context,
// and a malformed address fails before the cluster is contacted.
func TestAddresses(t *testing.T) {
	flags := cliopt.NewConfigFlags(false)
	ns := "media"
	flags.Namespace = &ns
	r := Resolver{K8s: flags}

	got, path, err := r.split("k8s://stunner/udp-gateway/udp-listener")
	require.NoError(t, err)
	assert.Equal(t, "stunner", got)
	assert.Equal(t, []string{"udp-gateway", "udp-listener"}, path)

	got, path, err = r.split("k8s:///media-server:5001")
	require.NoError(t, err)
	assert.Equal(t, "media", got, "the kubeconfig namespace")
	assert.Equal(t, []string{"media-server:5001"}, path)

	for _, addr := range []string{"k8s://stunner", "k8s://stunner/", "k8s://stunner/gw/l/x"} {
		_, err := r.Listener(context.Background(), addr)
		assert.ErrorContains(t, err, "invalid Gateway address", addr)
	}
	for _, addr := range []string{"k8s://stunner/svc", "k8s://stunner/svc:", "k8s://stunner/:80",
		"k8s://svc/stunner/svc:80"} {
		_, _, err := r.Peer(context.Background(), addr)
		assert.ErrorContains(t, err, "invalid Service address", addr)
	}
}

// TestFamily pins the family syntax and which hosts a family prefers.
func TestFamily(t *testing.T) {
	for s, want := range map[string]Family{"": AnyFamily, "ipv4": IPv4, "IPv6": IPv6} {
		f, err := NewFamily(s)
		require.NoError(t, err, s)
		assert.Equal(t, want, f, s)
	}
	_, err := NewFamily("dual")
	assert.Error(t, err)

	assert.True(t, IPv4.matches("1.2.3.4"))
	assert.False(t, IPv4.matches("2001:db8::1"))
	assert.True(t, IPv6.matches("2001:db8::1"))
	assert.False(t, IPv6.matches("1.2.3.4"))
	assert.False(t, IPv6.matches("::ffff:1.2.3.4"), "a v4-mapped address is IPv4")
	assert.False(t, IPv4.matches("stunner.example.com"), "a hostname is of no family")
	assert.False(t, AnyFamily.matches("1.2.3.4"), "any family takes the first address")
}
