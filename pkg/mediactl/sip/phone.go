// Package sip is a minimal SIP phone, a dialer and a listener of calls: Dial and DialContext
// return a call once it is answered, Accept the next call the phone answered. A call is a net.Conn
// of its RTP media, so any packet stream runs over it: mediactl.Stream sends, mediactl.Echo
// answers.
package sip

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/pion/logging"
	"github.com/pion/transport/v5"
	"github.com/pion/transport/v5/stdnet"

	"github.com/l7mp/stunner/v2/internal/proto/siptx"
	"github.com/l7mp/stunner/v2/pkg/udp"
)

// user is the user part of the phone's SIP URI.
const user = "phone"

// Config is the configuration of a phone.
type Config struct {
	// Net is the network the phone runs over; nil is the host network.
	Net transport.Net
	// Addr is the ip:port the phone answers calls at over UDP; empty picks a port on every
	// interface.
	Addr string
	// Username and Password answer the digest challenges of the INVITEs the phone sends.
	Username, Password string
	// AnswerDelay is how long the phone rings before it answers.
	AnswerDelay time.Duration
	// Registrar makes the phone a registrar too: it takes REGISTERs and dials an AOR registered
	// with it over the flow the AOR registered on (RFC 5626).
	Registrar bool
	// LoggerFactory makes the phone's logger; nil logs nothing.
	LoggerFactory logging.LoggerFactory
}

// StatusError is a final non-2xx response to a request.
type StatusError struct {
	Code   int
	Reason string
}

func (e *StatusError) Error() string { return fmt.Sprintf("%d %s", e.Code, e.Reason) }

// Phone is a SIP phone over UDP. Every peer has a conn of its own: a dialed call its dialed conn,
// the callers the conns the listener accepts. The local address of a conn is the phone's address
// on it.
type Phone struct {
	cfg      Config
	net      transport.Net
	listener net.Listener
	txl      *siptx.Layer
	log      logging.LeveledLogger

	mu         sync.Mutex
	calls      map[string]*Call // by Call-ID
	conns      map[net.Conn]bool
	incoming   chan *Call
	bindings   map[string]binding // the registered AORs, by user@host
	registered chan Registration

	done chan struct{}
	once sync.Once
}

// New creates a phone answering calls at its address.
func New(cfg Config) (*Phone, error) {
	n := cfg.Net
	if n == nil {
		var err error
		if n, err = stdnet.NewNet(); err != nil {
			return nil, err
		}
	}
	lf := cfg.LoggerFactory
	if lf == nil {
		lf = &logging.DefaultLoggerFactory{DefaultLogLevel: logging.LogLevelDisabled}
	}
	addr := cfg.Addr
	if addr == "" {
		addr = ":0"
	}
	l, err := udp.ListenConfig{Net: n, ConnectBack: true}.Listen("udp", addr)
	if err != nil {
		return nil, err
	}
	log := lf.NewLogger("sip-phone")
	p := &Phone{cfg: cfg, net: n, listener: l, txl: siptx.New(log), log: log,
		calls: map[string]*Call{}, conns: map[net.Conn]bool{}, incoming: make(chan *Call, 64),
		bindings: map[string]binding{}, registered: make(chan Registration, 64),
		done: make(chan struct{})}
	p.txl.OnRequest(p.handle)
	p.txl.OnResponse(func(res *sip.Response, _ net.Conn) {
		// a 2xx retransmitted after its transaction ended
		if c := p.call(res.CallID()); c != nil && res.IsSuccess() && res.CSeq().MethodName == sip.INVITE {
			c.ack()
		}
	})
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go p.serve(conn)
		}
	}()
	return p, nil
}

// Addr returns the address the phone answers calls at.
func (p *Phone) Addr() net.Addr { return p.listener.Addr() }

// CloseTimeout bounds the BYE of a call closed as a conn.
var CloseTimeout = 5 * time.Second

// Incoming returns the calls the phone answered, once acknowledged.
func (p *Phone) Incoming() <-chan *Call { return p.incoming }

// Accept returns the next call the phone answered, once acknowledged: the phone is a
// net.Listener of calls. It fails once the phone is closed.
func (p *Phone) Accept() (net.Conn, error) {
	select {
	case c := <-p.incoming:
		return c, nil
	case <-p.done:
		return nil, net.ErrClosed
	}
}

// DialContext calls the SIP URI of address, over UDP whatever the network: the phone is a dialer
// of calls.
func (p *Phone) DialContext(ctx context.Context, _, address string) (net.Conn, error) {
	c, err := p.Dial(ctx, address)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// Registration is a REGISTER a registrar phone took.
type Registration struct {
	// AOR is the address of record, Contact the address it is reached at.
	AOR, Contact sip.Uri
	// Expires is the expiry the phone granted, zero for a removed binding.
	Expires time.Duration
}

// Registered returns the REGISTERs a registrar phone took.
func (p *Phone) Registered() <-chan Registration { return p.registered }

// binding is an AOR registered with a registrar phone: the flow it registered on and its Contact.
type binding struct {
	conn    net.Conn
	contact sip.Uri
}

// Close ends every call locally, without a BYE, and stops the phone.
func (p *Phone) Close() error {
	p.once.Do(func() {
		close(p.done)
		_ = p.listener.Close()
		p.mu.Lock()
		calls := make([]*Call, 0, len(p.calls))
		for _, c := range p.calls {
			calls = append(calls, c)
		}
		for conn := range p.conns {
			_ = conn.Close()
		}
		p.mu.Unlock()
		for _, c := range calls {
			c.end("phone closed")
		}
	})
	return nil
}

// serve runs the transactions of the SIP conn of a peer until it closes.
func (p *Phone) serve(conn net.Conn) {
	p.mu.Lock()
	p.conns[conn] = true
	p.mu.Unlock()
	_ = p.txl.Serve(conn, false)
	_ = conn.Close()
	p.mu.Lock()
	delete(p.conns, conn)
	p.mu.Unlock()
}

func (p *Phone) call(id *sip.CallIDHeader) *Call {
	if id == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[id.Value()]
}

// handle answers the requests of the phone's peers.
func (p *Phone) handle(req *sip.Request, tx *siptx.ServerTx, conn net.Conn) {
	if tx == nil {
		if c := p.call(req.CallID()); c != nil {
			c.acked()
		}
		return
	}
	respond := func(code int, reason string) {
		_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
	}
	switch req.Method {
	case sip.INVITE:
		if tag, _ := req.To().Params.Get("tag"); tag != "" {
			respond(sip.StatusNotAcceptableHere, "Not Acceptable Here")
			return
		}
		p.answer(req, tx, conn)
	case sip.BYE:
		c := p.call(req.CallID())
		if c == nil {
			respond(sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist")
			return
		}
		respond(sip.StatusOK, "OK")
		c.end("remote hangup")
	case sip.OPTIONS:
		respond(sip.StatusOK, "OK")
	case sip.REGISTER:
		if !p.cfg.Registrar {
			respond(sip.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		p.register(req, tx, conn)
	default:
		respond(sip.StatusMethodNotAllowed, "Method Not Allowed")
	}
}

// Dial calls a SIP URI and returns the call once it is answered. An AOR
// registered with a registrar phone is called at its Contact over the flow it registered on.
// Cancelling the context before the answer cancels the call.
func (p *Phone) Dial(ctx context.Context, uri string) (*Call, error) {
	var target sip.Uri
	if err := sip.ParseUri(uri, &target); err != nil {
		return nil, err
	}
	p.mu.Lock()
	b, registered := p.bindings[target.User+"@"+target.Host]
	p.mu.Unlock()
	conn := b.conn
	if registered {
		target = b.contact
	} else {
		var err error
		if conn, err = p.net.Dial("udp", sip.NewRequest(sip.INVITE, target).Destination()); err != nil {
			return nil, err
		}
		go p.serve(conn)
	}
	c, err := p.newCall(randomHex(12), conn, true, !registered)
	if err != nil {
		if !registered {
			_ = conn.Close()
		}
		return nil, err
	}
	local := addrPortOf(conn.LocalAddr())
	c.mu.Lock()
	c.localTag, c.remoteTarget = randomHex(6), target
	c.localURI = sip.Uri{Scheme: "sip", User: user, Host: local.Addr().String(), Port: int(local.Port())}
	c.remoteURI = target
	c.mu.Unlock()
	offer := c.sdp([]uint8{payloadPCMU, payloadPCMA}, true)

	var auth sip.Header
	for {
		c.mu.Lock()
		c.cseq++
		seq := c.cseq
		c.mu.Unlock()
		req := sip.NewRequest(sip.INVITE, target)
		req.AppendHeader(via(conn))
		from := &sip.FromHeader{Address: c.localURI, Params: sip.NewParams()}
		from.Params.Add("tag", c.localTag)
		req.AppendHeader(from)
		req.AppendHeader(&sip.ToHeader{Address: target, Params: sip.NewParams()})
		cid := sip.CallIDHeader(c.id)
		req.AppendHeader(&cid)
		req.AppendHeader(&sip.CSeqHeader{SeqNo: seq, MethodName: sip.INVITE})
		mf := sip.MaxForwardsHeader(70)
		req.AppendHeader(&mf)
		req.AppendHeader(contact(conn))
		if auth != nil {
			req.AppendHeader(auth)
		}
		ct := sip.ContentTypeHeader("application/sdp")
		req.AppendHeader(&ct)
		req.SetBody(offer)

		tx, err := p.txl.Request(req, conn)
		if err != nil {
			c.end(err.Error())
			return nil, err
		}
		// our ACK got lost: the 2xx comes again
		tx.OnRetransmission(func(*sip.Response) { c.ack() })
		res, err := tx.Wait(ctx)
		if err != nil {
			c.end(err.Error())
			return nil, err
		}
		switch {
		case res.IsSuccess():
			if err := c.answered(res); err != nil {
				c.end(err.Error())
				return nil, err
			}
			c.ack()
			c.startMedia()
			return c, nil
		case (res.StatusCode == sip.StatusUnauthorized || res.StatusCode == sip.StatusProxyAuthRequired) &&
			auth == nil && p.cfg.Username != "":
			chal, name := res.GetHeader("WWW-Authenticate"), "Authorization"
			if res.StatusCode == sip.StatusProxyAuthRequired {
				chal, name = res.GetHeader("Proxy-Authenticate"), "Proxy-Authorization"
			}
			if chal == nil {
				c.end("challenge without a header")
				return nil, &StatusError{Code: res.StatusCode, Reason: res.Reason}
			}
			auth = sip.NewHeader(name, authorize(p.cfg.Username, p.cfg.Password, chal.Value(),
				string(sip.INVITE), target.String()))
		default:
			c.end(fmt.Sprintf("%d %s", res.StatusCode, res.Reason))
			return nil, &StatusError{Code: res.StatusCode, Reason: res.Reason}
		}
	}
}

// answer answers an INVITE with a call.
func (p *Phone) answer(req *sip.Request, tx *siptx.ServerTx, conn net.Conn) {
	respond := func(code int, reason string) {
		_ = tx.Respond(sip.NewResponseFromRequest(req, code, reason, nil))
	}
	remote, err := parseSDP(req.Body())
	if err != nil {
		respond(sip.StatusNotAcceptableHere, "Not Acceptable Here")
		return
	}
	c, err := p.newCall(req.CallID().Value(), conn, false, false)
	if err != nil {
		respond(sip.StatusInternalServerError, "Server Error")
		return
	}
	c.mu.Lock()
	c.localTag = randomHex(6)
	c.remoteTag, _ = req.From().Params.Get("tag")
	c.localURI, c.remoteURI = req.To().Address, req.From().Address
	if ct := req.Contact(); ct != nil {
		c.remoteTarget = ct.Address
	}
	for _, h := range req.GetHeaders("Record-Route") {
		c.routeSet = append(c.routeSet, h.(*sip.RecordRouteHeader).Address)
	}
	c.remote = remote
	c.mu.Unlock()

	ringing := sip.NewResponseFromRequest(req, sip.StatusRinging, "Ringing", nil)
	ringing.To().Params.Add("tag", c.localTag)
	_ = tx.Respond(ringing)
	select {
	case <-time.After(p.cfg.AnswerDelay):
	case <-tx.Cancelled():
		c.end("cancelled")
		return
	case <-p.done:
		return
	}

	// the media starts before the 2xx goes out: its ACK hands the call to Incoming, whose reader
	// must find the RTP session
	c.startMedia()
	res := sip.NewSDPResponseFromRequest(req, c.sdp(remote.payloads[:1], remote.mux))
	res.To().Params.Add("tag", c.localTag)
	res.AppendHeader(contact(conn))
	if err := tx.Respond(res); err != nil {
		c.end(err.Error())
	}
}

// register takes a REGISTER: it binds the AOR to the flow and the Contact the request came with,
// or removes the binding for a zero expiry, and grants the expiry asked for.
func (p *Phone) register(req *sip.Request, tx *siptx.ServerTx, conn net.Conn) {
	ct := req.Contact()
	if ct == nil {
		_ = tx.Respond(sip.NewResponseFromRequest(req, sip.StatusBadRequest, "Bad Request", nil))
		return
	}
	secs := uint64(3600)
	if h := req.GetHeader("Expires"); h != nil {
		secs, _ = strconv.ParseUint(h.Value(), 10, 32)
	}
	if e, ok := ct.Params.Get("expires"); ok {
		secs, _ = strconv.ParseUint(e, 10, 32)
	}
	aor := req.To().Address
	key := aor.User + "@" + aor.Host
	p.mu.Lock()
	if secs == 0 {
		delete(p.bindings, key)
	} else {
		p.bindings[key] = binding{conn: conn, contact: ct.Address}
	}
	p.mu.Unlock()

	res := sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil)
	granted := ct.Clone()
	granted.Params.Add("expires", strconv.FormatUint(secs, 10))
	res.AppendHeader(granted)
	_ = tx.Respond(res)
	select {
	case p.registered <- Registration{AOR: aor, Contact: ct.Address, Expires: time.Duration(secs) * time.Second}:
	default:
	}
}

// Ping sends an OPTIONS to the URI on a flow of its own and returns nil on a 2xx: the SIP peer at
// the URI's address takes requests. A URI with no user part addresses a proxy itself.
func (p *Phone) Ping(ctx context.Context, uri string) error {
	var target sip.Uri
	if err := sip.ParseUri(uri, &target); err != nil {
		return err
	}
	req := sip.NewRequest(sip.OPTIONS, target)
	conn, err := p.net.Dial("udp", req.Destination())
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck
	go p.serve(conn)

	local := addrPortOf(conn.LocalAddr())
	req.AppendHeader(via(conn))
	from := &sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: user, Host: local.Addr().String(),
		Port: int(local.Port())}, Params: sip.NewParams()}
	from.Params.Add("tag", randomHex(6))
	req.AppendHeader(from)
	req.AppendHeader(&sip.ToHeader{Address: target, Params: sip.NewParams()})
	cid := sip.CallIDHeader(randomHex(12))
	req.AppendHeader(&cid)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: sip.OPTIONS})
	mf := sip.MaxForwardsHeader(70)
	req.AppendHeader(&mf)
	req.SetBody(nil)

	res, err := p.txl.Do(ctx, req, conn)
	if err != nil {
		return err
	}
	if !res.IsSuccess() {
		return &StatusError{Code: res.StatusCode, Reason: res.Reason}
	}
	return nil
}

// via makes the Via of a request the phone sends on a conn.
func via(conn net.Conn) *sip.ViaHeader {
	local := addrPortOf(conn.LocalAddr())
	params := sip.NewParams()
	params.Add("branch", sip.RFC3261BranchMagicCookie+randomHex(8))
	params.Add("rport", "")
	return &sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "UDP",
		Host: local.Addr().String(), Port: int(local.Port()), Params: params}
}

// contact makes the phone's Contact on a conn.
func contact(conn net.Conn) *sip.ContactHeader {
	local := addrPortOf(conn.LocalAddr())
	return &sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: user,
		Host: local.Addr().String(), Port: int(local.Port())}}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
