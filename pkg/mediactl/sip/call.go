package sip

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
	"github.com/pion/sdp/v3"

	mediartp "github.com/l7mp/stunner/v2/pkg/mediactl/rtp"
)

// The G.711 payload types the phone speaks.
const (
	payloadPCMU uint8 = 0
	payloadPCMA uint8 = 8
)

// stream is the audio stream of an SDP.
type stream struct {
	addr           netip.Addr
	port, rtcpPort int
	mux            bool
	payloads       []uint8 // the G.711 ones, in order
}

// Call is a call of a phone, and a net.Conn of its media: its writes leave as the payloads of RTP
// packets to the peer's media address, its reads return the payloads of the RTP packets received,
// both on a single socket. Closing it hangs up.
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
	media        *mediartp.Conn // the RTP session, once the call is answered
	acknowledged bool
	endReason    string

	ended   chan struct{}
	endOnce sync.Once
	hangup  sync.Once // one BYE, however many hang up
	byeErr  error
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

// Stats returns the RTP and RTCP counters of the call.
func (c *Call) Stats() mediartp.Stats {
	c.mu.Lock()
	media := c.media
	c.mu.Unlock()
	if media == nil {
		return mediartp.Stats{}
	}
	return media.Stats()
}

// Read returns the payload of the next RTP packet of the call.
func (c *Call) Read(p []byte) (int, error) { return c.session().Read(p) }

// Write sends p as the payload of the next RTP packet of the call.
func (c *Call) Write(p []byte) (int, error) { return c.session().Write(p) }

// Close hangs up the call, bounded by CloseTimeout, unless it has ended already.
func (c *Call) Close() error {
	select {
	case <-c.ended:
		return nil
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), CloseTimeout)
	defer cancel()
	return c.Hangup(ctx)
}

// LocalAddr returns the address of the call's media socket.
func (c *Call) LocalAddr() net.Addr { return c.conn.LocalAddr() }

// RemoteAddr returns the peer's RTP address.
func (c *Call) RemoteAddr() net.Addr { return net.UDPAddrFromAddrPort(c.RemoteMedia()) }

// SetDeadline sets the deadlines of the media socket.
func (c *Call) SetDeadline(t time.Time) error { return c.conn.SetDeadline(t) }

// SetReadDeadline sets the read deadline of the media socket.
func (c *Call) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// SetWriteDeadline sets the write deadline of the media socket.
func (c *Call) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

// session returns the RTP session of the call; one is made when the call is answered, before the
// call is handed out.
func (c *Call) session() *mediartp.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.media
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
	fmt.Fprintf(b, "v=0\r\no=- %d 1 IN %s %s\r\ns=phone\r\nc=IN %s %s\r\nt=0 0\r\nm=audio %d RTP/AVP",
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

// startMedia makes the RTP session of the call, to the peer's media address in the first payload
// type the peer offered.
func (c *Call) startMedia() {
	c.mu.Lock()
	defer c.mu.Unlock()
	rtpTo := net.UDPAddrFromAddrPort(netip.AddrPortFrom(c.remote.addr, uint16(c.remote.port)))
	rtcpTo := net.UDPAddrFromAddrPort(netip.AddrPortFrom(c.remote.addr, uint16(c.remote.rtcpPort)))
	c.media = mediartp.NewConn(c.conn, rtpTo, rtcpTo, c.remote.payloads[0])
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

// Hangup ends the call with a BYE, unless the call has ended. A hangup while another is in flight
// waits for that one and returns its result.
func (c *Call) Hangup(ctx context.Context) error {
	c.hangup.Do(func() {
		select {
		case <-c.ended:
			return
		default:
		}
		res, err := c.p.txl.Do(ctx, c.inDialog(sip.BYE), c.sig)
		c.end("local hangup")
		if err == nil && !res.IsSuccess() {
			err = &StatusError{Code: res.StatusCode, Reason: res.Reason}
		}
		c.byeErr = err
	})
	return c.byeErr
}

// end ends the call locally.
func (c *Call) end(reason string) {
	c.endOnce.Do(func() {
		c.mu.Lock()
		c.endReason = reason
		c.mu.Unlock()
		// ended before the media closes: a reader that fails on it sees the call over
		close(c.ended)
		_ = c.conn.Close()
		if c.ownsSig {
			_ = c.sig.Close()
		}
		c.p.mu.Lock()
		if c.p.calls[c.id] == c {
			delete(c.p.calls, c.id)
		}
		c.p.mu.Unlock()
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
