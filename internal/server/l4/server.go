// Package l4 implements the L4 server: every client conn becomes a flow to an endpoint of the
// server's clusters. Flows are client-initiated only.
package l4

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/v2/internal/api"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	turnsrv "github.com/l7mp/stunner/v2/internal/server/turn"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// FlowTimeout is the idle timeout of a flow, the default TURN allocation lifetime.
var FlowTimeout = 10 * time.Minute

var (
	errQuotaExceeded = errors.New("flow quota exceeded")
	errNoEndpoint    = errors.New("no endpoint to forward to")
)

// Server is an L4 server.
type Server struct {
	name    string
	rt      *objruntime.Runtime
	log     logging.LeveledLogger
	idle    time.Duration
	quota   *turnsrv.Quota
	gate    turn.QuotaHandler
	events  EventHandler
	offload *OffloadHandler

	mu          sync.Mutex
	acceptQueue *server.AcceptQueue // nil while stopped
	conntrack   *server.Conntrack
}

func NewServer(name string, rt *objruntime.Runtime) *Server {
	log := rt.Logger.NewLogger(fmt.Sprintf("server-%s", name))
	// the quota machinery is shared with the TURN server
	q := turnsrv.NewQuotaHandler(rt)
	return &Server{
		name:    name,
		rt:      rt,
		log:     log,
		idle:    FlowTimeout,
		quota:   q,
		gate:    q.QuotaHandler(),
		events:  NewEventHandler(rt, log, q),
		offload: NewOffloadHandler(rt, log),
	}
}

func (s *Server) Name() string { return s.name }

// Start runs the accept loop.
func (s *Server) Start() error {
	q := server.NewAcceptQueue(server.Backlog)
	s.mu.Lock()
	s.acceptQueue, s.conntrack = q, server.NewConntrack(s.idle)
	s.mu.Unlock()
	go s.accept(q)
	s.log.Infof("server %s: L4 server running", s.name)
	return nil
}

// accept starts a flow per accepted conn until the queue closes.
func (s *Server) accept(q *server.AcceptQueue) {
	for {
		c, err := q.Accept()
		if err != nil {
			return
		}
		go func(c api.Conn) {
			f, err := s.newFlow(c)
			if err != nil {
				s.log.Infof("rejecting flow from client %s: %s", c.RemoteAddr().String(), err.Error())
				_ = c.Close()
				return
			}
			server.Forward(&f.Entry)
		}(c.(api.Conn))
	}
}

// Close tears down every flow, client conns included.
func (s *Server) Close(_ bool) error {
	s.mu.Lock()
	q, ct := s.acceptQueue, s.conntrack
	s.acceptQueue = nil
	s.mu.Unlock()
	if q != nil {
		_ = q.Close()
	}
	if ct != nil {
		ct.Close()
	}
	return nil
}

// Sessions returns the number of live flows.
func (s *Server) Sessions() int {
	s.mu.Lock()
	ct := s.conntrack
	s.mu.Unlock()
	if ct == nil {
		return 0
	}
	return ct.Len()
}

// Serve queues a client conn for the accept loop.
func (s *Server) Serve(c api.Conn) (api.Conn, api.Verdict, error) {
	s.mu.Lock()
	q := s.acceptQueue
	s.mu.Unlock()
	if q == nil {
		return nil, api.Consumed, server.ErrNotServing
	}
	if err := q.Push(c); err != nil {
		return nil, api.Consumed, err
	}
	return nil, api.Consumed, nil
}

// target picks the target of a new flow from the first cluster that routes one.
func (s *Server) target() (addr string, d api.Dialer, err error) {
	conf := s.rt.ServerConfig(s.name)
	if conf == nil {
		return "", nil, errNoEndpoint
	}
	for _, name := range conf.Clusters {
		r, found := s.rt.Router(name)
		if !found {
			continue
		}
		dst, ok, err := r.Route(netip.AddrPort{})
		if err != nil {
			s.log.Warnf("server %q: skipping cluster %q: %s", s.name, name, err.Error())
			continue
		}
		if d, found := s.rt.Dialer(name); ok && found {
			return dst.String(), d, nil
		}
	}
	return "", nil, errNoEndpoint
}

// newFlow picks the target, gates the quota, dials the leg and registers the flow.
func (s *Server) newFlow(client api.Conn) (*Flow, error) {
	addr, d, err := s.target()
	if err != nil {
		return nil, err
	}

	// the gate reserves the quota: every later failure path must release it
	user, realm := flowIdentity(s.rt, client.RemoteAddr())
	if !s.gate(user, realm, client.RemoteAddr()) {
		return nil, errQuotaExceeded
	}
	quotaRelease := func() {
		s.quota.AllocationHandler(client.RemoteAddr(), client.LocalAddr(), client.Tag().Proto.String(),
			user, realm, turnsrv.AllocationDeleted)
	}

	leg, err := d.Dial(context.Background(), nil, addr)
	if err != nil {
		quotaRelease()
		return nil, err
	}

	tunnel := stnrv2.ProtocolUnknown
	if conf := s.rt.ClusterConfig(d.Name()); conf != nil && conf.Tunnel != nil {
		if u, err := stnrv2.ParseURI(conf.Tunnel.URL); err == nil {
			tunnel = u.Protocol
		}
	}

	f := &Flow{Entry: server.Entry{Client: client, Leg: leg}}
	f.Event = FlowEvent{
		SrcAddr:      client.RemoteAddr(),
		DstAddr:      client.LocalAddr(),
		Protocol:     client.Tag().Proto,
		Peer:         leg.RemoteAddr(),
		PeerProtocol: leg.Tag().Proto,
		Cluster:      d.Name(),
		Tunnel:       tunnel,
		Username:     user,
		Realm:        realm,
	}
	// a leg reports its own wire: a direct leg leaves from its socket towards the peer, a leg
	// through an upstream TURN server from the session's transport socket towards the server
	f.Event.RelayAddr, f.Event.ServerAddr = leg.TransportAddrs()
	if f.Event.Tunnel == stnrv2.ProtocolUnknown {
		f.Event.ServerAddr = nil
	}

	f.Hooks = server.EntryHooks{
		Active: func() (bool, bool) { return s.offload.Active(f) },
		OnError: func(msg string) {
			s.events.OnFlowError(client.RemoteAddr(), f.Event.Protocol, msg)
		},
		OnClose: func(reason string) {
			s.log.Debugf("closing flow from client %s: %s", client.RemoteAddr().String(), reason)
			s.offload.Remove(f)
			s.events.OnFlowDeleted(f.Event)
		},
	}
	s.mu.Lock()
	ct := s.conntrack
	s.mu.Unlock()
	if !ct.Add(&f.Entry) {
		quotaRelease()
		_ = leg.Close()
		return nil, net.ErrClosed
	}
	s.events.OnFlowCreated(f.Event)
	s.offload.Upsert(f)
	return f, nil
}
