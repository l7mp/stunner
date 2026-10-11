package balancer_test

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server/balancer"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

// TestNew pins the policies with a balancer: ORIGINAL_DST balances nothing.
func TestNew(t *testing.T) {
	_, err := balancer.New(stnrv2.RoutingPolicyOriginalDst)
	assert.Error(t, err, "ORIGINAL_DST balances nothing")
	_, err = balancer.New(stnrv2.RoutingPolicyRoundRobin)
	assert.NoError(t, err)
}

// TestRoundRobin pins the round robin over the targets of every named cluster in order, with the
// name of the target's cluster, skipping missing clusters and endpoints without a port.
func TestRoundRobin(t *testing.T) {
	rt := runtime.New(runtime.Config{Logger: logger.NewLoggerFactory("all:ERROR")})
	for name, eps := range map[string][]string{
		"a": {"10.0.0.1:1000", "10.0.0.2:1000"},
		"b": {"10.0.1.1:2000", "10.0.1.9"},
	} {
		c, err := object.NewCluster(&stnrv2.ClusterConfig{Name: name, Protocol: "UDP", Endpoints: eps}, rt)
		require.NoError(t, err)
		require.NoError(t, rt.Registry.Add(c, nil))
		require.NoError(t, c.Start())
		t.Cleanup(func() { _ = c.Close(true) })
	}

	b, err := balancer.New(stnrv2.RoutingPolicyRoundRobin)
	require.NoError(t, err)
	_, _, ok := b.Next(rt, []string{"missing"})
	assert.False(t, ok, "no cluster, no target")

	type pick struct {
		dst     string
		cluster string
	}
	picks := []pick{}
	for range 4 {
		cluster, dst, ok := b.Next(rt, []string{"a", "missing", "b"})
		require.True(t, ok)
		picks = append(picks, pick{dst.String(), cluster})
	}
	assert.Equal(t, []pick{{"10.0.0.1:1000", "a"}, {"10.0.0.2:1000", "a"}, {"10.0.1.1:2000", "b"},
		{"10.0.0.1:1000", "a"}}, picks, "in turn, in cluster order")

	// a reconcile applies to the next choice
	c, ok := rt.Registry.Get(runtime.TypeCluster, "a")
	require.True(t, ok)
	require.NoError(t, c.(*object.Cluster).Reconcile(&stnrv2.ClusterConfig{Name: "a", Protocol: "UDP"}))
	for range 2 {
		_, dst, ok := b.Next(rt, []string{"a", "b"})
		require.True(t, ok)
		assert.Equal(t, netip.MustParseAddrPort("10.0.1.1:2000"), dst, "the only target left")
	}
}
