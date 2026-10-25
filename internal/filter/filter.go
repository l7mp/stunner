// Package filter implements the filters of stunnerd: named rules admitting or dropping traffic by
// address and port. Listeners check their clients' sources, clusters the peers of the traffic
// they carry.
package filter

import (
	"net/netip"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// rule is a compiled filter rule.
type rule struct {
	allow  bool
	any    bool // every address, of both families
	block  netip.Prefix
	except []netip.Prefix
	ports  [][2]uint16 // first and last port; none is every port
}

func (r *rule) matches(a netip.AddrPort) bool {
	ip := a.Addr().Unmap()
	if !r.any && !r.block.Contains(ip) {
		return false
	}
	for _, e := range r.except {
		if e.Contains(ip) {
			return false
		}
	}
	if len(r.ports) == 0 {
		return true
	}
	if a.Port() == 0 {
		// the address on any port: allowed on some ports is allowed, denied on some is not denied
		return r.allow
	}
	for _, p := range r.ports {
		if a.Port() >= p[0] && a.Port() <= p[1] {
			return true
		}
	}
	return false
}

// Filter is a compiled filter config: its rules, in order.
type Filter struct {
	name  string
	rules []rule
}

// New compiles a validated filter config.
func New(conf *stnrv2.FilterConfig) *Filter {
	f := &Filter{name: conf.Name}
	for _, r := range conf.Rules {
		a, _ := stnrv2.NewFilterAction(r.Action)
		cr := rule{allow: a == stnrv2.FilterActionAllow, any: r.IPBlock == nil}
		if r.IPBlock != nil {
			cr.block = netip.MustParsePrefix(r.IPBlock.CIDR)
			for _, e := range r.IPBlock.Except {
				cr.except = append(cr.except, netip.MustParsePrefix(e))
			}
		}
		for _, p := range r.Ports {
			last := p.EndPort
			if last == 0 {
				last = p.Port
			}
			cr.ports = append(cr.ports, [2]uint16{uint16(p.Port), uint16(last)})
		}
		f.rules = append(f.rules, cr)
	}
	return f
}

func (f *Filter) Name() string { return f.name }

// Match returns the verdict of the first rule matching the traffic of an address; matched is false
// when no rule matches. Port 0 asks for the address on any port: it matches an ALLOW on some of
// its ports, and a DENY only on all of them.
func (f *Filter) Match(a netip.AddrPort) (allow, matched bool) {
	for i := range f.rules {
		if f.rules[i].matches(a) {
			return f.rules[i].allow, true
		}
	}
	return false, false
}
