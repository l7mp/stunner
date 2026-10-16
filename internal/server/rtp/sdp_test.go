package rtp

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/pion/sdp/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sdpOf(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }

// Trunk SDP samples in the shapes carriers send.
var (
	// plain G.711, RTCP on the next port
	trunkPlain = sdpOf("v=0", "o=root 1406452391 1406452391 IN IP4 54.172.60.1",
		"s=Media Gateway", "c=IN IP4 54.172.60.1", "t=0 0", "m=audio 18436 RTP/AVP 0 8 101",
		"a=rtpmap:0 PCMU/8000", "a=rtpmap:8 PCMA/8000", "a=rtpmap:101 telephone-event/8000",
		"a=fmtp:101 0-16", "a=ptime:20", "a=maxptime:150", "a=sendrecv")
	// RTCP named on another address with a=rtcp, the connection per media section
	trunkRTCPAddr = sdpOf("v=0", "o=- 3 2 IN IP4 192.0.2.10", "s=-", "t=0 0",
		"m=audio 20000 RTP/AVP 0", "c=IN IP4 192.0.2.10", "a=rtcp:20011 IN IP4 192.0.2.11",
		"a=rtpmap:0 PCMU/8000", "a=sendrecv")
	// rtcp-mux, SRTP, ICE
	trunkMuxSRTPICE = sdpOf("v=0", "o=- 7 1 IN IP4 198.51.100.7", "s=-", "c=IN IP4 198.51.100.7",
		"t=0 0", "a=ice-lite", "m=audio 40000 RTP/SAVP 8", "a=rtpmap:8 PCMA/8000",
		"a=crypto:1 AES_CM_128_HMAC_SHA1_80 inline:WVNfX19zZW1jdGwgKCkgewkyMjA7fQp9CnVubGVz",
		"a=rtcp-mux", "a=ice-ufrag:abcd", "a=ice-pwd:0123456789abcdef01234567",
		"a=candidate:1 1 UDP 2130706431 198.51.100.7 40000 typ host", "a=sendonly")
	// a rejected video section beside the audio
	trunkRejected = sdpOf("v=0", "o=- 1 1 IN IP4 203.0.113.9", "s=-", "c=IN IP4 203.0.113.9",
		"t=0 0", "m=audio 5004 RTP/AVP 0", "a=rtpmap:0 PCMU/8000", "m=video 0 RTP/AVP 96")
)

func TestEndpoints(t *testing.T) {
	ap := netip.MustParseAddrPort
	for _, tc := range []struct {
		name string
		sdp  string
		want []endpoint
	}{
		{"plain", trunkPlain, []endpoint{{rtp: ap("54.172.60.1:18436"), rtcp: ap("54.172.60.1:18437")}}},
		{"rtcp-addr", trunkRTCPAddr, []endpoint{{rtp: ap("192.0.2.10:20000"), rtcp: ap("192.0.2.11:20011")}}},
		{"mux", trunkMuxSRTPICE, []endpoint{{rtp: ap("198.51.100.7:40000"), rtcp: ap("198.51.100.7:40000"), mux: true}}},
		{"rejected", trunkRejected, []endpoint{{rtp: ap("203.0.113.9:5004"), rtcp: ap("203.0.113.9:5005")}, {}}},
	} {
		d := &sdp.SessionDescription{}
		require.NoError(t, d.UnmarshalString(tc.sdp), tc.name)
		got, err := endpoints(d)
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.want, got, tc.name)
	}
}

func TestRewrite(t *testing.T) {
	ap := netip.MustParseAddrPort
	pub := ap("1.2.3.4:30000")
	for _, tc := range []struct {
		name      string
		sdp       string
		addrs     []netip.AddrPort
		rtcp      []netip.AddrPort
		mux       []bool
		contains  []string
		forbidden []string
	}{
		{
			name: "towards the cluster, mux offered with an RTCP fallback socket", sdp: trunkPlain,
			addrs: []netip.AddrPort{ap("10.0.0.4:5100")}, rtcp: []netip.AddrPort{ap("10.0.0.4:5102")},
			mux: []bool{true},
			contains: []string{"c=IN IP4 10.0.0.4", "m=audio 5100 RTP/AVP 0 8 101", "a=rtcp-mux", "a=rtcp:5102",
				"o=root 1406452391 1406452391 IN IP4 10.0.0.4", "a=rtpmap:101 telephone-event/8000",
				"a=maxptime:150", "a=sendrecv"},
			forbidden: []string{"54.172.60.1"},
		},
		{
			name: "towards a trunk, RTCP on the public port", sdp: trunkRTCPAddr,
			addrs: []netip.AddrPort{pub}, rtcp: []netip.AddrPort{pub}, mux: []bool{false},
			contains:  []string{"m=audio 30000 RTP/AVP 0", "c=IN IP4 1.2.3.4", "a=rtcp:30000"},
			forbidden: []string{"192.0.2.1", "a=rtcp-mux", "a=rtcp:20011"},
		},
		{
			name: "ICE stripped, crypto and direction kept", sdp: trunkMuxSRTPICE,
			addrs: []netip.AddrPort{pub}, rtcp: []netip.AddrPort{pub}, mux: []bool{true},
			contains: []string{"m=audio 30000 RTP/SAVP 8", "a=crypto:1 AES_CM_128_HMAC_SHA1_80",
				"a=rtcp-mux", "a=sendonly"},
			forbidden: []string{"ice-", "candidate", "198.51.100.7"},
		},
		{
			name: "a rejected section stays rejected", sdp: trunkRejected,
			addrs: []netip.AddrPort{pub, {}}, rtcp: []netip.AddrPort{pub, {}}, mux: []bool{false, false},
			contains:  []string{"m=audio 30000 RTP/AVP 0", "m=video 0 RTP/AVP 96"},
			forbidden: []string{"203.0.113.9"},
		},
	} {
		d := &sdp.SessionDescription{}
		require.NoError(t, d.UnmarshalString(tc.sdp), tc.name)
		rewrite(d, tc.addrs, tc.rtcp, tc.mux)
		b, err := d.Marshal()
		require.NoError(t, err, tc.name)
		out := string(b)
		for _, s := range tc.contains {
			assert.Contains(t, out, s, tc.name)
		}
		for _, s := range tc.forbidden {
			assert.NotContains(t, out, s, tc.name)
		}
		// the rewritten SDP parses back to the proxy's endpoints
		d2 := &sdp.SessionDescription{}
		require.NoError(t, d2.Unmarshal(b), tc.name)
		eps, err := endpoints(d2)
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.addrs[0], eps[0].rtp, tc.name)
	}
}
