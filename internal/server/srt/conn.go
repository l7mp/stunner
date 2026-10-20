package srt

import (
	"encoding/binary"
	"io"

	"github.com/datarhei/gosrt/packet"

	"github.com/l7mp/stunner/v2/internal/api"
)

// callerConn is the caller's conn of a spliced flow. Its reads write the endpoint's cookie into a
// conclusion the caller repeats, which still carries the server's cookie, and end at the caller's
// shutdown, the end of stream UDP does not have: the shutdown is read, the next read is io.EOF.
type callerConn struct {
	api.Conn
	cookie uint32
	done   bool
}

func (c *callerConn) Read(p []byte) (int, error) {
	if c.done {
		return 0, io.EOF
	}
	n, err := c.Conn.Read(p)
	// only control packets, the top bit set, are parsed: data packets are the bulk of the flow
	if n == 0 || p[0]&0x80 == 0 {
		return n, err
	}
	pkt, perr := packet.NewPacketFromData(nil, p[:n])
	if perr != nil {
		return n, err
	}
	defer pkt.Decommission()
	switch pkt.Header().ControlType {
	case packet.CTRLTYPE_SHUTDOWN:
		c.done = true
	case packet.CTRLTYPE_HANDSHAKE:
		var hs packet.CIFHandshake
		if pkt.UnmarshalCIF(&hs) == nil && hs.HandshakeType == packet.HSTYPE_CONCLUSION {
			binary.BigEndian.PutUint32(p[offCookie:], c.cookie)
		}
	}
	return n, err
}
