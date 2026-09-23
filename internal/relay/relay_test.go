package relay

import (
	"net"
	"testing"

	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	"github.com/stretchr/testify/assert"
)

// TestRelayIPFor verifies the per-allocation relay-address family selection: the first Addrs entry
// matching the allocation network's family, falling back to the listener Addr.
func TestRelayIPFor(t *testing.T) {
	v4 := net.ParseIP("1.2.3.4")
	v6 := net.ParseIP("2001:db8::1")
	fallback := net.ParseIP("9.9.9.9")

	// dual-stack: pick the matching family, regardless of order or transport
	dual := &Relay{addrs: []net.IP{v4, v6}, fallback: net.ParseIP("0.0.0.0")}
	assert.Equal(t, v4, dual.relayIPFor("udp4"), "udp4 -> IPv4")
	assert.Equal(t, v6, dual.relayIPFor("udp6"), "udp6 -> IPv6")
	assert.Equal(t, v4, dual.relayIPFor("tcp4"), "tcp4 -> IPv4")
	assert.Equal(t, v6, dual.relayIPFor("tcp6"), "tcp6 -> IPv6")

	reordered := &Relay{addrs: []net.IP{v6, v4}, fallback: net.ParseIP("0.0.0.0")}
	assert.Equal(t, v4, reordered.relayIPFor("udp4"), "order-independent IPv4")
	assert.Equal(t, v6, reordered.relayIPFor("udp6"), "order-independent IPv6")

	// no matching family -> fall back to Addr
	v4only := &Relay{addrs: []net.IP{v4}, fallback: fallback}
	assert.Equal(t, fallback, v4only.relayIPFor("udp6"), "no IPv6 -> fallback")

	// no Addrs at all -> Addr
	none := &Relay{fallback: v4}
	assert.Equal(t, v4, none.relayIPFor("udp6"), "empty addrs -> fallback")
}

// TestParseRelayAddrs covers the Addr/Addrs parsing, including the "localhost" dual-stack loopback.
func TestParseRelayAddrs(t *testing.T) {
	// explicit dual-stack Addrs, Addr as fallback
	addrs, fb := parseRelayAddrs(&stnrv1.ListenerConfig{Addr: "0.0.0.0", Addrs: []string{"1.2.3.4", "2001:db8::1"}})
	assert.Equal(t, []net.IP{net.ParseIP("1.2.3.4"), net.ParseIP("2001:db8::1")}, addrs, "dual-stack addrs")
	assert.Equal(t, net.ParseIP("0.0.0.0"), fb, "fallback = Addr")

	// no Addrs -> fallback from Addr only
	addrs, fb = parseRelayAddrs(&stnrv1.ListenerConfig{Addr: "1.2.3.4"})
	assert.Empty(t, addrs, "no addrs")
	assert.Equal(t, net.ParseIP("1.2.3.4"), fb, "fallback")

	// localhost is not an IP literal -> seed the dual-stack loopback so both families match
	addrs, fb = parseRelayAddrs(&stnrv1.ListenerConfig{Addr: "localhost"})
	assert.Equal(t, []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, addrs, "localhost -> dual-stack loopback")
	assert.Nil(t, fb, "localhost has no IP fallback")
	r := &Relay{addrs: addrs, fallback: fb}
	assert.Equal(t, net.ParseIP("127.0.0.1"), r.relayIPFor("udp4"), "localhost udp4 -> IPv4 loopback")
	assert.Equal(t, net.ParseIP("::1"), r.relayIPFor("udp6"), "localhost udp6 -> IPv6 loopback")
}
