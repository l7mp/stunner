package srt

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/datarhei/gosrt/packet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/netconn"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func TestResource(t *testing.T) {
	assert.Equal(t, "live/van7", Resource("#!::r=live/van7,m=publish"))
	assert.Equal(t, "live/van7", Resource("#!::m=request,r=live/van7,u=alice"))
	assert.Equal(t, "", Resource("#!::m=publish"), "the access control syntax without a resource")
	assert.Equal(t, "plain-stream", Resource("plain-stream"), "any other stream ID is the stream")
}

// conclusion makes a caller's conclusion naming a stream ID.
func conclusion(t *testing.T, sid string, cookie uint32) []byte {
	t.Helper()
	p := handshakePacket(0, &packet.CIFHandshake{
		Version:       5,
		HandshakeType: packet.HSTYPE_CONCLUSION,
		SRTSocketId:   42,
		SynCookie:     cookie,
		HasSID:        true,
		StreamId:      sid,
	})
	require.Greater(t, len(p), offCookie+4)
	return p
}

// control makes a control packet with an empty CIF.
func control(typ packet.CtrlType) []byte {
	pkt := packet.NewPacket(nil)
	defer pkt.Decommission()
	pkt.Header().IsControlPacket, pkt.Header().ControlType = true, typ
	var b bytes.Buffer
	_ = pkt.Marshal(&b)
	return b.Bytes()
}

func TestParseHandshake(t *testing.T) {
	p := conclusion(t, "#!::r=a", 7)
	hs, ok := parseHandshake(p)
	require.True(t, ok)
	assert.Equal(t, packet.HSTYPE_CONCLUSION, hs.HandshakeType)
	assert.Equal(t, uint32(42), hs.SRTSocketId)
	assert.Equal(t, uint32(7), hs.SynCookie)
	assert.Equal(t, uint32(7), binary.BigEndian.Uint32(p[offCookie:]), "the cookie offset")
	assert.Equal(t, "#!::r=a", hs.StreamId)

	_, ok = parseHandshake(control(packet.CTRLTYPE_KEEPALIVE))
	assert.False(t, ok, "another control packet")
	_, ok = parseHandshake(p[:20])
	assert.False(t, ok, "a short packet")
}

// TestCallerConn pins the caller's conn of a spliced flow: a repeated conclusion leaves with the
// endpoint's cookie, other packets unchanged, and the shutdown ends the stream.
func TestCallerConn(t *testing.T) {
	client, caller := net.Pipe()
	c := &callerConn{Conn: netconn.NewConn(client, api.Tag{Name: "srt", Proto: stnrv2.ProtocolUDP}, nil),
		cookie: 0x77e0d913}
	send := func(p []byte) { go func() { _, _ = caller.Write(p) }() }
	buf := make([]byte, 1500)

	send(conclusion(t, "live", 0x5e81c07a))
	n, err := c.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, uint32(0x77e0d913), binary.BigEndian.Uint32(buf[offCookie:n]), "the endpoint's cookie")

	data := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	send(data)
	n, err = c.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, data, buf[:n], "a data packet passes unchanged")

	keepalive := control(packet.CTRLTYPE_KEEPALIVE)
	send(keepalive)
	n, err = c.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, keepalive, buf[:n], "a keepalive passes unchanged")

	shutdown := control(packet.CTRLTYPE_SHUTDOWN)
	send(shutdown)
	n, err = c.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, shutdown, buf[:n], "the shutdown is read")
	_, err = c.Read(buf)
	assert.ErrorIs(t, err, io.EOF, "and ends the stream")
}

// TestPuntRules pins the packets an offloaded flow keeps in user space: the handshakes and the
// shutdowns, by their wire encoding, and nothing else.
func TestPuntRules(t *testing.T) {
	punted := func(p []byte) bool {
		for _, r := range puntRules {
			if r.Match(p) {
				return true
			}
		}
		return false
	}
	assert.True(t, punted(conclusion(t, "#!::r=a", 7)), "a conclusion")
	assert.True(t, punted(control(packet.CTRLTYPE_SHUTDOWN)), "a shutdown")
	for _, typ := range []packet.CtrlType{packet.CTRLTYPE_KEEPALIVE, packet.CTRLTYPE_ACK,
		packet.CTRLTYPE_NAK, packet.CTRLTYPE_ACKACK} {
		assert.False(t, punted(control(typ)), "control type %d", typ)
	}
	data := packet.NewPacket(nil)
	defer data.Decommission()
	data.Header().PacketSequenceNumber = data.Header().PacketSequenceNumber.Inc()
	data.SetData([]byte("media"))
	var b bytes.Buffer
	require.NoError(t, data.Marshal(&b))
	assert.False(t, punted(b.Bytes()), "a data packet")
}
