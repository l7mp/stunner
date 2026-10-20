package srt

import (
	"bytes"
	"strings"

	"github.com/datarhei/gosrt/packet"
)

const (
	// srtMagic is the extension field of an induction response.
	srtMagic = 0x4A17
	// offCookie is the offset of the SYN cookie in a handshake packet: the server writes the
	// endpoint's cookie into the caller's raw conclusion, because a conclusion re-marshaled by
	// gosrt would lose the extensions gosrt does not keep (packet filter, group).
	offCookie = 16 + 28
)

// parseHandshake reads an SRT handshake packet; false for any other packet.
func parseHandshake(p []byte) (*packet.CIFHandshake, bool) {
	pkt, err := packet.NewPacketFromData(nil, p)
	if err != nil {
		return nil, false
	}
	defer pkt.Decommission()
	h := pkt.Header()
	hs := &packet.CIFHandshake{}
	if !h.IsControlPacket || h.ControlType != packet.CTRLTYPE_HANDSHAKE || pkt.UnmarshalCIF(hs) != nil {
		return nil, false
	}
	return hs, true
}

// handshakePacket makes the handshake packet of a CIF, addressed to the socket dest.
func handshakePacket(dest uint32, hs *packet.CIFHandshake) []byte {
	pkt := packet.NewPacket(nil)
	defer pkt.Decommission()
	h := pkt.Header()
	h.IsControlPacket, h.ControlType, h.DestinationSocketId = true, packet.CTRLTYPE_HANDSHAKE, dest
	_ = pkt.MarshalCIF(hs)
	var b bytes.Buffer
	_ = pkt.Marshal(&b)
	return b.Bytes()
}

// Resource returns the stream a stream ID names: the r= key of the access control syntax
// (#!::r=live/van7,m=publish), else the whole stream ID.
func Resource(sid string) string {
	rest, ok := strings.CutPrefix(sid, "#!::")
	if !ok {
		return sid
	}
	for kv := range strings.SplitSeq(rest, ",") {
		if v, ok := strings.CutPrefix(kv, "r="); ok {
			return v
		}
	}
	return ""
}
