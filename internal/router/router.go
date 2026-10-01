// Package router implements the routing policies of a cluster over its endpoints.
package router

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
	"sync/atomic"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// New creates the router of a cluster config, skipping the endpoints its policy cannot use.
func New(conf *stnrv2.ClusterConfig, rt *runtime.Runtime, log logging.LeveledLogger) (api.Router, error) {
	policy, err := stnrv2.NewRoutingPolicy(conf.RoutingPolicy)
	if err != nil {
		return nil, err
	}
	b := base{name: conf.Name, rt: rt}
	for _, raw := range conf.Endpoints {
		ep, err := stnrv2.ParseEndpoint(raw)
		if err != nil {
			log.Warnf("cluster %s: skipping endpoint %q: %s", conf.Name, raw, err.Error())
			continue
		}
		_, dialable := ep.HostPort()
		switch {
		case policy == stnrv2.RoutingPolicyFilter && ep.RestrictsPort():
			log.Warnf("cluster %s: the port of endpoint %q is not enforced: a %s cluster admits "+
				"peers by IP", conf.Name, raw, policy.String())
		case policy != stnrv2.RoutingPolicyFilter && !dialable:
			log.Warnf("cluster %s: skipping endpoint %q: a %s cluster needs a single address "+
				"or domain with a single port", conf.Name, raw, policy.String())
			continue
		}
		b.endpoints = append(b.endpoints, ep)
	}

	if policy == stnrv2.RoutingPolicyRoundRobin {
		return &RoundRobin{base: b}, nil
	}
	return &Filter{base: b}, nil
}

type base struct {
	name      string
	endpoints []*stnrv2.Endpoint
	rt        *runtime.Runtime
}

func (b *base) Name() string { return b.name }

// addrs returns the IPs of an endpoint, resolving a domain.
func (b *base) addrs(ep *stnrv2.Endpoint) []net.IP {
	if ep.Domain == "" {
		return []net.IP{ep.Prefix.IP}
	}
	hosts, err := b.rt.Resolver.Lookup(ep.Domain)
	if err != nil {
		return nil
	}
	return hosts
}

// Filter admits the destinations an endpoint contains, ports ignored.
type Filter struct {
	base
}

func (f *Filter) Route(dst netip.AddrPort) (netip.AddrPort, bool, error) {
	if !dst.IsValid() {
		return dst, false, fmt.Errorf("cluster %s: a %s cluster needs a destination", f.name,
			stnrv2.RoutingPolicyFilter.String())
	}
	ip := net.IP(dst.Addr().Unmap().AsSlice())
	for _, ep := range f.endpoints {
		if ep.Domain == "" {
			if ep.Contains(ip) {
				return dst, true, nil
			}
			continue
		}
		for _, h := range f.addrs(ep) {
			if h.Equal(ip) {
				return dst, true, nil
			}
		}
	}
	return dst, false, nil
}

// RoundRobin chooses the resolved endpoints in turn.
type RoundRobin struct {
	base
	next atomic.Uint64
}

func (r *RoundRobin) Route(dst netip.AddrPort) (netip.AddrPort, bool, error) {
	if dst.IsValid() {
		return dst, false, fmt.Errorf("cluster %s: a %s cluster chooses its own destination",
			r.name, stnrv2.RoutingPolicyRoundRobin.String())
	}
	targets := []netip.AddrPort{}
	for _, ep := range r.endpoints {
		// a domain resolving to both families yields its IPv4 addresses, as Envoy's V4_PREFERRED
		hosts := r.addrs(ep)
		if slices.ContainsFunc(hosts, func(h net.IP) bool { return h.To4() != nil }) {
			hosts = slices.DeleteFunc(hosts, func(h net.IP) bool { return h.To4() == nil })
		}
		for _, h := range hosts {
			if a, ok := netip.AddrFromSlice(h); ok {
				targets = append(targets, netip.AddrPortFrom(a.Unmap(), uint16(ep.Port)))
			}
		}
	}
	if len(targets) == 0 {
		return dst, false, nil
	}
	return targets[int((r.next.Add(1)-1)%uint64(len(targets)))], true, nil
}
