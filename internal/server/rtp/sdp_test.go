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
		name  string
		sdp   string
		addrs []netip.AddrPort
		rtcp  []netip.AddrPort
		mux   []bool
		check func(t *testing.T, d *sdp.SessionDescription)
	}{
		{
			name: "towards the cluster, mux offered with an RTCP fallback socket", sdp: trunkPlain,
			addrs: []netip.AddrPort{ap("10.0.0.4:5100")}, rtcp: []netip.AddrPort{ap("10.0.0.4:5102")},
			mux: []bool{true},
			check: func(t *testing.T, d *sdp.SessionDescription) {
				assert.Equal(t, "10.0.0.4", d.ConnectionInformation.Address.Address, "session address")
				assert.Equal(t, uint64(1406452391), d.Origin.SessionID, "origin kept")
				assert.Equal(t, "10.0.0.4", d.Origin.UnicastAddress, "origin address")
				m := d.MediaDescriptions[0]
				assert.Equal(t, 5100, m.MediaName.Port.Value)
				assert.Equal(t, []string{"0", "8", "101"}, m.MediaName.Formats)
				_, ok := m.Attribute("rtcp-mux")
				assert.True(t, ok, "rtcp-mux offered")
				v, _ := m.Attribute("rtcp")
				assert.Equal(t, "5102", v, "the RTCP fallback socket")
				v, _ = m.Attribute("maxptime")
				assert.Equal(t, "150", v, "other attributes kept")
				_, ok = m.Attribute("sendrecv")
				assert.True(t, ok, "direction kept")
				assert.Contains(t, m.Attributes, sdp.Attribute{Key: "rtpmap", Value: "101 telephone-event/8000"})
			},
		},
		{
			name: "towards a trunk, RTCP on the public port", sdp: trunkRTCPAddr,
			addrs: []netip.AddrPort{pub}, rtcp: []netip.AddrPort{pub}, mux: []bool{false},
			check: func(t *testing.T, d *sdp.SessionDescription) {
				m := d.MediaDescriptions[0]
				assert.Equal(t, 30000, m.MediaName.Port.Value)
				assert.Equal(t, "1.2.3.4", m.ConnectionInformation.Address.Address, "media address")
				v, _ := m.Attribute("rtcp")
				assert.Equal(t, "30000", v, "RTCP on the public port, not on the trunk's address")
				_, ok := m.Attribute("rtcp-mux")
				assert.False(t, ok, "no rtcp-mux")
			},
		},
		{
			name: "ICE stripped, crypto and direction kept", sdp: trunkMuxSRTPICE,
			addrs: []netip.AddrPort{pub}, rtcp: []netip.AddrPort{pub}, mux: []bool{true},
			check: func(t *testing.T, d *sdp.SessionDescription) {
				assert.Equal(t, "1.2.3.4", d.ConnectionInformation.Address.Address, "session address")
				_, ok := d.Attribute("ice-lite")
				assert.False(t, ok, "no session ICE")
				m := d.MediaDescriptions[0]
				assert.Equal(t, []string{"RTP", "SAVP"}, m.MediaName.Protos)
				assert.Equal(t, []string{"8"}, m.MediaName.Formats)
				for _, key := range []string{"ice-ufrag", "ice-pwd", "candidate"} {
					_, ok := m.Attribute(key)
					assert.False(t, ok, "no %s", key)
				}
				v, _ := m.Attribute("crypto")
				assert.Equal(t, "1 AES_CM_128_HMAC_SHA1_80 inline:WVNfX19zZW1jdGwgKCkgewkyMjA7fQp9CnVubGVz", v)
				_, ok = m.Attribute("rtcp-mux")
				assert.True(t, ok)
				_, ok = m.Attribute("sendonly")
				assert.True(t, ok)
			},
		},
		{
			name: "a rejected section stays rejected", sdp: trunkRejected,
			addrs: []netip.AddrPort{pub, {}}, rtcp: []netip.AddrPort{pub, {}}, mux: []bool{false, false},
			check: func(t *testing.T, d *sdp.SessionDescription) {
				assert.Equal(t, "1.2.3.4", d.ConnectionInformation.Address.Address, "session address")
				assert.Equal(t, "1.2.3.4", d.Origin.UnicastAddress, "origin address")
				require.Len(t, d.MediaDescriptions, 2)
				assert.Equal(t, 30000, d.MediaDescriptions[0].MediaName.Port.Value)
				assert.Equal(t, 0, d.MediaDescriptions[1].MediaName.Port.Value, "video rejected")
				assert.Equal(t, []string{"96"}, d.MediaDescriptions[1].MediaName.Formats)
			},
		},
	} {
		d := &sdp.SessionDescription{}
		require.NoError(t, d.UnmarshalString(tc.sdp), tc.name)
		rewrite(d, tc.addrs, tc.rtcp, tc.mux)
		b, err := d.Marshal()
		require.NoError(t, err, tc.name)
		// the rewritten SDP parses back, to the proxy's endpoints
		d2 := &sdp.SessionDescription{}
		require.NoError(t, d2.Unmarshal(b), tc.name)
		t.Run(tc.name, func(t *testing.T) { tc.check(t, d2) })
		eps, err := endpoints(d2)
		require.NoError(t, err, tc.name)
		assert.Equal(t, tc.addrs[0], eps[0].rtp, tc.name)
	}
}
