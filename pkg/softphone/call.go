package softphone

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
)

// The G.711 payload types the phone speaks.
const (
	payloadPCMU uint8 = 0
	payloadPCMA uint8 = 8
)

// Stats are the media statistics of a call.
type Stats struct {
	// Sent and Received count the RTP packets.
	Sent, Received int
	// Lost is the number of RTP packets missing from the received sequence.
	Lost int
	// RTCPSent and RTCPReceived count the RTCP packets.
	RTCPSent, RTCPReceived int
}

// String summarizes the statistics.
func (s Stats) String() string {
	return fmt.Sprintf("sent=%d received=%d lost=%d rtcp-sent=%d rtcp-received=%d",
		s.Sent, s.Received, s.Lost, s.RTCPSent, s.RTCPReceived)
}

// stream is the audio stream of an SDP.
type stream struct {
	addr           netip.Addr
	port, rtcpPort int
	mux            bool
	payloads       []uint8 // the G.711 ones, in order
}

// Call is a call of a phone. A dialed call sends silence, an answered one echoes every RTP and
// RTCP packet it receives, both on a single socket.
type Call struct {
	p        *Phone
	id       string
	outbound bool
	sig      net.Conn // the SIP conn of the peer
	ownsSig  bool     // the call dialed sig and closes it at its end
	conn     net.PacketConn

	mu           sync.Mutex
	localTag     string
	remoteTag    string
	localURI     sip.Uri
	remoteURI    sip.Uri
	remoteTarget sip.Uri
	routeSet     []sip.Uri
	cseq         uint32
	inviteSeq    uint32
	remote       stream
	stats        Stats
	firstSeq     int64
	maxSeq       int64 // extended over wraps
	acknowledged bool
	endReason    string

	ended   chan struct{}
	endOnce sync.Once
}

// newCall makes a call over the SIP conn of a peer, its media bound at the conn's local address.
func (p *Phone) newCall(id string, sig net.Conn, outbound, ownsSig bool) (*Call, error) {
	ip := addrPortOf(sig.LocalAddr()).Addr().String()
	conn, err := p.net.ListenPacket("udp", net.JoinHostPort(ip, "0"))
	if err != nil {
		return nil, err
	}
	c := &Call{p: p, id: id, outbound: outbound, sig: sig, ownsSig: ownsSig, conn: conn,
		ended: make(chan struct{})}
	p.mu.Lock()
	p.calls[id] = c
	p.mu.Unlock()
	return c, nil
}

// ID returns the Call-ID of the call.
func (c *Call) ID() string { return c.id }

// Done is closed once the call ends.
func (c *Call) Done() <-chan struct{} { return c.ended }

// EndReason returns why the call ended.
func (c *Call) EndReason() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.endReason
}

// Stats returns the media statistics of the call.
func (c *Call) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.stats
	if s.Received > 0 {
		s.Lost = max(0, int(c.maxSeq-c.firstSeq+1)-s.Received)
	}
	return s
}

// RemoteMedia returns the address the call sends RTP to.
func (c *Call) RemoteMedia() netip.AddrPort {
	c.mu.Lock()
	defer c.mu.Unlock()
	return netip.AddrPortFrom(c.remote.addr, uint16(c.remote.port))
}

// String describes the call.
func (c *Call) String() string {
	return fmt.Sprintf("call %s local=%s remote=%s", c.id, c.conn.LocalAddr(), c.RemoteMedia())
}

// sdp makes the phone's SDP for the call.
func (c *Call) sdp(payloads []uint8, mux bool) []byte {
	local := addrPortOf(c.conn.LocalAddr())
	addr, port := local.Addr(), local.Port()
	family := "IP4"
	if addr.Is6() {
		family = "IP6"
	}
	b := &strings.Builder{}
	fmt.Fprintf(b, "v=0\r\no=- %d 1 IN %s %s\r\ns=softphone\r\nc=IN %s %s\r\nt=0 0\r\nm=audio %d RTP/AVP",
		time.Now().UnixNano()>>1, family, addr, family, addr, port)
	for _, pt := range payloads {
		fmt.Fprintf(b, " %d", pt)
	}
	b.WriteString("\r\n")
	for _, pt := range payloads {
		name := "PCMU"
		if pt == payloadPCMA {
			name = "PCMA"
		}
		fmt.Fprintf(b, "a=rtpmap:%d %s/8000\r\n", pt, name)
	}
	b.WriteString("a=ptime:20\r\n")
	if mux {
		b.WriteString("a=rtcp-mux\r\n")
	}
	b.WriteString("a=sendrecv\r\n")
	return []byte(b.String())
}

// parseSDP reads the first audio stream of an SDP.
func parseSDP(b []byte) (stream, error) {
	d := &sdp.SessionDescription{}
	if err := d.Unmarshal(b); err != nil {
		return stream{}, err
	}
	for _, m := range d.MediaDescriptions {
		if m.MediaName.Media != "audio" {
			continue
		}
		ci := d.ConnectionInformation
		if m.ConnectionInformation != nil {
			ci = m.ConnectionInformation
		}
		if ci == nil || ci.Address == nil {
			return stream{}, errors.New("no connection address in SDP")
		}
		addr, err := netip.ParseAddr(ci.Address.Address)
		if err != nil {
			return stream{}, fmt.Errorf("bad connection address in SDP: %w", err)
		}
		s := stream{addr: addr, port: m.MediaName.Port.Value, rtcpPort: m.MediaName.Port.Value + 1}
		for _, f := range m.MediaName.Formats {
			if pt, err := strconv.Atoi(f); err == nil && (uint8(pt) == payloadPCMU || uint8(pt) == payloadPCMA) {
				s.payloads = append(s.payloads, uint8(pt))
			}
		}
		for _, a := range m.Attributes {
			switch a.Key {
			case "rtcp-mux":
				s.mux = true
			case "rtcp":
				// a=rtcp:port [IN IP4 addr]
				if p, err := strconv.Atoi(strings.Fields(a.Value)[0]); err == nil {
					s.rtcpPort = p
				}
			}
		}
		if s.mux {
			s.rtcpPort = s.port
		}
		if len(s.payloads) == 0 {
			return stream{}, errors.New("no G.711 in SDP")
		}
		return s, nil
	}
	return stream{}, errors.New("no audio stream in SDP")
}

// answered applies the 2xx answering the call's INVITE.
func (c *Call) answered(res *sip.Response) error {
	remote, err := parseSDP(res.Body())
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remote = remote
	c.remoteTag, _ = res.To().Params.Get("tag")
	if ct := res.Contact(); ct != nil {
		c.remoteTarget = ct.Address
	}
	rr := res.GetHeaders("Record-Route")
	for i := len(rr) - 1; i >= 0; i-- {
		c.routeSet = append(c.routeSet, rr[i].(*sip.RecordRouteHeader).Address)
	}
	c.inviteSeq = c.cseq
	return nil
}

// acked takes the ACK of the call's 2xx and hands the call to Incoming.
func (c *Call) acked() {
	c.mu.Lock()
	first := !c.acknowledged
	c.acknowledged = true
	c.mu.Unlock()
	if first {
		select {
		case c.p.incoming <- c:
		default:
			c.p.log.Warnf("answered call %s not delivered: Incoming is full", c.id)
		}
	}
}

// startMedia starts reading, and for a dialed call sending, the media of the call.
func (c *Call) startMedia() {
	go c.readMedia()
	if c.outbound {
		go c.sendSilence()
	}
}

func (c *Call) readMedia() {
	buf := make([]byte, 1500)
	for {
		n, _, err := c.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		// RFC 5761: the second octet of RTCP is 192-223
		isRTCP := n >= 2 && buf[1] >= 192 && buf[1] <= 223
		var h rtp.Header
		if _, err := h.Unmarshal(buf[:n]); !isRTCP && err != nil {
			continue
		}
		c.mu.Lock()
		dst := netip.AddrPortFrom(c.remote.addr, uint16(c.remote.port))
		if isRTCP {
			c.stats.RTCPReceived++
			dst = netip.AddrPortFrom(c.remote.addr, uint16(c.remote.rtcpPort))
		} else {
			seq := int64(h.SequenceNumber)
			if c.stats.Received == 0 {
				c.firstSeq, c.maxSeq = seq, seq
			} else if ext := c.maxSeq + int64(int16(h.SequenceNumber-uint16(c.maxSeq))); ext > c.maxSeq {
				c.maxSeq = ext
			}
			c.stats.Received++
		}
		if !c.outbound {
			if _, err := c.conn.WriteTo(buf[:n], net.UDPAddrFromAddrPort(dst)); err == nil && isRTCP {
				c.stats.RTCPSent++
			} else if err == nil {
				c.stats.Sent++
			}
		}
		c.mu.Unlock()
	}
}

// sendSilence sends a packet of G.711 silence every 20 ms and a sender report every second.
func (c *Call) sendSilence() {
	c.mu.Lock()
	pt := c.remote.payloads[0]
	rtpAddr := net.UDPAddrFromAddrPort(netip.AddrPortFrom(c.remote.addr, uint16(c.remote.port)))
	rtcpAddr := net.UDPAddrFromAddrPort(netip.AddrPortFrom(c.remote.addr, uint16(c.remote.rtcpPort)))
	c.mu.Unlock()
	fill := byte(0xff)
	if pt == payloadPCMA {
		fill = 0xd5
	}
	silence := make([]byte, 160)
	for i := range silence {
		silence[i] = fill
	}
	pkt := rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: pt, SSRC: uint32(time.Now().UnixNano())},
		Payload: silence}
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-c.ended:
			return
		case <-t.C:
		}
		pkt.SequenceNumber++
		pkt.Timestamp += uint32(len(silence))
		b, _ := pkt.Marshal()
		if _, err := c.conn.WriteTo(b, rtpAddr); err == nil {
			c.mu.Lock()
			c.stats.Sent++
			c.mu.Unlock()
		}
		if pkt.SequenceNumber%50 != 0 {
			continue
		}
		b, _ = rtcp.Marshal([]rtcp.Packet{&rtcp.SenderReport{SSRC: pkt.SSRC, RTPTime: pkt.Timestamp}})
		if _, err := c.conn.WriteTo(b, rtcpAddr); err == nil {
			c.mu.Lock()
			c.stats.RTCPSent++
			c.mu.Unlock()
		}
	}
}

// inDialog makes an in-dialog request of the call.
func (c *Call) inDialog(method sip.RequestMethod) *sip.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	req := sip.NewRequest(method, c.remoteTarget)
	req.AppendHeader(via(c.sig))
	from := &sip.FromHeader{Address: c.localURI, Params: sip.NewParams()}
	from.Params.Add("tag", c.localTag)
	to := &sip.ToHeader{Address: c.remoteURI, Params: sip.NewParams()}
	to.Params.Add("tag", c.remoteTag)
	req.AppendHeader(from)
	req.AppendHeader(to)
	cid := sip.CallIDHeader(c.id)
	req.AppendHeader(&cid)
	seq := c.inviteSeq
	if method != sip.ACK {
		c.cseq++
		seq = c.cseq
	}
	req.AppendHeader(&sip.CSeqHeader{SeqNo: seq, MethodName: method})
	mf := sip.MaxForwardsHeader(70)
	req.AppendHeader(&mf)
	for _, r := range c.routeSet {
		req.AppendHeader(&sip.RouteHeader{Address: r})
	}
	req.AppendHeader(contact(c.sig))
	req.SetBody(nil)
	return req
}

// ack acknowledges the 2xx of the call's INVITE, once the call has its dialog.
func (c *Call) ack() {
	c.mu.Lock()
	dialog := c.remoteTag != ""
	c.mu.Unlock()
	if dialog {
		_ = c.p.txl.Write(c.sig, c.inDialog(sip.ACK))
	}
}

// Hangup ends the call with a BYE.
func (c *Call) Hangup(ctx context.Context) error {
	res, err := c.p.txl.Do(ctx, c.inDialog(sip.BYE), c.sig)
	c.end("local hangup")
	if err == nil && !res.IsSuccess() {
		err = &StatusError{Code: res.StatusCode, Reason: res.Reason}
	}
	return err
}

// end ends the call locally.
func (c *Call) end(reason string) {
	c.endOnce.Do(func() {
		c.mu.Lock()
		c.endReason = reason
		c.mu.Unlock()
		_ = c.conn.Close()
		if c.ownsSig {
			_ = c.sig.Close()
		}
		c.p.mu.Lock()
		if c.p.calls[c.id] == c {
			delete(c.p.calls, c.id)
		}
		c.p.mu.Unlock()
		close(c.ended)
	})
}

// addrPortOf returns the address and port of a UDP address.
func addrPortOf(a net.Addr) netip.AddrPort {
	if u, ok := a.(*net.UDPAddr); ok {
		ap := u.AddrPort()
		return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
	}
	ap, _ := netip.ParseAddrPort(a.String())
	return ap
}
