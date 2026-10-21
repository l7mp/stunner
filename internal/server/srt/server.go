// Package srt implements the SRT server: it routes every SRT flow by the stream it names to the
// endpoint holding the stream and relays it, the stream's encryption untouched. The first flow
// naming a stream places it on an endpoint of the server's clusters by the load-balancing policy;
// every later flow naming it, publisher or reader, goes to the same endpoint.
package srt

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	gosrt "github.com/datarhei/gosrt"
	"github.com/datarhei/gosrt/packet"
	"github.com/pion/logging"
	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/flow"
	"github.com/l7mp/stunner/v2/internal/offload"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	"github.com/l7mp/stunner/v2/internal/server/balancer"
	turnsrv "github.com/l7mp/stunner/v2/internal/server/turn"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

var (
	// FlowTimeout is the idle timeout of a relayed flow: SRT peers send keepalives every second
	// and give up on a peer silent for 5 seconds.
	FlowTimeout = 10 * time.Second
	// HandshakeTimeout bounds a caller's handshake, the endpoint's included.
	HandshakeTimeout = 10 * time.Second
	// RetryPeriod is how often the server repeats a handshake towards an endpoint that is silent.
	RetryPeriod = 250 * time.Millisecond
	// CookiePeriod is how long a SYN cookie is valid: the cookie of the current and of the
	// previous period are accepted.
	CookiePeriod = time.Minute
)

var (
	errNoEndpoint    = errors.New("no endpoint to place the stream on")
	errQuotaExceeded = errors.New("stream quota exceeded")
)

// Server is an SRT server object.
type Server struct {
	server.Base
	rt     *objruntime.Runtime
	log    logging.LeveledLogger
	quota  *turnsrv.Quota
	gate   turn.QuotaHandler
	secret []byte

	mu        sync.RWMutex
	running   bool
	conntrack *flow.Conntrack    // the relayed flows
	streams   map[string]*stream // the placement of every stream, by its resource
	lb        balancer.Balancer  // places the new streams
	lbPolicy  stnrv2.LBPolicy    // the load-balancing policy of lb
	linger    string             // as configured
	lingerDur time.Duration
}

// stream is the placement of a stream: the endpoint its flows go to, the live flows, and the timer
// that removes it once it lingered past its last flow. A stream holds one stream of the quota.
type stream struct {
	cluster  string
	endpoint netip.AddrPort
	refs     int
	expire   *time.Timer
}

var (
	_ api.Server        = &Server{}
	_ objruntime.Object = &Server{}
)

// New creates the SRT server of a validated config.
func New(conf *stnrv2.ServerConfig, rt *objruntime.Runtime) (*Server, error) {
	q := turnsrv.NewQuotaHandler(rt)
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	s := &Server{
		rt:     rt,
		log:    rt.Logger.NewLogger(fmt.Sprintf("server-%s", conf.Name)),
		quota:  q,
		gate:   q.QuotaHandler(),
		secret: secret,
	}
	s.Init(conf)
	if err := s.Reconcile(conf); err != nil {
		return nil, err
	}
	return s, nil
}

// Reconcile applies the spec, the linger period and the load-balancing policy to the next flows.
func (s *Server) Reconcile(conf stnrv2.Config) error {
	if err := s.Base.Reconcile(conf); err != nil {
		return err
	}
	req, _ := s.Desired(conf)
	d, err := time.ParseDuration(req.SRT.Linger)
	if err != nil {
		return err
	}
	policy, err := stnrv2.NewLBPolicy(req.SRT.LBPolicy)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if policy != s.lbPolicy {
		lb, err := balancer.New(policy)
		if err != nil {
			return err
		}
		s.lb, s.lbPolicy = lb, policy
	}
	s.linger, s.lingerDur = req.SRT.Linger, d
	return nil
}

func (s *Server) GetConfig() stnrv2.Config {
	c := s.Base.GetConfig().(*stnrv2.ServerConfig)
	s.mu.RLock()
	defer s.mu.RUnlock()
	c.SRT = &stnrv2.SRTServerConfig{Linger: s.linger, LBPolicy: s.lbPolicy.String()}
	return c
}

func (s *Server) Status() stnrv2.Status {
	return &stnrv2.ServerStatus{ServerConfig: s.GetConfig().(*stnrv2.ServerConfig), Sessions: s.Sessions()}
}

// Start starts serving with no streams placed.
func (s *Server) Start() error {
	s.mu.Lock()
	s.running = true
	s.conntrack, s.streams = flow.NewConntrack(FlowTimeout), map[string]*stream{}
	s.mu.Unlock()
	s.log.Infof("server %s: SRT server running", s.Name())
	return nil
}

// Close tears down every flow and gives back the quota of every stream.
func (s *Server) Close(_ bool) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	ct := s.conntrack
	s.mu.Unlock()
	ct.Close()
	// the flows released their streams on close: give back the quota of the lingering ones
	s.mu.Lock()
	for resource, st := range s.streams {
		if st.expire != nil {
			st.expire.Stop()
		}
		delete(s.streams, resource)
		s.quota.AllocationHandler(nil, nil, "", s.Name(), stnrv2.DefaultRealm, turnsrv.AllocationDeleted)
	}
	s.mu.Unlock()
	return nil
}

// Sessions returns the number of relayed flows.
func (s *Server) Sessions() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.conntrack == nil {
		return 0
	}
	return s.conntrack.Len()
}

// Serve takes the conn of a caller.
func (s *Server) Serve(c api.Conn) (api.Conn, api.Verdict, error) {
	s.mu.RLock()
	running := s.running
	s.mu.RUnlock()
	if !running {
		return nil, api.Consumed, server.ErrNotServing
	}
	go s.serve(c)
	return nil, api.Consumed, nil
}

// serve answers a caller's induction, then takes its conclusion to the endpoint of the stream it
// names; anything else before the conclusion is dropped.
func (s *Server) serve(c api.Conn) {
	_ = c.SetReadDeadline(time.Now().Add(HandshakeTimeout))
	buf := make([]byte, 1500)
	for {
		n, err := c.Read(buf)
		if err != nil {
			_ = c.Close()
			return
		}
		hs, ok := parseHandshake(buf[:n])
		if !ok {
			continue
		}
		switch hs.HandshakeType {
		case packet.HSTYPE_INDUCTION:
			// the caller's fields echoed, as an SRT handshake with the server's socket and cookie
			caller := hs.SRTSocketId
			hs.Version, hs.EncryptionField, hs.ExtensionField = 5, 0, srtMagic
			hs.SRTSocketId, hs.SynCookie = mrand.Uint32(), s.cookie(c.RemoteAddr(), time.Now())
			_, _ = c.Write(handshakePacket(caller, hs))
		case packet.HSTYPE_CONCLUSION:
			if !s.validCookie(c.RemoteAddr(), hs.SynCookie) {
				continue
			}
			s.splice(c, append([]byte(nil), buf[:n]...), hs)
			return
		}
	}
}

// splice places the stream of a conclusion, runs the caller's handshake with the endpoint and
// relays the flow, for as long as the flow lives; a stream it cannot serve is rejected with the
// reason.
func (s *Server) splice(c api.Conn, conclusion []byte, hs *packet.CIFHandshake) {
	defer c.Close() //nolint:errcheck
	reject := func(reason gosrt.RejectionReason, msg string) {
		s.log.Infof("rejecting SRT flow from %s for stream ID %q: %s", c.RemoteAddr(), hs.StreamId, msg)
		hs.HandshakeType = packet.HandshakeType(reason)
		_, _ = c.Write(handshakePacket(hs.SRTSocketId, hs))
	}
	resource := Resource(hs.StreamId)
	if resource == "" {
		reject(gosrt.REJX_BAD_REQUEST, "no stream in the stream ID")
		return
	}
	st, err := s.place(resource)
	if err != nil {
		reason := gosrt.REJX_NOTFOUND
		if errors.Is(err, errQuotaExceeded) {
			reason = gosrt.REJX_OVERLOAD
		}
		reject(reason, err.Error())
		return
	}
	defer s.release(resource, st)

	endpoint := st.endpoint.String()
	d, ok := s.rt.Dialer(st.cluster)
	if !ok {
		reject(gosrt.REJX_NOTFOUND, "no cluster "+st.cluster)
		return
	}
	leg, err := d.Dial(context.Background(), nil, endpoint)
	if err != nil {
		reject(gosrt.REJX_GW, err.Error())
		return
	}
	defer leg.Close() //nolint:errcheck

	resp, answer, cookie, err := handshakeEndpoint(leg, conclusion, hs)
	if err != nil {
		reject(gosrt.REJX_GW, "endpoint "+endpoint+": "+err.Error())
		return
	}
	_, _ = c.Write(resp)
	if answer.HandshakeType.IsRejection() {
		s.log.Infof("endpoint %s rejected the SRT flow from %s for stream %q: reason %d", endpoint,
			c.RemoteAddr(), resource, answer.HandshakeType)
		return
	}
	_ = c.SetReadDeadline(time.Time{})

	e := &flow.Entry{Client: &callerConn{Conn: c, cookie: cookie}, Leg: leg}
	s.mu.RLock()
	ct := s.conntrack
	s.mu.RUnlock()
	if !ct.Add(e) {
		return
	}
	s.log.Debugf("SRT flow from %s for stream %q relayed to %s", c.RemoteAddr(), resource, endpoint)
	go func() {
		if err := e.Offload(s.rt.OffloadEngine, puntRules...); err != nil && !errors.Is(err, net.ErrClosed) {
			s.log.Debugf("SRT flow from %s stays in user space: %s", c.RemoteAddr(), err)
		}
	}()
	flow.Forward(e)
}

// puntRules keep the handshakes and the shutdowns of an offloaded flow in user space: a repeated
// conclusion needs the endpoint's cookie, a shutdown ends the flow. They match the first 16 bits of
// a packet: the control flag and the control type, 0 for a handshake and 5 for a shutdown.
var puntRules = []offload.PuntRule{
	{Mask: 0xffff0000, Value: 0x80000000},
	{Mask: 0xffff0000, Value: 0x80050000},
}

// handshakeEndpoint runs the caller's handshake with the endpoint: an induction made from the
// caller's conclusion for the endpoint's cookie, then the raw conclusion with that cookie. It
// returns the endpoint's answer to the conclusion, a conclusion or a rejection, raw and parsed,
// and the endpoint's cookie.
func handshakeEndpoint(leg api.Conn, conclusion []byte, hs *packet.CIFHandshake) ([]byte, *packet.CIFHandshake, uint32, error) {
	defer func() { _ = leg.SetReadDeadline(time.Time{}) }()
	deadline := time.Now().Add(HandshakeTimeout)
	buf := make([]byte, 1500)
	exchange := func(req []byte, want func(*packet.CIFHandshake) bool) ([]byte, *packet.CIFHandshake, error) {
		for time.Now().Before(deadline) {
			if _, err := leg.Write(req); err != nil {
				return nil, nil, err
			}
			_ = leg.SetReadDeadline(time.Now().Add(RetryPeriod))
			for {
				n, err := leg.Read(buf)
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					break
				}
				if err != nil {
					return nil, nil, err
				}
				if h, ok := parseHandshake(buf[:n]); ok && want(h) {
					return append([]byte(nil), buf[:n]...), h, nil
				}
			}
		}
		return nil, nil, errors.New("handshake timed out")
	}

	induction := handshakePacket(0, &packet.CIFHandshake{
		Version:                     4,
		HandshakeType:               packet.HSTYPE_INDUCTION,
		InitialPacketSequenceNumber: hs.InitialPacketSequenceNumber,
		MaxTransmissionUnitSize:     hs.MaxTransmissionUnitSize,
		MaxFlowWindowSize:           hs.MaxFlowWindowSize,
		SRTSocketId:                 hs.SRTSocketId,
		PeerIP:                      hs.PeerIP,
	})
	_, ind, err := exchange(induction, func(h *packet.CIFHandshake) bool {
		return h.HandshakeType == packet.HSTYPE_INDUCTION
	})
	if err != nil {
		return nil, nil, 0, err
	}
	binary.BigEndian.PutUint32(conclusion[offCookie:], ind.SynCookie)
	resp, answer, err := exchange(conclusion, func(h *packet.CIFHandshake) bool {
		return h.HandshakeType == packet.HSTYPE_CONCLUSION || h.HandshakeType.IsRejection()
	})
	if err != nil {
		return nil, nil, 0, err
	}
	return resp, answer, ind.SynCookie, nil
}

// place returns the placement of a stream, with one more flow on it. A stream keeps its endpoint
// while the endpoint is a target of its cluster; a new stream, and one whose endpoint is gone, takes
// the next target of the clusters by the load-balancing policy, and a stream of the quota.
func (s *Server) place(resource string) (*stream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.streams[resource]; ok {
		if r, ok := s.rt.Router(st.cluster); ok && slices.Contains(r.Targets(), st.endpoint) {
			if st.expire != nil {
				st.expire.Stop()
				st.expire = nil
			}
			st.refs++
			return st, nil
		}
	}
	cluster, dst, ok := s.lb.Next(s.rt, s.Clusters())
	if !ok {
		return nil, errNoEndpoint
	}
	if !s.gate(s.Name(), stnrv2.DefaultRealm, nil) {
		return nil, errQuotaExceeded
	}
	st := &stream{cluster: cluster, endpoint: dst, refs: 1}
	s.streams[resource] = st
	return st, nil
}

// release ends a flow of a stream. The stream keeps its endpoint for the linger period after its
// last flow, then gives its quota back; a stream placed afresh meanwhile gives it back at once.
func (s *Server) release(resource string, st *stream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.refs--; st.refs > 0 {
		return
	}
	if s.streams[resource] != st {
		s.quota.AllocationHandler(nil, nil, "", s.Name(), stnrv2.DefaultRealm, turnsrv.AllocationDeleted)
		return
	}
	st.expire = time.AfterFunc(s.lingerDur, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if st.refs > 0 {
			return
		}
		if s.streams[resource] == st {
			delete(s.streams, resource)
		}
		s.log.Debugf("stream %q released", resource)
		s.quota.AllocationHandler(nil, nil, "", s.Name(), stnrv2.DefaultRealm, turnsrv.AllocationDeleted)
	})
}

// cookie is the SYN cookie of a caller in a cookie period: a MAC of its address and the period.
func (s *Server) cookie(addr net.Addr, t time.Time) uint32 {
	m := hmac.New(sha256.New, s.secret)
	_, _ = m.Write([]byte(addr.String()))
	_ = binary.Write(m, binary.BigEndian, t.UnixNano()/int64(CookiePeriod))
	return binary.BigEndian.Uint32(m.Sum(nil))
}

// validCookie accepts the cookie of the current and of the previous cookie period.
func (s *Server) validCookie(addr net.Addr, c uint32) bool {
	now := time.Now()
	return c == s.cookie(addr, now) || c == s.cookie(addr, now.Add(-CookiePeriod))
}
