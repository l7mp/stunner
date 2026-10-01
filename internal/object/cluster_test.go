package object_test

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func TestClusterObjectSemantics(t *testing.T) {
	cluster := func(typ stnrv2.ClusterType, eps ...string) *stnrv2.ClusterConfig {
		c := &stnrv2.ClusterConfig{Name: "cluster-a", Type: typ.String(), Endpoints: eps,
			Protocol: "UDP"}
		require.NoError(t, c.Validate())
		return c
	}

	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "strict-dns",
		setup: func(t *testing.T) (runtime.Object, stnrv2.Config, *stnrv2.StunnerConfig) {
			env := newTestEnv()
			base := cluster(stnrv2.ClusterTypeStrictDNS, "media.example.com:5000", "turn.example.com")
			obj, err := object.NewCluster(base, env.rt)
			require.NoError(t, err)
			return obj, base, nil
		},
		expectations: []inspectExpectation{
			{name: "unchanged-none", conf: cluster(stnrv2.ClusterTypeStrictDNS,
				"media.example.com:5000", "turn.example.com"), want: runtime.ActionNone},
			// Validate sorts the endpoints, so their order is no change at all
			{name: "reorder-none", conf: cluster(stnrv2.ClusterTypeStrictDNS,
				"turn.example.com", "media.example.com:5000"), want: runtime.ActionNone},
			// ports and domains reconcile in place
			{name: "port-change-reconcile", conf: cluster(stnrv2.ClusterTypeStrictDNS,
				"media.example.com:6000", "turn.example.com"), want: runtime.ActionReconcile},
			{name: "duplicate-domain-reconcile", conf: cluster(stnrv2.ClusterTypeStrictDNS,
				"media.example.com:5000", "media.example.com:5001", "turn.example.com"),
				want: runtime.ActionReconcile},
			{name: "domain-change-reconcile", conf: cluster(stnrv2.ClusterTypeStrictDNS,
				"media.example.org:5000", "turn.example.com"), want: runtime.ActionReconcile},
			{name: "domain-removed-reconcile", conf: cluster(stnrv2.ClusterTypeStrictDNS,
				"media.example.com:5000"), want: runtime.ActionReconcile},
			{name: "type-change-reconcile", conf: cluster(stnrv2.ClusterTypeStatic, "10.0.0.1"),
				want: runtime.ActionReconcile},
		},
	})

	// a router or address change reconciles in place, a dialer change restarts: live sessions
	// keep their dialers, new ones use the new one
	tunnel := func(port string) *stnrv2.TunnelConfig {
		return &stnrv2.TunnelConfig{URL: "turn:turn.example.com:" + port, Auth: staticAuthConfig()}
	}
	transport := func(proto stnrv2.Protocol, tn *stnrv2.TunnelConfig, addrs ...string) *stnrv2.ClusterConfig {
		c := cluster(stnrv2.ClusterTypeStatic, "10.0.0.1", "10.1.0.0/16")
		c.Protocol, c.Tunnel, c.Addrs = proto.String(), tn, addrs
		require.NoError(t, c.Validate())
		return c
	}
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "static",
		setup: func(t *testing.T) (runtime.Object, stnrv2.Config, *stnrv2.StunnerConfig) {
			env := newTestEnv()
			base := transport(stnrv2.ProtocolUDP, tunnel("3478"), "10.0.0.1")
			obj, err := object.NewCluster(base, env.rt)
			require.NoError(t, err)
			return obj, base, nil
		},
		expectations: []inspectExpectation{
			{name: "unchanged-none", conf: transport(stnrv2.ProtocolUDP, tunnel("3478"), "10.0.0.1"),
				want: runtime.ActionNone},
			{name: "endpoint-change-reconcile", conf: func() *stnrv2.ClusterConfig {
				c := transport(stnrv2.ProtocolUDP, tunnel("3478"), "10.0.0.1")
				c.Endpoints = []string{"10.0.0.2:5000"}
				return c
			}(), want: runtime.ActionReconcile},
			{name: "tunnel-change-restart", conf: transport(stnrv2.ProtocolUDP, tunnel("3479"),
				"10.0.0.1"), want: runtime.ActionRestart},
			{name: "protocol-change-restart", conf: transport(stnrv2.ProtocolTCP, nil, "10.0.0.1"),
				want: runtime.ActionRestart},
			{name: "addresses-change-reconcile", conf: transport(stnrv2.ProtocolUDP, tunnel("3478"),
				"10.0.0.2"), want: runtime.ActionReconcile},
			{name: "routing-policy-change-reconcile", conf: func() *stnrv2.ClusterConfig {
				c := transport(stnrv2.ProtocolUDP, tunnel("3478"), "10.0.0.1")
				c.RoutingPolicy = stnrv2.RoutingPolicyRoundRobin.String()
				return c
			}(), want: runtime.ActionReconcile},
		},
	})
}

// recordingResolver records the domains registered with it.
type recordingResolver struct {
	mu         sync.Mutex
	registered []string
	zone       map[string][]net.IP
}

func (r *recordingResolver) Register(domain string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registered = append(r.registered, domain)
	return nil
}

func (r *recordingResolver) Unregister(domain string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := slices.Index(r.registered, domain); i >= 0 {
		r.registered = slices.Delete(r.registered, i, i+1)
	}
}

func (r *recordingResolver) Lookup(domain string) ([]net.IP, error) {
	return slices.Clone(r.zone[domain]), nil
}
func (r *recordingResolver) Start() {}
func (r *recordingResolver) Close() {}

func (r *recordingResolver) domains() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Sorted(slices.Values(r.registered))
}

// TestClusterDomains pins the resolver side of a STRICT_DNS cluster: Start registers each domain
// once, a reconcile of a running cluster moves the registrations to the new domains, Close
// unregisters exactly those, and a STATIC cluster registers nothing.
func TestClusterDomains(t *testing.T) {
	env := newTestEnv()
	res := &recordingResolver{}
	env.rt.Resolver = res

	c, err := object.NewCluster(&stnrv2.ClusterConfig{Name: "cluster-a", Protocol: "UDP",
		Type:      stnrv2.ClusterTypeStrictDNS.String(),
		Endpoints: []string{"media.example.com:5000", "media.example.com:5001", "turn.example.com"},
	}, env.rt)
	require.NoError(t, err)
	mustAdd(t, env, c)
	require.NoError(t, c.Start())
	assert.Equal(t, []string{"media.example.com", "turn.example.com"}, res.domains())

	require.NoError(t, c.Reconcile(&stnrv2.ClusterConfig{Name: "cluster-a", Protocol: "UDP",
		Type:      stnrv2.ClusterTypeStrictDNS.String(),
		Endpoints: []string{"media.example.com:6000", "media.example.org"},
	}))
	assert.Equal(t, []string{"media.example.com", "media.example.org"}, res.domains())

	require.NoError(t, c.Close(false))
	assert.Empty(t, res.domains())

	// a reconcile of a stopped cluster registers nothing until Start
	require.NoError(t, c.Reconcile(&stnrv2.ClusterConfig{Name: "cluster-a", Protocol: "UDP",
		Type: stnrv2.ClusterTypeStrictDNS.String(), Endpoints: []string{"turn.example.com"}}))
	assert.Empty(t, res.domains())
	require.NoError(t, c.Start())
	assert.Equal(t, []string{"turn.example.com"}, res.domains())
	require.NoError(t, c.Close(false))
	assert.Empty(t, res.domains())

	s, err := object.NewCluster(&stnrv2.ClusterConfig{Name: "cluster-b", Protocol: "UDP",
		Endpoints: []string{"10.0.0.1"}}, env.rt)
	require.NoError(t, err)
	require.NoError(t, s.Start())
	assert.Empty(t, res.domains())
	require.NoError(t, s.Close(false))
}

// TestClusterRoute pins what a cluster found by name routes: a FILTER cluster admits the IPs an
// endpoint contains (a prefix or a resolved domain, ports ignored), a ROUND_ROBIN cluster chooses
// the single-port endpoints in turn, and each refuses the other kind of request. A reconcile
// applies to the next request.
func TestClusterRoute(t *testing.T) {
	env := newTestEnv()
	env.rt.Resolver = &recordingResolver{zone: map[string][]net.IP{
		"media.example.com": {net.ParseIP("10.2.0.1"), net.ParseIP("fd00::1"), net.ParseIP("10.2.0.2")},
	}}
	endpoints := []string{"10.0.0.1:5000", "10.1.0.0/16:6000-6100", "media.example.com:7000"}
	c, err := object.NewCluster(&stnrv2.ClusterConfig{Name: "cluster-a", Protocol: "UDP",
		Endpoints: endpoints}, env.rt)
	require.NoError(t, err)
	mustAdd(t, env, c)
	require.NoError(t, c.Start())

	r, ok := env.rt.Router("cluster-a")
	require.True(t, ok, "found by name")
	for ip, want := range map[string]bool{
		"10.0.0.1": true, "10.0.0.2": false, "10.1.9.9": true, "10.2.0.1": true, "10.2.0.3": false,
		"fd00::1": true,
	} {
		dst := netip.AddrPortFrom(netip.MustParseAddr(ip), 1234)
		got, ok, err := r.Route(dst)
		require.NoError(t, err)
		assert.Equal(t, want, ok, ip)
		assert.Equal(t, dst, got, "a filter routes to the destination itself")
	}
	_, _, err = r.Route(netip.AddrPort{})
	assert.Error(t, err, "a filter needs a destination")

	require.NoError(t, c.Reconcile(&stnrv2.ClusterConfig{Name: "cluster-a", Protocol: "UDP",
		Endpoints: endpoints, RoutingPolicy: stnrv2.RoutingPolicyRoundRobin.String()}))
	picked := map[string]bool{}
	for range 6 {
		dst, ok, err := r.Route(netip.AddrPort{})
		require.NoError(t, err)
		require.True(t, ok)
		picked[dst.String()] = true
	}
	assert.Equal(t, map[string]bool{"10.0.0.1:5000": true, "10.2.0.1:7000": true,
		"10.2.0.2:7000": true}, picked, "round robin over the resolved single-port endpoints, IPv4 preferred")
	_, _, err = r.Route(netip.MustParseAddrPort("10.0.0.1:5000"))
	assert.Error(t, err, "a load balancer chooses its own destination")

	require.NoError(t, c.Reconcile(&stnrv2.ClusterConfig{Name: "cluster-a", Protocol: "UDP",
		Endpoints: []string{"192.168.0.0/16"}, RoutingPolicy: stnrv2.RoutingPolicyRoundRobin.String()}))
	_, ok, err = r.Route(netip.AddrPort{})
	require.NoError(t, err)
	assert.False(t, ok, "a prefix is nothing to dial")
	require.NoError(t, c.Close(false))
}

// TestClusterDialer pins that a cluster found by name delegates to the dialer it runs, refuses
// while it is down, and runs the new dialer after a restart.
func TestClusterDialer(t *testing.T) {
	env := newLiveEnv(t, nil)
	c, err := object.NewCluster(&stnrv2.ClusterConfig{Name: "cluster-a", Protocol: "UDP",
		Addrs: []string{"10.0.0.1"}}, env.rt)
	require.NoError(t, err)
	env.start(t, c, nil)

	found, ok := env.rt.Dialer("cluster-a")
	require.True(t, ok, "found by name")
	assert.Equal(t, stnrv2.ProtocolUDP, found.Protocol())
	relay, advertised, err := found.ListenPacket("udp4", 0)
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", advertised.(*net.UDPAddr).IP.String(), "advertised at its address")
	_ = relay.Close()

	// an address change applies in place to the next relay socket
	require.NoError(t, c.Reconcile(&stnrv2.ClusterConfig{Name: "cluster-a", Protocol: "UDP",
		Addrs: []string{"10.0.0.2"}}))
	relay, advertised, err = found.ListenPacket("udp4", 0)
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.2", advertised.(*net.UDPAddr).IP.String(), "the new address")
	_ = relay.Close()

	// the restart of a dialer change: close, reconcile, start
	require.NoError(t, c.Close(false))
	_, err = found.Dial(context.Background(), nil, "127.0.0.1:1")
	assert.Error(t, err, "a cluster that is down refuses")
	require.NoError(t, c.Reconcile(&stnrv2.ClusterConfig{Name: "cluster-a", Protocol: "TCP"}))
	require.NoError(t, c.Start())
	assert.Equal(t, stnrv2.ProtocolTCP, found.Protocol(), "the new dialer serves")
	_, _, err = found.ListenPacket("udp4", 0)
	assert.ErrorIs(t, err, api.ErrNotSupported)
	require.NoError(t, c.Close(false))
	_, ok = env.rt.Dialer("no-such-cluster")
	assert.False(t, ok)
	_, ok = env.rt.Router("no-such-cluster")
	assert.False(t, ok)
}
