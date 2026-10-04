package filter

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func compile(t *testing.T, rules ...stnrv2.FilterRule) *Filter {
	t.Helper()
	conf := &stnrv2.FilterConfig{Name: "f", Rules: rules}
	require.NoError(t, conf.Validate())
	return New(conf)
}

func TestFilter(t *testing.T) {
	// the first matching rule decides: the exception before the block
	f := compile(t,
		stnrv2.FilterRule{Action: "DENY", IPBlock: &stnrv2.IPBlock{CIDR: "203.0.113.66"}},
		stnrv2.FilterRule{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "203.0.113.0/24",
			Except: []string{"203.0.113.128/25"}}},
		stnrv2.FilterRule{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "198.51.100.7"},
			Ports: []stnrv2.FilterPort{{Port: 5060}, {Port: 10000, EndPort: 20000}}},
		stnrv2.FilterRule{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "2001:db8::/32"},
			Ports: []stnrv2.FilterPort{{Port: 5060}}},
		stnrv2.FilterRule{Action: "DENY", IPBlock: &stnrv2.IPBlock{CIDR: "0.0.0.0/0"}},
	)
	for addr, want := range map[string]bool{
		"203.0.113.10:5060":          true,
		"203.0.113.66:5060":          false, // the exception first
		"203.0.113.200:5060":         false, // excepted from the block, then the final DENY
		"198.51.100.7:5060":          true,
		"198.51.100.7:5061":          false, // not a port of the rule
		"198.51.100.7:15000":         true,  // within the range
		"198.51.100.7:20001":         false,
		"[2001:db8::1]:5060":         true,
		"[::ffff:203.0.113.10]:5060": true,  // a v4-mapped address matches its v4 block
		"192.0.2.1:5060":             false, // the final DENY
	} {
		allow, matched := f.Match(netip.MustParseAddrPort(addr))
		assert.Equal(t, want, allow && matched, addr)
	}
	_, matched := f.Match(netip.MustParseAddrPort("[2001:db8::1]:5070"))
	assert.False(t, matched, "no rule matches: the decision is left to the next filter")

	// the order decides: an ALLOW before a DENY of the same block wins
	f = compile(t,
		stnrv2.FilterRule{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.0.0.0/8"}},
		stnrv2.FilterRule{Action: "DENY", IPBlock: &stnrv2.IPBlock{CIDR: "10.1.0.0/16"}})
	allow, _ := f.Match(netip.MustParseAddrPort("10.1.2.3:80"))
	assert.True(t, allow)
	_, matched = compile(t).Match(netip.MustParseAddrPort("10.0.0.1:80"))
	assert.False(t, matched, "no rules match nothing")
}

// TestFilterAnyAddress pins that a rule without an address block matches every address of both
// families: a bare DENY closes an allow-list for IPv4 and IPv6 at once.
func TestFilterAnyAddress(t *testing.T) {
	f := compile(t,
		stnrv2.FilterRule{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.0.0.0/8"}},
		stnrv2.FilterRule{Action: "DENY", Ports: []stnrv2.FilterPort{{Port: 22}}},
		stnrv2.FilterRule{Action: "DENY"},
	)
	for addr, want := range map[string]bool{
		"10.1.2.3:22":         true, // the ALLOW comes first
		"192.0.2.1:22":        false,
		"192.0.2.1:5060":      false,
		"[2001:db8::1]:5060":  false,
		"[::ffff:10.1.2.3]:1": true, // a mapped IPv4 address is IPv4
	} {
		allow, matched := f.Match(netip.MustParseAddrPort(addr))
		assert.True(t, matched, addr)
		assert.Equal(t, want, allow, addr)
	}
}

func TestFilterValidate(t *testing.T) {
	for _, r := range []stnrv2.FilterRule{
		{Action: "DROP", IPBlock: &stnrv2.IPBlock{CIDR: "10.0.0.0/8"}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "sip.example.com"}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.0.0.0/33"}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.0.0.0/8", Except: []string{"x"}}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.0.0.0/8"}, Ports: []stnrv2.FilterPort{{Port: 0}}},
		{Action: "ALLOW", IPBlock: &stnrv2.IPBlock{CIDR: "10.0.0.0/8"},
			Ports: []stnrv2.FilterPort{{Port: 6000, EndPort: 5000}}},
	} {
		conf := &stnrv2.FilterConfig{Name: "f", Rules: []stnrv2.FilterRule{r}}
		assert.Error(t, conf.Validate(), "%+v", r)
	}

	// actions are upper case and blocks canonical, and a second validation changes nothing
	conf := &stnrv2.FilterConfig{Name: "f", Rules: []stnrv2.FilterRule{{Action: "allow",
		IPBlock: &stnrv2.IPBlock{CIDR: "10.1.2.3/8", Except: []string{"10.1.2.3"}}}}}
	require.NoError(t, conf.Validate())
	assert.Equal(t, "ALLOW", conf.Rules[0].Action)
	assert.Equal(t, "10.0.0.0/8", conf.Rules[0].IPBlock.CIDR)
	assert.Equal(t, []string{"10.1.2.3/32"}, conf.Rules[0].IPBlock.Except)
	once := &stnrv2.FilterConfig{}
	conf.DeepCopyInto(once)
	require.NoError(t, conf.Validate())
	assert.True(t, once.DeepEqual(conf))
}
