package v2

import (
	"fmt"
	"net/netip"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// FilterConfig is a named, ordered list of ALLOW and DENY rules on addresses and ports. The first
// matching rule decides; each caller decides what no match means: listener and cluster filters
// pass unmatched traffic, server filters refuse it.
type FilterConfig struct {
	// Name of the filter.
	Name string `json:"name"`
	// Rules are the rules of the filter.
	Rules []FilterRule `json:"rules,omitempty"`
}

// FilterRule admits or drops the traffic of an address block, optionally only on some ports.
type FilterRule struct {
	// Action is ALLOW or DENY.
	Action string `json:"action"`
	// IPBlock is the addresses the rule matches; none is every address of both families.
	IPBlock *IPBlock `json:"ip_block,omitempty"`
	// Ports restricts the rule to ports or port ranges; empty is every port.
	Ports []FilterPort `json:"ports,omitempty"`
}

// IPBlock is a block of addresses, as in a Kubernetes NetworkPolicy.
type IPBlock struct {
	// CIDR is the prefix of the block, such as "10.0.0.0/8"; a single address is a full-length
	// prefix.
	CIDR string `json:"cidr"`
	// Except are prefixes within the block the rule does not match.
	Except []string `json:"except,omitempty"`
}

// FilterPort is a port, or with EndPort a range of ports, as in a Kubernetes NetworkPolicy.
type FilterPort struct {
	// Port is the port, or the first port of the range.
	Port int `json:"port"`
	// EndPort is the last port of the range; 0 is the single port Port.
	EndPort int `json:"end_port,omitempty"`
}

// Validate checks a configuration and injects defaults: actions are upper case, prefixes are
// canonical (a single address becomes its full-length prefix).
func (req *FilterConfig) Validate() error {
	if req.Name == "" {
		return fmt.Errorf("missing name in filter configuration: %s", req.String())
	}
	if req.Rules == nil {
		req.Rules = []FilterRule{}
	}
	for i := range req.Rules {
		r := &req.Rules[i]
		a, err := NewFilterAction(r.Action)
		if err != nil {
			return fmt.Errorf("filter %q: %w", req.Name, err)
		}
		r.Action = a.String()
		if b := r.IPBlock; b != nil {
			if b.CIDR, err = canonicalPrefix(b.CIDR); err != nil {
				return fmt.Errorf("filter %q: %w", req.Name, err)
			}
			if b.Except == nil {
				b.Except = []string{}
			}
			for j := range b.Except {
				if b.Except[j], err = canonicalPrefix(b.Except[j]); err != nil {
					return fmt.Errorf("filter %q: %w", req.Name, err)
				}
			}
		}
		if r.Ports == nil {
			r.Ports = []FilterPort{}
		}
		for _, p := range r.Ports {
			if p.Port < 1 || p.Port > 65535 || (p.EndPort != 0 && (p.EndPort < p.Port || p.EndPort > 65535)) {
				return fmt.Errorf("filter %q: invalid port %d-%d", req.Name, p.Port, p.EndPort)
			}
		}
	}
	return nil
}

// canonicalPrefix parses an address or a prefix into a canonical prefix.
func canonicalPrefix(s string) (string, error) {
	if a, err := netip.ParseAddr(s); err == nil {
		return netip.PrefixFrom(a, a.BitLen()).String(), nil
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return "", fmt.Errorf("invalid address block %q", s)
	}
	return p.Masked().String(), nil
}

// ConfigName returns the name of the object to be configured.
func (req *FilterConfig) ConfigName() string {
	return req.Name
}

// DeepEqual compares two configurations.
func (req *FilterConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// DeepCopyInto copies a configuration.
func (req *FilterConfig) DeepCopyInto(dst Config) {
	ret := dst.(*FilterConfig)
	*ret = *req
	if req.Rules != nil {
		ret.Rules = make([]FilterRule, len(req.Rules))
		for i, r := range req.Rules {
			ret.Rules[i] = r
			if r.IPBlock != nil {
				b := *r.IPBlock
				b.Except = slices.Clone(r.IPBlock.Except)
				ret.Rules[i].IPBlock = &b
			}
			ret.Rules[i].Ports = slices.Clone(r.Ports)
		}
	}
}

// String stringifies the configuration.
func (req *FilterConfig) String() string {
	n := "-"
	if req.Name != "" {
		n = req.Name
	}
	rules := []string{}
	for _, r := range req.Rules {
		s := r.Action + ":*"
		if r.IPBlock != nil {
			s = r.Action + ":" + r.IPBlock.CIDR
			for _, e := range r.IPBlock.Except {
				s += "!" + e
			}
		}
		for _, p := range r.Ports {
			s += ":" + strconv.Itoa(p.Port)
			if p.EndPort != 0 {
				s += "-" + strconv.Itoa(p.EndPort)
			}
		}
		rules = append(rules, s)
	}
	return fmt.Sprintf("%q:{rules=[%s]}", n, strings.Join(rules, ","))
}

// FilterStatus is the status of a filter: its config.
type FilterStatus struct {
	*FilterConfig
}

// String stringifies the status.
func (req *FilterStatus) String() string {
	return req.FilterConfig.String()
}
