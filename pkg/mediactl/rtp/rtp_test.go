package rtp

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConn sends payloads between two RTP sessions over rtcp-mux: the payloads arrive as written,
// the sequence is counted, and every ReportEvery packets a sender report is counted, not read.
func TestConn(t *testing.T) {
	ReportEvery = 5
	pa, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	pb, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	a := NewConn(pa, pb.LocalAddr(), pb.LocalAddr(), 0)
	b := NewConn(pb, pa.LocalAddr(), pa.LocalAddr(), 0)
	defer a.Close() //nolint:errcheck
	defer b.Close() //nolint:errcheck

	buf := make([]byte, 1500)
	for i := range 10 {
		_, err := a.Write([]byte{byte(i), 1, 2})
		require.NoError(t, err)
		n, err := b.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, []byte{byte(i), 1, 2}, buf[:n], "the payload as written")
	}
	assert.Equal(t, Stats{Sent: 10, RTCPSent: 2}, a.Stats())
	s := b.Stats()
	assert.Equal(t, 10, s.Received)
	assert.Zero(t, s.Lost)
	assert.Equal(t, 1, s.RTCPReceived, "the first report was read past, the second waits in the socket")
	assert.Equal(t, pb.LocalAddr(), a.RemoteAddr())
}
