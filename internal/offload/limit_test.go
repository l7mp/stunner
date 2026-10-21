package offload

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	licensecfg "github.com/l7mp/stunner/v2/pkg/config/license"
)

// side returns the client side of a test flow from client port p.
func side(p int) Connection {
	return Connection{RemoteAddr: &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: p},
		LocalAddr: &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 3478}, Protocol: ProtocolUDP}
}

// peer is the peer side every test flow shares.
var peer = Connection{RemoteAddr: &net.UDPAddr{IP: net.IPv4(10, 0, 1, 1), Port: 9000},
	LocalAddr: &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 30000}, Protocol: ProtocolUDP}

// TestOffloadLimit pins the admission of the offload limit over the null engine: the flows beyond
// the limit are refused, a refreshed flow keeps its place, a removed one frees it, a restart frees
// all, and an unlimited or unknown limit admits everything.
func TestOffloadLimit(t *testing.T) {
	defer func(f func(licensecfg.ConfigManager) int) { offloadLimit = f }(offloadLimit)
	limit := 2
	offloadLimit = func(licensecfg.ConfigManager) int { return limit }

	e := New(Deps{})
	require.NoError(t, e.Start("none", nil))
	defer e.Close() //nolint:errcheck

	require.NoError(t, e.Upsert(side(1), peer, "l", "c"), "1st")
	require.NoError(t, e.Upsert(side(2), peer, "l", "c"), "2nd")
	assert.ErrorIs(t, e.Upsert(side(3), peer, "l", "c"), ErrOffloadLimit, "3rd beyond the limit")
	assert.NoError(t, e.Upsert(side(1), peer, "l", "c"), "a refresh keeps its place")

	require.NoError(t, e.Remove(side(1), peer))
	assert.NoError(t, e.Upsert(side(3), peer, "l", "c"), "3rd in the freed place")
	assert.ErrorIs(t, e.Upsert(side(1), peer, "l", "c"), ErrOffloadLimit, "1st is refused now")
	assert.NoError(t, e.Remove(side(4), peer), "removing an unknown flow is no error")
	assert.ErrorIs(t, e.Upsert(side(4), peer, "l", "c"), ErrOffloadLimit,
		"removing an unknown flow frees nothing")

	require.NoError(t, e.Close())
	require.NoError(t, e.Start("none", nil))
	assert.NoError(t, e.Upsert(side(1), peer, "l", "c"), "a restart frees every place")
	assert.NoError(t, e.Upsert(side(4), peer, "l", "c"))
	assert.ErrorIs(t, e.Upsert(side(5), peer, "l", "c"), ErrOffloadLimit)

	limit = 1
	assert.ErrorIs(t, e.Upsert(side(6), peer, "l", "c"), ErrOffloadLimit,
		"a lower limit refuses new flows")
	assert.NoError(t, e.Upsert(side(4), peer, "l", "c"), "but refreshes the held ones")

	for _, l := range []int{0, -1} {
		limit = l
		for p := 10; p < 20; p++ {
			assert.NoError(t, e.Upsert(side(p), peer, "l", "c"), "limit %d admits everything", l)
		}
	}
}

// TestOffloadLimitOfLicense pins that the limit is the offload limit of the license's tier.
func TestOffloadLimitOfLicense(t *testing.T) {
	assert.Equal(t, -1, offloadLimit(nil), "no license, no known limit")
	lic := licensecfg.New(nil)
	assert.Equal(t, licensecfg.GetLimits(lic.SubscriptionType()).Offloads, offloadLimit(lic))
}
