// Package rtp frames a packet conn as an RTP session: a net.Conn whose writes leave as the
// payloads of RTP packets and whose reads return the payloads of the RTP packets received. It
// sends an RTCP sender report every second's worth of packets and counts the RTP and RTCP traffic
// of the session.
package rtp

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
)

// ReportEvery is the number of RTP packets between two sender reports: a second of 20 ms frames.
var ReportEvery = 50

// Stats are the counters of a session.
type Stats struct {
	// Sent and Received count the RTP packets.
	Sent, Received int
	// Lost is the number of RTP packets missing from the received sequence.
	Lost int
	// RTCPSent and RTCPReceived count the RTCP packets.
	RTCPSent, RTCPReceived int
}

// String summarizes the counters.
func (s Stats) String() string {
	return fmt.Sprintf("sent=%d received=%d lost=%d rtcp-sent=%d rtcp-received=%d",
		s.Sent, s.Received, s.Lost, s.RTCPSent, s.RTCPReceived)
}

// Conn is an RTP session over a packet conn, to a peer's RTP address and RTCP address (the same
// with rtcp-mux).
type Conn struct {
	pc            net.PacketConn
	rtpTo, rtcpTo net.Addr
	mu            sync.Mutex
	pkt           rtp.Packet
	stats         Stats
	firstSeq      int64
	maxSeq        int64 // extended over wraps
	readBuf       []byte
	closeOnce     sync.Once
}

// NewConn makes an RTP session of a payload type over a packet conn. Every packet advances the
// RTP timestamp by the length of its payload, the clock of G.711 at 8 kHz.
func NewConn(pc net.PacketConn, rtpTo, rtcpTo net.Addr, payloadType uint8) *Conn {
	return &Conn{pc: pc, rtpTo: rtpTo, rtcpTo: rtcpTo, readBuf: make([]byte, 1500),
		pkt: rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: payloadType,
			SSRC: uint32(time.Now().UnixNano())}}}
}

// Write sends p as the payload of the next RTP packet, and a sender report every ReportEvery
// packets.
func (c *Conn) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.pkt.SequenceNumber++
	c.pkt.Timestamp += uint32(len(p))
	c.pkt.Payload = p
	b, err := c.pkt.Marshal()
	seq, ssrc, ts := c.pkt.SequenceNumber, c.pkt.SSRC, c.pkt.Timestamp
	c.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if _, err := c.pc.WriteTo(b, c.rtpTo); err != nil {
		return 0, err
	}
	c.mu.Lock()
	c.stats.Sent++
	c.mu.Unlock()
	if int(seq)%ReportEvery == 0 {
		sr, _ := rtcp.Marshal([]rtcp.Packet{&rtcp.SenderReport{SSRC: ssrc, RTPTime: ts}})
		if _, err := c.pc.WriteTo(sr, c.rtcpTo); err == nil {
			c.mu.Lock()
			c.stats.RTCPSent++
			c.mu.Unlock()
		}
	}
	return len(p), nil
}

// Read returns the payload of the next RTP packet received; RTCP packets and garbage are counted
// or dropped, never returned.
func (c *Conn) Read(p []byte) (int, error) {
	for {
		n, _, err := c.pc.ReadFrom(c.readBuf)
		if err != nil {
			return 0, err
		}
		// RFC 5761: the second octet of RTCP is 192-223
		if n >= 2 && c.readBuf[1] >= 192 && c.readBuf[1] <= 223 {
			c.mu.Lock()
			c.stats.RTCPReceived++
			c.mu.Unlock()
			continue
		}
		var pkt rtp.Packet
		if err := pkt.Unmarshal(c.readBuf[:n]); err != nil {
			continue
		}
		c.mu.Lock()
		seq := int64(pkt.SequenceNumber)
		if c.stats.Received == 0 {
			c.firstSeq, c.maxSeq = seq, seq
		} else if ext := c.maxSeq + int64(int16(pkt.SequenceNumber-uint16(c.maxSeq))); ext > c.maxSeq {
			c.maxSeq = ext
		}
		c.stats.Received++
		c.mu.Unlock()
		return copy(p, pkt.Payload), nil
	}
}

// Stats returns the counters of the session.
func (c *Conn) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.stats
	if s.Received > 0 {
		s.Lost = max(0, int(c.maxSeq-c.firstSeq+1)-s.Received)
	}
	return s
}

// Close ends the session and closes the packet conn.
func (c *Conn) Close() error {
	var err error
	c.closeOnce.Do(func() { err = c.pc.Close() })
	return err
}

// LocalAddr returns the local address of the packet conn.
func (c *Conn) LocalAddr() net.Addr { return c.pc.LocalAddr() }

// RemoteAddr returns the peer's RTP address.
func (c *Conn) RemoteAddr() net.Addr { return c.rtpTo }

// SetDeadline sets the deadlines of the packet conn.
func (c *Conn) SetDeadline(t time.Time) error { return c.pc.SetDeadline(t) }

// SetReadDeadline sets the read deadline of the packet conn.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.pc.SetReadDeadline(t) }

// SetWriteDeadline sets the write deadline of the packet conn.
func (c *Conn) SetWriteDeadline(t time.Time) error { return c.pc.SetWriteDeadline(t) }
