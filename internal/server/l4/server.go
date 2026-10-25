// Package l4 implements the L4 server: every client conn becomes a flow to an endpoint of the
// server's clusters. Flows are client-initiated only.
package l4

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/flow"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	"github.com/l7mp/stunner/v2/internal/server/balancer"
	turnsrv "github.com/l7mp/stunner/v2/internal/server/turn"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// FlowTimeout is the idle timeout of a flow, the default TURN allocation lifetime.
var FlowTimeout = 10 * time.Minute

var (
	errQuotaExceeded = errors.New("flow quota exceeded")
	errNoEndpoint    = errors.New("no endpoint to forward to")
)

// Server is an L4 server object.
type Server struct {
	server.Base
	rt     *objruntime.Runtime
	log    logging.LeveledLogger
	idle   time.Duration
	quota  *turnsrv.Quota
	gate   turn.QuotaHandler
	events EventHandler

	mu          sync.RWMutex
	auth        string              // the policy whose realm scopes the quota of the flows
	lbPolicy    stnrv2.LBPolicy     // the load-balancing policy of lb
	lb          balancer.Balancer   // chooses the peer of a new flow among the targets of the clusters
	acceptQueue *server.AcceptQueue // nil while stopped
	conntrack   *flow.Conntrack
}

var (
	_ api.Server        = &Server{}
	_ objruntime.Object = &Server{}
)

// New creates the L4 server of a validated config.
func New(conf *stnrv2.ServerConfig, rt *objruntime.Runtime) (*Server, error) {
	log := rt.Logger.NewLogger(fmt.Sprintf("server-%s", conf.Name))
	// the quota machinery is shared with the TURN server
	q := turnsrv.NewQuotaHandler(rt)
	s := &Server{
		rt:     rt,
		log:    log,
		idle:   FlowTimeout,
		quota:  q,
		gate:   q.QuotaHandler(),
		events: NewEventHandler(rt, log, q),
	}
	s.Init(conf, rt)
	if err := s.Reconcile(conf); err != nil {
		return nil, err
	}
	return s, nil
}

// Reconcile applies the spec and the policy to the next flows.
func (s *Server) Reconcile(conf stnrv2.Config) error {
	if err := s.Base.Reconcile(conf); err != nil {
		return err
	}
	req, _ := s.Desired(conf)
	policy, err := stnrv2.NewLBPolicy(req.L4.LBPolicy)
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
	s.auth = req.L4.Auth
	return nil
}

func (s *Server) GetConfig() stnrv2.Config {
	c := s.Base.GetConfig().(*stnrv2.ServerConfig)
	s.mu.RLock()
	defer s.mu.RUnlock()
	c.L4 = &stnrv2.L4ServerConfig{Auth: s.auth, LBPolicy: s.lbPolicy.String()}
	return c
}

func (s *Server) Status() stnrv2.Status {
	return &stnrv2.ServerStatus{ServerConfig: s.GetConfig().(*stnrv2.ServerConfig), Sessions: s.Sessions()}
}

// Start runs the accept loop.
func (s *Server) Start() error {
	q := server.NewAcceptQueue(server.Backlog)
	s.mu.Lock()
	s.acceptQueue, s.conntrack = q, flow.NewConntrack(s.idle)
	s.mu.Unlock()
	go s.accept(q)
	s.log.Infof("server %s: L4 server running", s.Name())
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
				s.log.Debugf("rejecting flow from client %s: %s", c.RemoteAddr().String(), err.Error())
				_ = c.Close()
				return
			}
			flow.Forward(&f.Entry)
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
	s.mu.RLock()
	ct := s.conntrack
	s.mu.RUnlock()
	if ct == nil {
		return 0
	}
	return ct.Len()
}

// Serve queues a client conn for the accept loop.
func (s *Server) Serve(c api.Conn) (api.Conn, api.Verdict, error) {
	s.mu.RLock()
	q := s.acceptQueue
	s.mu.RUnlock()
	if q == nil {
		return nil, api.Consumed, server.ErrNotServing
	}
	if err := q.Push(c); err != nil {
		return nil, api.Consumed, err
	}
	return nil, api.Consumed, nil
}

// newFlow picks the target, gates the quota, dials the leg and registers the flow.
func (s *Server) newFlow(client api.Conn) (*Flow, error) {
	s.mu.RLock()
	lb := s.lb
	s.mu.RUnlock()
	cluster, dst, ok := lb.Next(s.rt, s.Clusters())
	if !ok {
		return nil, errNoEndpoint
	}
	d, ok := s.rt.Dialer(cluster)
	if !ok {
		return nil, errNoEndpoint
	}

	// the gate reserves the quota: every later failure path must release it
	user, realm := s.flowIdentity(client.RemoteAddr())
	if !s.gate(user, realm, client.RemoteAddr()) {
		return nil, errQuotaExceeded
	}
	quotaRelease := func() {
		s.quota.AllocationHandler(client.RemoteAddr(), client.LocalAddr(), client.Tag().Proto.String(),
			user, realm, turnsrv.AllocationDeleted)
	}

	leg, err := d.Dial(context.Background(), nil, dst.String())
	if err != nil {
		quotaRelease()
		return nil, err
	}

	f := &Flow{Entry: flow.Entry{Client: client, Leg: leg}}
	f.Event = FlowEvent{
		SrcAddr:      client.RemoteAddr(),
		DstAddr:      client.LocalAddr(),
		Protocol:     client.Tag().Proto,
		Peer:         leg.RemoteAddr(),
		PeerProtocol: leg.Tag().Proto,
		Cluster:      cluster,
		Username:     user,
		Realm:        realm,
	}
	f.Hooks = flow.EntryHooks{
		OnError: func(msg string) {
			s.events.OnFlowError(client.RemoteAddr(), f.Event.Protocol, msg)
		},
		OnClose: func(reason string) {
			s.log.Debugf("closing flow from client %s: %s", client.RemoteAddr().String(), reason)
			s.events.OnFlowDeleted(f.Event)
		},
	}
	s.mu.RLock()
	ct := s.conntrack
	s.mu.RUnlock()
	if !ct.Add(&f.Entry) {
		quotaRelease()
		_ = leg.Close()
		return nil, net.ErrClosed
	}
	s.events.OnFlowCreated(f.Event)
	// a tunnelled leg binds its channel after the first write: the offload waits for it
	go func() {
		if err := f.Offload(s.rt.OffloadEngine); err != nil && !errors.Is(err, net.ErrClosed) {
			s.log.Debugf("flow from client %s stays in user space: %s", client.RemoteAddr(), err)
		}
	}()
	return f, nil
}
