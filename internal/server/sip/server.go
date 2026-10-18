// Package sip implements the SIP server, a transaction-stateful proxy of inbound calls.
package sip

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pion/stun/v4"
	pionturn "github.com/pion/turn/v5"

	sipmsg "github.com/emiago/sipgo/sip"
	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/ng"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	"github.com/l7mp/stunner/v2/internal/server/balancer"
	turnsrv "github.com/l7mp/stunner/v2/internal/server/turn"
	"github.com/l7mp/stunner/v2/internal/siptx"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

var (
	// PeerTimeout is how long a SIP peer conn lives without traffic.
	PeerTimeout = 30 * time.Minute
	// MediaProxyTimeout bounds an ng request to the media proxy.
	MediaProxyTimeout = 5 * time.Second
)

const (
	sideExternal = ng.DirectionExternal
	sideInternal = ng.DirectionInternal
	// routeParam marks the Record-Routes and the Vias of the proxy, with a flow token as its
	// value.
	routeParam = "stnr"
)

var (
	errNoRoute   = errors.New("no cluster routes the request")
	errForbidden = errors.New("a request without a route of the proxy on a dialed conn")
	errGone      = errors.New("the conn of the flow token is gone")
)

// Server is a sip server.
type Server struct {
	server.Base
	rt  *objruntime.Runtime
	log logging.LeveledLogger
	txl *siptx.Layer

	mu      sync.Mutex
	running bool
	lastID  uint64
	peers   map[uint64]*peer
	// dialed are the dialed peers by remote address, reused by the requests to that address
	dialed    map[netip.AddrPort]*peer
	conntrack *server.Conntrack
	media     *ng.Client
	calls     map[string]*call // the calls the server admitted, by Call-ID
	stop      chan struct{}
	// key signs the flow tokens and the digest nonces of the proxy
	key []byte
	// instance is the RFC 5626 instance id the server registers with
	instance string

	quota  *turnsrv.Quota
	gate   pionturn.QuotaHandler
	events EventHandler

	// the options of the server: Reconcile writes them, the handlers read them, under confMu
	confMu     sync.RWMutex
	auth       string
	publicAddr string
	publicPort int
	mediaProxy string
	lbPolicy   stnrv2.LBPolicy   // the load-balancing policy of lb
	lb         balancer.Balancer // chooses the in-cluster target of a new dialog
	// registrations are kept up over the flows of the listeners labeled with their names
	registrations []stnrv2.SIPRegistrationConfig
}

// call is a call the server admitted against the quota, established once created is set.
type call struct {
	event   CallEvent
	created bool
}

// CallSweepInterval is how often the server asks the media proxy whether its calls still run.
var CallSweepInterval = 30 * time.Second

// constructor builds the SIP server of a validated config.
var constructor = func(conf *stnrv2.ServerConfig, rt *objruntime.Runtime) (objruntime.Object, error) {
	s, err := newServer(conf, rt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// New creates the SIP server of a validated config.
func New(conf *stnrv2.ServerConfig, rt *objruntime.Runtime) (objruntime.Object, error) {
	return constructor(conf, rt)
}

// peer is a SIP peer conn: one a listener handed over, or one the server dialed. Reading it keeps
// its conntrack entry alive.
type peer struct {
	api.Conn
	e      *server.Entry
	id     uint64
	dialed bool
	// registration names the registration the peer's flow carries, empty for none
	registration string
	// answered carries the transaction IDs of the STUN Binding answers on a datagram
	// registration flow, nil on other flows
	answered chan [stun.TransactionIDSize]byte
	done     chan struct{} // closed with the conn
}

func (p *peer) Read(b []byte) (int, error) {
	n, err := p.Conn.Read(b)
	if err == nil {
		p.e.Touch()
		// the registrar's answer to a keepalive of the flow (RFC 5626 section 4.4.2)
		if p.answered != nil && stun.IsMessage(b[:n]) {
			m := &stun.Message{Raw: append([]byte(nil), b[:n]...)}
			if m.Decode() == nil && m.Type == stun.BindingSuccess {
				select {
				case p.answered <- m.TransactionID:
				default:
				}
			}
		}
	}
	return n, err
}

// stream tells whether the peer's conn is a stream, framed by Content-Length, or datagrams.
func (p *peer) stream() bool {
	return p.Tag().Proto == stnrv2.ProtocolTCP || p.Tag().Proto == stnrv2.ProtocolTLS
}

// side is the side of the media proxy the peer is on.
func (p *peer) side() string {
	if p.dialed {
		return sideInternal
	}
	return sideExternal
}

// newServer creates the SIP server of a validated config.
func newServer(conf *stnrv2.ServerConfig, rt *objruntime.Runtime) (*Server, error) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	log := rt.Logger.NewLogger(fmt.Sprintf("server-%s", conf.Name))
	q := turnsrv.NewQuotaHandler(rt)
	s := &Server{
		rt:       rt,
		log:      log,
		txl:      siptx.New(log),
		key:      key,
		instance: uuid.NewString(),
		quota:    q,
		gate:     q.QuotaHandler(),
		events:   NewEventHandler(log, q),
	}
	s.txl.OnRequest(func(req *sipmsg.Request, tx *siptx.ServerTx, c net.Conn) {
		if tx == nil {
			s.forwardAck(req, c.(*peer))
			return
		}
		s.proxy(req, tx, c.(*peer))
	})
	s.txl.OnResponse(func(res *sipmsg.Response, _ net.Conn) { s.forwardResponse(res) })
	s.Init(conf)
	if err := s.Reconcile(conf); err != nil {
		return nil, err
	}
	return s, nil
}

// Reconcile applies the spec and the options to the next requests.
func (s *Server) Reconcile(conf stnrv2.Config) error {
	if err := s.Base.Reconcile(conf); err != nil {
		return err
	}
	req, _ := s.Desired(conf)
	policy, err := stnrv2.NewLBPolicy(req.SIP.LBPolicy)
	if err != nil {
		return err
	}
	s.confMu.Lock()
	defer s.confMu.Unlock()
	if policy != s.lbPolicy {
		lb, err := balancer.New(policy)
		if err != nil {
			return err
		}
		s.lb, s.lbPolicy = lb, policy
	}
	s.auth, s.mediaProxy = req.SIP.Auth, req.SIP.MediaProxy
	s.publicAddr, s.publicPort = req.SIP.PublicAddr, req.SIP.PublicPort
	s.registrations = slices.Clone(req.SIP.Registrations)
	return nil
}

func (s *Server) GetConfig() stnrv2.Config {
	c := s.Base.GetConfig().(*stnrv2.ServerConfig)
	s.confMu.RLock()
	defer s.confMu.RUnlock()
	c.SIP = &stnrv2.SIPServerConfig{Auth: s.auth, PublicAddr: s.publicAddr, PublicPort: s.publicPort,
		MediaProxy: s.mediaProxy, LBPolicy: s.lbPolicy.String(),
		Registrations: slices.Clone(s.registrations)}
	return c
}

func (s *Server) Status() stnrv2.Status {
	return &stnrv2.ServerStatus{ServerConfig: s.GetConfig().(*stnrv2.ServerConfig), Sessions: s.Sessions()}
}

func (s *Server) Start() error {
	s.mu.Lock()
	s.running = true
	s.peers, s.dialed = map[uint64]*peer{}, map[netip.AddrPort]*peer{}
	s.conntrack = server.NewConntrack(PeerTimeout)
	s.calls, s.stop = map[string]*call{}, make(chan struct{})
	stop := s.stop
	s.mu.Unlock()
	go s.sweep(stop)
	s.log.Infof("server %s: sip server running", s.Name())
	return nil
}

// Close closes every SIP peer conn and the media proxy conn.
func (s *Server) Close(_ bool) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	ct, media, calls := s.conntrack, s.media, s.calls
	s.media, s.calls = nil, map[string]*call{}
	close(s.stop)
	s.mu.Unlock()
	for _, c := range calls {
		s.release(c)
	}
	ct.Close()
	if media != nil {
		_ = media.Close()
	}
	return nil
}

// Sessions returns the number of calls the server admitted, set up or established.
func (s *Server) Sessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// Serve takes a SIP conn from a listener.
func (s *Server) Serve(c api.Conn) (api.Conn, api.Verdict, error) {
	if s.addPeer(c, false) == nil {
		return nil, api.Consumed, server.ErrNotServing
	}
	return nil, api.Consumed, nil
}

// addPeer tracks a SIP peer conn and reads it; nil when the server is not running.
func (s *Server) addPeer(c api.Conn, dialed bool) *peer {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	ct := s.conntrack
	s.lastID++
	e := &server.Entry{Client: c}
	p := &peer{Conn: c, e: e, id: s.lastID, dialed: dialed, done: make(chan struct{})}
	if !dialed {
		// a listener labels the flows a registration runs over
		p.registration = c.Tag().Labels[stnrv2.RegistrationLabel]
		if p.registration != "" && !p.stream() {
			p.answered = make(chan [stun.TransactionIDSize]byte, 1)
		}
	}
	s.peers[p.id] = p
	remote := addrPort(c.RemoteAddr())
	if dialed {
		s.dialed[remote] = p
	}
	s.mu.Unlock()
	e.Hooks.OnClose = func(string) {
		close(p.done)
		s.mu.Lock()
		delete(s.peers, p.id)
		if s.dialed[remote] == p {
			delete(s.dialed, remote)
		}
		s.mu.Unlock()
	}
	if !ct.Add(e) {
		return nil
	}
	go func() {
		if err := s.txl.Serve(p, p.stream()); err != nil {
			s.log.Debugf("SIP conn from %s closed: %s", c.RemoteAddr(), err.Error())
		}
		e.Close("peer closed")
	}()
	if p.registration != "" {
		go s.register(p, p.registration)
	}
	return p
}

// token returns the flow token of a peer: its id and a MAC over it.
func (s *Server) token(p *peer) string {
	id := strconv.FormatUint(p.id, 36)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte("flow:" + id))
	return id + "." + hex.EncodeToString(mac.Sum(nil)[:8])
}

// peerOf returns the live peer a flow token names, nil for a forged token or a closed conn.
func (s *Server) peerOf(token string) *peer {
	idStr, _, _ := strings.Cut(token, ".")
	id, err := strconv.ParseUint(idStr, 36, 64)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	p := s.peers[id]
	s.mu.Unlock()
	if p == nil || !hmac.Equal([]byte(s.token(p)), []byte(token)) {
		return nil
	}
	return p
}

// route strips the proxy's own Routes off a request and finds the peer it leaves on: the one its
// flow token names, else, for a new inbound request, the first cluster's choice.
func (s *Server) route(req *sipmsg.Request, in *peer) (*peer, error) {
	token := ""
	for r := req.Route(); r != nil; r = req.Route() {
		t, ours := r.Address.UriParams.Get(routeParam)
		if !ours {
			break
		}
		token = t
		req.RemoveHeader("Route")
	}
	if token != "" {
		if p := s.peerOf(token); p != nil {
			return p, nil
		}
		return nil, errGone
	}
	if in.dialed {
		return nil, errForbidden
	}

	s.confMu.RLock()
	lb := s.lb
	s.confMu.RUnlock()
	cluster, dst, ok := lb.Next(s.rt, s.Clusters())
	if !ok {
		return nil, errNoRoute
	}
	d, ok := s.rt.Dialer(cluster)
	if !ok {
		return nil, errNoRoute
	}
	s.mu.Lock()
	p, ok := s.dialed[dst]
	s.mu.Unlock()
	if ok {
		return p, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := d.Dial(ctx, nil, dst.String())
	if err != nil {
		return nil, err
	}
	if p = s.addPeer(c, true); p == nil {
		_ = c.Close()
		return nil, server.ErrNotServing
	}
	return p, nil
}

// addr returns the proxy's SIP address towards a peer: towards a carrier, the public address of
// the listener of the carrier's conn, else that of the server, where set, else the local address
// of the conn. A registration's flow is reached over the flow itself: its local address, as in the
// Contact it registered.
func (s *Server) addr(p *peer) (string, int) {
	local := addrPort(p.LocalAddr())
	host, port := local.Addr().String(), int(local.Port())
	if p.dialed || p.registration != "" {
		return host, port
	}
	s.confMu.RLock()
	publicAddr, publicPort := s.publicAddr, s.publicPort
	s.confMu.RUnlock()
	if tag := p.Tag(); tag.PublicAddr != "" {
		publicAddr, publicPort = tag.PublicAddr, tag.PublicPort
	}
	if publicAddr != "" {
		host = publicAddr
	}
	if publicPort != 0 {
		port = publicPort
	}
	return host, port
}

// via makes the Via of the proxy on a request leaving towards a peer, carrying the flow token of
// the peer the request came from.
func (s *Server) via(out, in *peer, rport bool) *sipmsg.ViaHeader {
	host, port := s.addr(out)
	params := sipmsg.NewParams()
	params.Add("branch", sipmsg.RFC3261BranchMagicCookie+randomHex(10))
	if rport {
		params.Add("rport", "")
	}
	params.Add(routeParam, s.token(in))
	return &sipmsg.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0",
		Transport: transport(out), Host: host, Port: port, Params: params}
}

// proxy forwards a request statefully.
func (s *Server) proxy(req *sipmsg.Request, tx *siptx.ServerTx, in *peer) {
	respond := func(code int, reason string) {
		_ = tx.Respond(sipmsg.NewResponseFromRequest(req, code, reason, nil))
	}

	if mf := req.MaxForwards(); mf != nil {
		if *mf == 0 {
			respond(sipmsg.StatusTooManyHops, "Too Many Hops")
			return
		}
		mf.Dec()
	} else {
		mf := sipmsg.MaxForwardsHeader(70)
		req.AppendHeader(&mf)
	}

	// an OPTIONS to the proxy itself
	if req.Method == sipmsg.OPTIONS && req.Recipient.User == "" && req.Route() == nil {
		res := sipmsg.NewResponseFromRequest(req, sipmsg.StatusOK, "OK", nil)
		res.AppendHeader(sipmsg.NewHeader("Allow",
			"INVITE, ACK, CANCEL, BYE, OPTIONS, UPDATE, INFO, REFER, NOTIFY, MESSAGE"))
		_ = tx.Respond(res)
		return
	}

	// a request a trunk starts authenticates by the server's auth policy
	if r := req.Route(); !in.dialed && (r == nil || !r.Address.UriParams.Has(routeParam)) &&
		!s.authenticate(tx, req) {
		return
	}

	out, err := s.route(req, in)
	switch {
	case errors.Is(err, errForbidden):
		s.log.Infof("rejecting %s from %s: %s", req.Method, in.RemoteAddr(), err.Error())
		respond(sipmsg.StatusForbidden, "Forbidden")
		return
	case errors.Is(err, errGone):
		s.log.Infof("rejecting %s from %s: %s", req.Method, in.RemoteAddr(), err.Error())
		respond(sipmsg.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist")
		return
	case errors.Is(err, errNoRoute):
		s.log.Infof("rejecting %s from %s: %s", req.Method, in.RemoteAddr(), err.Error())
		respond(sipmsg.StatusNotFound, "Not Found")
		return
	case err != nil:
		s.log.Infof("cannot forward %s from %s: %s", req.Method, in.RemoteAddr(), err.Error())
		respond(sipmsg.StatusServiceUnavailable, "Service Unavailable")
		return
	}

	// a call a trunk starts is admitted against the quota; every way the INVITE fails gives the
	// quota back
	toTag, _ := req.To().Params.Get("tag")
	newDialog := req.IsInvite() && toTag == ""
	callID := req.CallID().Value()
	admitted := newDialog && !in.dialed
	if admitted && !s.admit(callID, in) {
		s.log.Infof("rejecting call %s from %s: quota exceeded", callID, in.RemoteAddr())
		respond(sipmsg.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	fail := func(code int, reason string) {
		if admitted {
			s.end(callID)
		}
		respond(code, reason)
	}

	fwd := req.Clone()
	if newDialog {
		// RFC 5658: the route towards the party the request came from below, the one towards
		// the party it goes to on top, each with the flow token of its party's conn
		fwd.PrependHeader(s.recordRoute(in))
		fwd.PrependHeader(s.recordRoute(out))
	}
	fwd.PrependHeader(s.via(out, in, true))

	offer := len(req.Body()) > 0 && isSDP(req) && (req.IsInvite() || req.Method == sipmsg.UPDATE)
	if offer {
		body, err := s.mediaRequest(&ng.Request{Command: ng.CommandOffer, CallID: req.CallID().Value(),
			FromTag: fromTag(req), ToTag: toTag, SDP: string(req.Body()),
			Direction: []string{in.side(), out.side()}, ICE: "remove", Replace: []string{"origin"}})
		if err != nil {
			s.log.Infof("media proxy refused the offer of call %s: %s", req.CallID().Value(), err)
			fail(sipmsg.StatusNotAcceptableHere, "Not Acceptable Here")
			return
		}
		fwd.SetBody(body)
	}

	// a CANCEL that came while routing ends the request here: the transaction sent the 487
	select {
	case <-tx.Cancelled():
		if offer {
			s.deleteMedia(req)
		}
		if admitted {
			s.end(callID)
		}
		return
	default:
	}
	fwdTx, err := s.txl.Request(fwd, out)
	if err != nil {
		fail(sipmsg.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	// a retransmitted 2xx of an INVITE goes upstream again, statelessly
	var last2xx []byte
	fwdTx.OnRetransmission(func(*sipmsg.Response) {
		s.mu.Lock()
		b := last2xx
		s.mu.Unlock()
		if b != nil {
			_, _ = in.Write(b)
		}
	})

	cancelled := tx.Cancelled()
	for {
		select {
		case res, ok := <-fwdTx.Responses():
			if !ok {
				s.log.Infof("%s to %s failed: %v", req.Method, out.RemoteAddr(), fwdTx.Err())
				if newDialog || req.Method == sipmsg.BYE {
					s.deleteMedia(req)
				}
				if admitted || req.Method == sipmsg.BYE {
					s.end(callID)
				}
				s.events.OnCallError(in.RemoteAddr(), in.Tag().Proto, fmt.Sprint(fwdTx.Err()))
				respond(sipmsg.StatusRequestTimeout, "Request Timeout")
				return
			}
			res.RemoveHeader("Via")
			if res.StatusCode == sipmsg.StatusTrying {
				continue
			}
			if len(res.Body()) > 0 && isSDP(res) && res.StatusCode > 100 && res.StatusCode < 300 &&
				(req.IsInvite() || req.Method == sipmsg.UPDATE) {
				resTag, _ := res.To().Params.Get("tag")
				mreq := &ng.Request{Command: ng.CommandAnswer, CallID: req.CallID().Value(),
					FromTag: fromTag(req), ToTag: resTag, SDP: string(res.Body()),
					ICE: "remove", Replace: []string{"origin"}}
				if !offer {
					// a late offer: the answer comes in the ACK
					mreq.Command, mreq.Direction = ng.CommandOffer, []string{out.side(), in.side()}
				}
				body, err := s.mediaRequest(mreq)
				if err != nil {
					s.log.Infof("media proxy refused the answer of call %s: %s",
						req.CallID().Value(), err)
				} else {
					res.SetBody(body)
				}
			}
			final := !res.IsProvisional()
			if final && res.IsSuccess() && req.IsInvite() {
				s.mu.Lock()
				last2xx = []byte(res.String())
				s.mu.Unlock()
			}
			if final && (req.Method == sipmsg.BYE || (newDialog && !res.IsSuccess())) {
				s.deleteMedia(req)
			}
			switch {
			case final && admitted && res.IsSuccess():
				s.established(callID)
			case final && (admitted || req.Method == sipmsg.BYE):
				s.end(callID)
			}
			_ = tx.Respond(res)
			if final {
				return
			}
		case <-cancelled:
			// the final response the CANCEL brings comes back on the forwarded transaction
			cancelled = nil
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), siptx.CancelTimeout)
				defer cancel()
				_, _ = s.txl.Do(ctx, siptx.NewCancel(fwd), out)
			}()
		}
	}
}

// forwardAck forwards the ACK of a 2xx statelessly along the dialog's route; an ACK with SDP
// answers a late offer. An ACK without a route of the proxy has no dialog to follow.
func (s *Server) forwardAck(req *sipmsg.Request, in *peer) {
	if r := req.Route(); r == nil || !r.Address.UriParams.Has(routeParam) {
		s.log.Debugf("dropping ACK from %s: no route of the proxy", in.RemoteAddr())
		return
	}
	out, err := s.route(req, in)
	if err != nil {
		s.log.Debugf("dropping ACK from %s: %s", in.RemoteAddr(), err.Error())
		return
	}
	if mf := req.MaxForwards(); mf != nil {
		mf.Dec()
	}
	if len(req.Body()) > 0 && isSDP(req) {
		toTag, _ := req.To().Params.Get("tag")
		body, err := s.mediaRequest(&ng.Request{Command: ng.CommandAnswer,
			CallID: req.CallID().Value(), FromTag: fromTag(req), ToTag: toTag, SDP: string(req.Body()),
			ICE: "remove", Replace: []string{"origin"}})
		if err == nil {
			req.SetBody(body)
		}
	}
	req.PrependHeader(s.via(out, in, false))
	_, _ = out.Write([]byte(req.String()))
}

// forwardResponse forwards a response no transaction matches, such as a retransmitted 2xx after
// the transaction ended, on the conn the flow token of the proxy's Via names.
func (s *Server) forwardResponse(res *sipmsg.Response) {
	v := res.Via()
	if v == nil {
		return
	}
	token, ours := v.Params.Get(routeParam)
	if !ours {
		return
	}
	res.RemoveHeader("Via")
	if p := s.peerOf(token); p != nil {
		_, _ = p.Write([]byte(res.String()))
	}
}

// mediaRequest sends an ng request to the media proxy and returns the rewritten SDP.
func (s *Server) mediaRequest(req *ng.Request) ([]byte, error) {
	cl, err := s.mediaClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), MediaProxyTimeout)
	defer cancel()
	res, err := cl.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	return []byte(res.SDP), nil
}

// deleteMedia ends the media of the call of a request.
func (s *Server) deleteMedia(req *sipmsg.Request) {
	cl, err := s.mediaClient()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), MediaProxyTimeout)
	defer cancel()
	if _, err := cl.Do(ctx, &ng.Request{Command: ng.CommandDelete, CallID: req.CallID().Value(),
		FromTag: fromTag(req)}); err != nil {
		s.log.Debugf("media proxy delete of call %s: %s", req.CallID().Value(), err.Error())
	}
}

// mediaClient returns the ng client of the media proxy, dialing the first endpoint of the media
// proxy cluster unless a live one is open.
func (s *Server) mediaClient() (*ng.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return nil, server.ErrNotServing
	}
	if s.media != nil {
		select {
		case <-s.media.Done():
			_ = s.media.Close()
			s.media = nil
		default:
			return s.media, nil
		}
	}
	s.confMu.RLock()
	name := s.mediaProxy
	s.confMu.RUnlock()
	if name == "" {
		return nil, fmt.Errorf("server %s names no media proxy", s.Name())
	}
	// the offer and the answer of a call must reach the same media proxy: no balancing, the
	// cluster's first endpoint (a FIFO cluster opens the fifo its address names)
	cl := s.rt.ClusterConfig(name)
	d, found := s.rt.Dialer(name)
	if cl == nil || !found {
		return nil, fmt.Errorf("no media proxy cluster %q", name)
	}
	dst := ""
	if len(cl.Endpoints) > 0 {
		dst = cl.Endpoints[0]
	}
	ctx, cancel := context.WithTimeout(context.Background(), MediaProxyTimeout)
	defer cancel()
	c, err := d.Dial(ctx, nil, dst)
	if err != nil {
		return nil, err
	}
	// the in-process media proxy speaks JSON, an external one bencode
	enc := ng.Bencode
	if d.Protocol() == stnrv2.ProtocolFIFO {
		enc = ng.JSON
	}
	s.media = ng.NewClient(c, enc)
	return s.media, nil
}

// recordRoute makes the Record-Route of the proxy towards a peer, carrying its flow token.
func (s *Server) recordRoute(p *peer) *sipmsg.RecordRouteHeader {
	host, port := s.addr(p)
	params := sipmsg.NewParams()
	params.Add("lr", "")
	params.Add(routeParam, s.token(p))
	return &sipmsg.RecordRouteHeader{Address: sipmsg.Uri{Scheme: "sip", Host: host, Port: port,
		UriParams: params}}
}

func fromTag(req *sipmsg.Request) string {
	t, _ := req.From().Params.Get("tag")
	return t
}

func isSDP(msg sipmsg.Message) bool {
	ct := msg.GetHeaders("Content-Type")
	return len(ct) > 0 && strings.EqualFold(ct[0].Value(), "application/sdp")
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// transport returns the Via transport of a conn.
func transport(c api.Conn) string {
	switch c.Tag().Proto {
	case stnrv2.ProtocolTCP:
		return "TCP"
	case stnrv2.ProtocolTLS:
		return "TLS"
	}
	return "UDP"
}

func addrPort(a net.Addr) netip.AddrPort {
	var ap netip.AddrPort
	switch a := a.(type) {
	case *net.UDPAddr:
		ap = a.AddrPort()
	case *net.TCPAddr:
		ap = a.AddrPort()
	default:
		ap, _ = netip.ParseAddrPort(a.String())
	}
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}

// admit reserves the quota of a call a trunk starts: the server's name is the user, in the realm
// of the server's auth policy.
func (s *Server) admit(callID string, in *peer) bool {
	s.confMu.RLock()
	name := s.auth
	s.confMu.RUnlock()
	realm := stnrv2.DefaultRealm
	if auth := s.rt.AuthConfig(name); auth != nil {
		realm = auth.Realm
	}
	src := in.RemoteAddr()
	if !s.gate(s.Name(), realm, src) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[callID] = &call{event: CallEvent{SrcAddr: src, DstAddr: in.LocalAddr(),
		Protocol: in.Tag().Proto, CallID: callID, Username: s.Name(), Realm: realm}}
	return true
}

// established reports an admitted call established.
func (s *Server) established(callID string) {
	s.mu.Lock()
	c, ok := s.calls[callID]
	if ok && !c.created {
		c.created = true
	} else {
		ok = false
	}
	s.mu.Unlock()
	if ok {
		s.events.OnCallCreated(c.event)
	}
}

// end ends an admitted call.
func (s *Server) end(callID string) {
	s.mu.Lock()
	c, ok := s.calls[callID]
	delete(s.calls, callID)
	s.mu.Unlock()
	if ok {
		s.release(c)
	}
}

// release gives the quota of an ended call back: an established call is deleted.
func (s *Server) release(c *call) {
	if c.created {
		s.events.OnCallDeleted(c.event)
		return
	}
	ev := c.event
	s.quota.AllocationHandler(ev.SrcAddr, ev.DstAddr, ev.Protocol.String(), ev.Username, ev.Realm,
		turnsrv.AllocationDeleted)
}

// sweep ends the established calls the media proxy no longer knows: the calls whose parties left
// without a BYE.
func (s *Server) sweep(stop chan struct{}) {
	t := time.NewTicker(CallSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		s.mu.Lock()
		ids := make([]string, 0, len(s.calls))
		for id, c := range s.calls {
			if c.created {
				ids = append(ids, id)
			}
		}
		s.mu.Unlock()
		cl, err := s.mediaClient()
		if err != nil {
			continue
		}
		for _, id := range ids {
			ctx, cancel := context.WithTimeout(context.Background(), MediaProxyTimeout)
			resp, err := cl.Do(ctx, &ng.Request{Command: ng.CommandQuery, CallID: id})
			cancel()
			if err != nil && resp != nil {
				s.end(id)
			}
		}
	}
}
