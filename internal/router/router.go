// Package router implements the destinations of a cluster: its endpoints, resolved.
package router

import (
	"net"
	"net/netip"
	"slices"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Router lists the destinations of a cluster: its endpoints that carry a port, domains resolved.
type Router struct {
	name      string
	endpoints []*stnrv2.Endpoint
	rt        *runtime.Runtime
}

var _ api.Router = &Router{}

// New creates the router of a cluster config, skipping the endpoints that do not parse.
func New(conf *stnrv2.ClusterConfig, rt *runtime.Runtime, log logging.LeveledLogger) (api.Router, error) {
	r := &Router{name: conf.Name, rt: rt}
	for _, raw := range conf.Endpoints {
		ep, err := stnrv2.ParseEndpoint(raw)
		if err != nil {
			log.Warnf("cluster %s: skipping endpoint %q: %s", conf.Name, raw, err.Error())
			continue
		}
		r.endpoints = append(r.endpoints, ep)
	}
	return r, nil
}

func (r *Router) Name() string { return r.name }

// Targets returns the endpoints that carry a port, a domain resolved to its addresses.
func (r *Router) Targets() []netip.AddrPort {
	targets := []netip.AddrPort{}
	for _, ep := range r.endpoints {
		if ep.Port == 0 {
			continue
		}
		hosts := []net.IP{ep.Addr.AsSlice()}
		if ep.Domain != "" {
			var err error
			if hosts, err = r.rt.Resolver.Lookup(ep.Domain); err != nil {
				continue
			}
			// a domain resolving to both families yields its IPv4 addresses, as Envoy's
			// V4_PREFERRED
			if slices.ContainsFunc(hosts, func(h net.IP) bool { return h.To4() != nil }) {
				hosts = slices.DeleteFunc(hosts, func(h net.IP) bool { return h.To4() == nil })
			}
		}
		for _, h := range hosts {
			if a, ok := netip.AddrFromSlice(h); ok {
				targets = append(targets, netip.AddrPortFrom(a.Unmap(), uint16(ep.Port)))
			}
		}
	}
	return targets
}
