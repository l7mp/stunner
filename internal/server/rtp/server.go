// Package rtp implements the RTP server, a media proxy driven over ng.
package rtp

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/logging"
	"github.com/pion/sdp/v3"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/flow"
	"github.com/l7mp/stunner/v2/internal/proto/ng"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

var (
	// MediaTimeout is how long a call lives without media in either direction.
	MediaTimeout = 5 * time.Minute
	// EarlyMediaHold is how long media from an unknown source waits for the answer that names it.
	EarlyMediaHold = 2 * time.Second
	// EarlyMediaBuffer is the number of packets a stream holds per direction for a party it
	// cannot reach yet; older packets give way to newer ones.
	EarlyMediaBuffer = 50
	// MaxUnmatched is the number of media conns from unknown sources that may wait at once.
	MaxUnmatched = 256
)

const bufferSize = 65535

var (
	errNoSocket = errors.New("no cluster makes a media socket")
	errNoCall   = errors.New("unknown call")
)

// Server is an rtp server.
type Server struct {
	server.Base
	rt  *objruntime.Runtime
	log logging.LeveledLogger

	mu        sync.RWMutex
	running   bool
	calls     map[string]*call
	sources   map[netip.AddrPort]*stream // trunk media source -> its stream
	changed   chan struct{}              // closed and replaced on every change of sources
	controls  map[api.Conn]bool
	conntrack *flow.Conntrack // the trunk media conns
	stop      chan struct{}
	unmatched int // the trunk media conns waiting for their call

	// the options of the server: Reconcile writes them, the media path reads them, under confMu
	confMu     sync.RWMutex
	publicAddr string
	publicPort int
}

// constructor builds the RTP server of a validated config.
var constructor = func(conf *stnrv2.ServerConfig, rt *objruntime.Runtime) (objruntime.Object, error) {
	s, err := newServer(conf, rt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// New creates the RTP server of a validated config.
func New(conf *stnrv2.ServerConfig, rt *objruntime.Runtime) (objruntime.Object, error) {
	return constructor(conf, rt)
}

// call is the media of a call: a stream per media section.
type call struct {
	id      string
	streams []*stream
	// offerer is the side of the party that made the last offer.
	offerer string
	// last is the time of the last signal or media, in Unix nanoseconds.
	last atomic.Int64
}

// stream is one media section of a call, relayed between its external party (the trunk) and its
// internal party (the media peer in the cluster).
type stream struct {
	call *call
	// ext is the trunk's endpoint, the zero value until known; extRTP and extRTCP are the conns
	// of its RTP and RTCP sources once they sent anything.
	ext             endpoint
	extRTP, extRTCP api.Conn
	// sock carries RTP, and RTCP when the internal party muxes, to and from the internal party;
	// rtcpSock carries its RTCP otherwise.
	sock, rtcpSock         net.PacketConn
	sockAddr, rtcpSockAddr netip.AddrPort // as advertised
	cluster                string         // the cluster that made the sockets
	in                     endpoint       // the internal party, the zero value until known
	// toExt and toIn hold the packets for a party the stream cannot reach yet, oldest first.
	toExt, toIn [][]byte
}

// extConn returns the trunk conn a packet goes out on, nil while the trunk has not sent from the
// source it goes to. The caller holds mu.
func (st *stream) extConn(p []byte) api.Conn {
	if isRTCP(p) && !st.ext.mux {
		return st.extRTCP
	}
	return st.extRTP
}

// inDest returns the socket and the address a packet for the internal party goes out on, an
// invalid address while the internal party is unknown. The caller holds mu.
func (st *stream) inDest(p []byte) (net.PacketConn, netip.AddrPort) {
	if isRTCP(p) && !st.in.mux {
		return st.rtcpSock, st.in.rtcp
	}
	return st.sock, st.in.rtp
}

// flushExt sends the held packets the trunk can now be reached for, in order. The caller holds mu.
func (st *stream) flushExt() {
	rest := st.toExt[:0]
	for _, p := range st.toExt {
		if c := st.extConn(p); c != nil {
			_, _ = c.Write(p)
		} else {
			rest = append(rest, p)
		}
	}
	st.toExt = rest
}

// flushIn sends the held packets to the internal party, in order. The caller holds mu.
func (st *stream) flushIn() {
	for _, p := range st.toIn {
		if sock, dst := st.inDest(p); sock != nil && dst.IsValid() {
			_, _ = sock.WriteTo(p, net.UDPAddrFromAddrPort(dst))
		}
	}
	st.toIn = nil
}

// hold appends a copy of a packet to a queue, dropping the oldest beyond EarlyMediaBuffer.
func hold(q [][]byte, p []byte) [][]byte {
	if len(q) >= EarlyMediaBuffer {
		q = q[1:]
	}
	return append(q, append([]byte(nil), p...))
}

// newServer creates the RTP server of a validated config.
func newServer(conf *stnrv2.ServerConfig, rt *objruntime.Runtime) (*Server, error) {
	s := &Server{rt: rt, log: rt.Logger.NewLogger(fmt.Sprintf("server-%s", conf.Name))}
	s.Init(conf)
	if err := s.Reconcile(conf); err != nil {
		return nil, err
	}
	return s, nil
}

// Reconcile applies the spec and the public address to the next calls.
func (s *Server) Reconcile(conf stnrv2.Config) error {
	if err := s.Base.Reconcile(conf); err != nil {
		return err
	}
	req, _ := s.Desired(conf)
	s.confMu.Lock()
	defer s.confMu.Unlock()
	s.publicAddr, s.publicPort = req.RTP.PublicAddr, req.RTP.PublicPort
	return nil
}

func (s *Server) GetConfig() stnrv2.Config {
	c := s.Base.GetConfig().(*stnrv2.ServerConfig)
	s.confMu.RLock()
	defer s.confMu.RUnlock()
	c.RTP = &stnrv2.RTPServerConfig{PublicAddr: s.publicAddr, PublicPort: s.publicPort}
	return c
}

func (s *Server) Status() stnrv2.Status {
	return &stnrv2.ServerStatus{ServerConfig: s.GetConfig().(*stnrv2.ServerConfig), Sessions: s.Sessions()}
}

// Start starts serving with an empty call table.
func (s *Server) Start() error {
	s.mu.Lock()
	s.running = true
	s.calls, s.sources = map[string]*call{}, map[netip.AddrPort]*stream{}
	s.changed, s.controls = make(chan struct{}), map[api.Conn]bool{}
	s.conntrack, s.stop = flow.NewConntrack(MediaTimeout), make(chan struct{})
	stop := s.stop
	s.mu.Unlock()
	go s.reap(stop)
	s.log.Infof("server %s: rtp server running", s.Name())
	return nil
}

// Close ends every call and closes the control conns.
func (s *Server) Close(_ bool) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	close(s.stop)
	for id := range s.calls {
		s.deleteLocked(id)
	}
	for c := range s.controls {
		_ = c.Close()
	}
	ct := s.conntrack
	s.mu.Unlock()
	ct.Close()
	return nil
}

// Sessions returns the number of calls.
func (s *Server) Sessions() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.calls)
}

// Serve takes a control conn from a listener labeled "role: control", and a carrier media conn
// from any other.
func (s *Server) Serve(c api.Conn) (api.Conn, api.Verdict, error) {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil, api.Consumed, server.ErrNotServing
	}
	if c.Tag().Labels[stnrv2.RoleLabel] == stnrv2.RoleControl {
		s.controls[c] = true
		s.mu.Unlock()
		go func() {
			err := ng.Serve(c, s.handle)
			s.log.Debugf("control conn from %s closed: %v", c.RemoteAddr(), err)
			s.mu.Lock()
			delete(s.controls, c)
			s.mu.Unlock()
			_ = c.Close()
		}()
		return nil, api.Consumed, nil
	}
	s.mu.Unlock()
	go s.serveMedia(c)
	return nil, api.Consumed, nil
}

// handle runs an ng command.
func (s *Server) handle(req *ng.Request) *ng.Response {
	s.log.Debugf("ng %s call-id=%s from-tag=%s to-tag=%s", req.Command, req.CallID, req.FromTag,
		req.ToTag)
	switch req.Command {
	case ng.CommandPing:
		return &ng.Response{Result: ng.ResultPong}
	case ng.CommandOffer, ng.CommandAnswer:
		sdpOut, err := s.negotiate(req)
		if err != nil {
			s.log.Infof("ng %s of call %s failed: %s", req.Command, req.CallID, err.Error())
			return ng.Errorf("%s", err.Error())
		}
		return &ng.Response{Result: ng.ResultOK, SDP: sdpOut}
	case ng.CommandDelete:
		s.mu.Lock()
		_, ok := s.calls[req.CallID]
		s.deleteLocked(req.CallID)
		s.mu.Unlock()
		if !ok {
			return ng.Errorf("%s", errNoCall.Error())
		}
		return &ng.Response{Result: ng.ResultOK}
	case ng.CommandQuery:
		s.mu.RLock()
		defer s.mu.RUnlock()
		c, ok := s.calls[req.CallID]
		if !ok {
			return ng.Errorf("%s", errNoCall.Error())
		}
		return &ng.Response{Result: ng.ResultOK, Fields: ng.Message{
			"streams": int64(len(c.streams)), "last": c.last.Load() / int64(time.Second)}}
	default:
		return ng.Errorf("unsupported command %q", req.Command)
	}
}

// negotiate handles an offer or an answer: it records the endpoints of the party sending the SDP
// and returns the SDP rewritten for the other party.
func (s *Server) negotiate(req *ng.Request) (string, error) {
	d := &sdp.SessionDescription{}
	if err := d.UnmarshalString(req.SDP); err != nil {
		return "", fmt.Errorf("bad SDP: %w", err)
	}
	eps, err := endpoints(d)
	if err != nil {
		return "", err
	}
	// the side of the party sending this SDP: the offerer for an offer, the other one for an
	// answer
	side := ng.DirectionExternal
	if len(req.Direction) > 0 && req.Direction[0] == ng.DirectionInternal {
		side = ng.DirectionInternal
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return "", server.ErrNotServing
	}
	c, ok := s.calls[req.CallID]
	switch {
	case req.Command == ng.CommandOffer:
		if !ok {
			c = &call{id: req.CallID}
			s.calls[req.CallID] = c
		}
		c.offerer = side
	case !ok:
		return "", errNoCall
	case c.offerer == ng.DirectionExternal:
		side = ng.DirectionInternal
	default:
		side = ng.DirectionExternal
	}
	c.last.Store(time.Now().UnixNano())
	offer := req.Command == ng.CommandOffer

	addrs := make([]netip.AddrPort, len(eps))
	rtcp := make([]netip.AddrPort, len(eps))
	mux := make([]bool, len(eps))
	public, err := s.advertised()
	if err != nil {
		return "", err
	}
	for i, e := range eps {
		if i >= len(c.streams) {
			c.streams = append(c.streams, &stream{call: c})
		}
		st := c.streams[i]
		if !e.rtp.IsValid() {
			continue // rejected or removed: the port stays 0
		}
		if st.sock == nil {
			if err := s.allocate(st); err != nil {
				s.deleteLocked(c.id)
				return "", err
			}
		}
		if side == ng.DirectionExternal {
			// the trunk's endpoints, the internal party gets our sockets
			if st.ext.rtp.IsValid() {
				delete(s.sources, st.ext.rtp)
				delete(s.sources, st.ext.rtcp)
			}
			st.ext = e
			s.sources[e.rtp], s.sources[e.rtcp] = st, st
			close(s.changed)
			s.changed = make(chan struct{})
			addrs[i], rtcp[i] = st.sockAddr, st.rtcpSockAddr
			// offer rtcp-mux to the internal party with a separate RTCP socket as the
			// fallback; answer with it only if the internal offer had it
			mux[i] = offer || st.in.mux
			if mux[i] && !offer {
				rtcp[i] = st.sockAddr
			}
		} else {
			st.in = e
			st.flushIn()
			// the trunk sends RTP and RTCP to the public address, demuxed by source
			addrs[i], rtcp[i] = public, public
			mux[i] = offer || st.ext.mux
		}
	}
	rewrite(d, addrs, rtcp, mux)
	b, err := d.Marshal()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// advertised is where the trunks send media.
func (s *Server) advertised() (netip.AddrPort, error) {
	s.confMu.RLock()
	addr, port := s.publicAddr, s.publicPort
	s.confMu.RUnlock()
	a, err := netip.ParseAddr(addr)
	if err != nil || a.IsUnspecified() || port == 0 {
		return netip.AddrPort{}, fmt.Errorf("server %s has no public address and port to advertise",
			s.Name())
	}
	return netip.AddrPortFrom(a, uint16(port)), nil
}

// allocate makes the internal sockets of a stream through the first cluster that makes them, and
// starts relaying what arrives on them. The caller holds mu.
func (s *Server) allocate(st *stream) error {
	for _, name := range s.Clusters() {
		d, ok := s.rt.Dialer(name)
		if !ok {
			continue
		}
		sock, addr, err := d.ListenPacket("udp4", 0)
		if err != nil {
			continue
		}
		rtcpSock, rtcpAddr, err := d.ListenPacket("udp4", 0)
		if err != nil {
			_ = sock.Close()
			continue
		}
		st.sock, st.rtcpSock, st.cluster = sock, rtcpSock, name
		st.sockAddr, st.rtcpSockAddr = addrPort(addr), addrPort(rtcpAddr)
		go s.relayInternal(st, sock)
		go s.relayInternal(st, rtcpSock)
		return nil
	}
	return errNoSocket
}

// relayInternal relays what the internal party sends to a stream socket to the trunk's RTP or
// RTCP source, holding packets until that source has sent.
func (s *Server) relayInternal(st *stream, sock net.PacketConn) {
	buf := make([]byte, bufferSize)
	for {
		n, from, err := sock.ReadFrom(buf)
		if err != nil {
			return
		}
		p := buf[:n]
		s.mu.RLock()
		ok := st.in.rtp.IsValid() && addrPort(from).Addr() == st.in.rtp.Addr()
		dst := st.extConn(p)
		s.mu.RUnlock()
		if !ok {
			continue
		}
		st.call.last.Store(time.Now().UnixNano())
		if dst == nil {
			s.mu.Lock()
			// the trunk may have shown up meanwhile, after flushing what was held
			if dst = st.extConn(p); dst == nil {
				st.toExt = hold(st.toExt, p)
			}
			s.mu.Unlock()
			if dst == nil {
				continue
			}
		}
		_, _ = dst.Write(p)
	}
}

// serveMedia relays a trunk media source to the internal party of its stream, waiting a while
// for the answer naming an unknown source.
func (s *Server) serveMedia(c api.Conn) {
	src := addrPort(c.RemoteAddr())
	s.mu.Lock()
	if s.sources[src] == nil && s.unmatched >= MaxUnmatched {
		s.mu.Unlock()
		s.log.Debugf("dropping media from unknown source %s: too many unknown sources", src)
		_ = c.Close()
		return
	}
	s.unmatched++
	s.mu.Unlock()
	matched := func() {
		s.mu.Lock()
		s.unmatched--
		s.mu.Unlock()
	}
	deadline := time.After(EarlyMediaHold)
	var st *stream
	for st == nil {
		s.mu.RLock()
		st = s.sources[src]
		changed, running := s.changed, s.running
		s.mu.RUnlock()
		if !running {
			matched()
			_ = c.Close()
			return
		}
		if st != nil {
			break
		}
		select {
		case <-changed:
		case <-deadline:
			s.log.Debugf("dropping media from unknown source %s", src)
			matched()
			_ = c.Close()
			return
		}
	}
	matched()

	e := &flow.Entry{Client: c}
	e.Hooks.OnClose = func(string) {
		s.mu.Lock()
		if st.extRTP == c {
			st.extRTP = nil
		}
		if st.extRTCP == c {
			st.extRTCP = nil
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	ct := s.conntrack
	if src == st.ext.rtp {
		st.extRTP = c
	}
	if src == st.ext.rtcp {
		st.extRTCP = c
	}
	st.flushExt()
	s.mu.Unlock()
	if !ct.Add(e) {
		_ = c.Close()
		return
	}

	buf := make([]byte, bufferSize)
	for {
		n, err := c.Read(buf)
		if err != nil {
			e.Close("trunk side closed")
			return
		}
		e.Touch()
		p := buf[:n]
		st.call.last.Store(time.Now().UnixNano())
		s.mu.RLock()
		sock, dst := st.inDest(p)
		s.mu.RUnlock()
		if sock == nil || !dst.IsValid() {
			s.mu.Lock()
			// the answer may have come meanwhile, after flushing what was held
			if sock, dst = st.inDest(p); sock == nil || !dst.IsValid() {
				st.toIn = hold(st.toIn, p)
			}
			s.mu.Unlock()
			if sock == nil || !dst.IsValid() {
				continue
			}
		}
		_, _ = sock.WriteTo(p, net.UDPAddrFromAddrPort(dst))
	}
}

// deleteLocked removes a call and closes its sockets and trunk conns. The caller holds mu.
func (s *Server) deleteLocked(id string) {
	c, ok := s.calls[id]
	if !ok {
		return
	}
	delete(s.calls, id)
	for _, st := range c.streams {
		if s.sources[st.ext.rtp] == st {
			delete(s.sources, st.ext.rtp)
		}
		if s.sources[st.ext.rtcp] == st {
			delete(s.sources, st.ext.rtcp)
		}
		for _, sock := range []net.PacketConn{st.sock, st.rtcpSock} {
			if sock != nil {
				_ = sock.Close()
			}
		}
		for _, conn := range []api.Conn{st.extRTP, st.extRTCP} {
			if conn != nil {
				// the conntrack entry closes with its conn
				_ = conn.Close()
			}
		}
	}
	s.log.Debugf("call %s deleted", id)
}

// reap deletes the calls quiet for MediaTimeout.
func (s *Server) reap(stop chan struct{}) {
	t := time.NewTicker(MediaTimeout / 10)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		s.mu.Lock()
		for id, c := range s.calls {
			if time.Since(time.Unix(0, c.last.Load())) > MediaTimeout {
				s.log.Infof("call %s timed out", id)
				s.deleteLocked(id)
			}
		}
		s.mu.Unlock()
	}
}

// isRTCP tells RTCP from RTP by the packet type (RFC 5761 section 4).
func isRTCP(b []byte) bool { return len(b) >= 2 && b[1] >= 192 && b[1] <= 223 }

func addrPort(a net.Addr) netip.AddrPort {
	var ap netip.AddrPort
	switch a := a.(type) {
	case *net.UDPAddr:
		ap = a.AddrPort()
	default:
		ap, _ = netip.ParseAddrPort(a.String())
	}
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
}
