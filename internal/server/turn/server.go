// Package turn implements the TURN server: one pion/turn instance serving the client conns handed
// to it, relaying through the server's clusters.
package turn

import (
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
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// PreAllocationTimeout is how long a datagram conn may live without an allocation.
var PreAllocationTimeout = 30 * time.Second

// Server is a TURN server object. Reconcile and the lifecycle write its fields, the data path (the
// pion handlers, Serve) reads them, under mu.
type Server struct {
	server.Base
	rt    *objruntime.Runtime
	log   logging.LeveledLogger
	quota *Quota

	mu   sync.RWMutex
	auth string
	// realm is the realm the running pion instance was built with, and stun whether its policy
	// was of type none, which makes a pure STUN server
	realm string
	stun  bool
	pion  *turn.Server
	queue *server.AcceptQueue // the listener pion accepts from
	// conntrack tracks the served conns for the offload bookkeeping, and to close and reap the
	// ones pion never allocated (pion closes only allocated conns)
	conntrack *flow.Conntrack
	offloads  sync.Map // offload key -> offloadPair
}

var (
	_ api.Server        = &Server{}
	_ objruntime.Object = &Server{}
)

// New creates the TURN server of a validated config.
func New(conf *stnrv2.ServerConfig, rt *objruntime.Runtime) (*Server, error) {
	s := &Server{
		rt:    rt,
		log:   rt.Logger.NewLogger(fmt.Sprintf("server-%s", conf.Name)),
		quota: NewQuotaHandler(rt),
	}
	s.Init(conf, rt)
	if err := s.Reconcile(conf); err != nil {
		return nil, err
	}
	return s, nil
}

// Inspect adds to the base what pion bakes into its instance: a change of the realm of the policy,
// or of whether the policy is of type none, restarts.
func (s *Server) Inspect(old, new stnrv2.Config, full *stnrv2.StunnerConfig) (objruntime.Action, error) {
	action, err := s.Base.Inspect(old, new, full)
	if err != nil || action == objruntime.ActionRestart {
		return action, err
	}
	req, _ := s.Desired(new)
	s.mu.RLock()
	realm, stun := s.realm, s.stun
	s.mu.RUnlock()
	// a missing policy is not none
	if full != nil && realm != "" {
		auth, err := full.GetAuthConfig(req.TURN.Auth)
		if err == nil && auth.Realm != realm {
			return objruntime.ActionRestart, nil
		}
		t, _ := stnrv2.NewAuthType(auth.Type)
		if (err == nil && t == stnrv2.AuthTypeNone) != stun {
			return objruntime.ActionRestart, nil
		}
	}
	return action, nil
}

// Reconcile applies the spec, the filters and the policy to the next requests.
func (s *Server) Reconcile(conf stnrv2.Config) error {
	if err := s.Base.Reconcile(conf); err != nil {
		return err
	}
	req, _ := s.Desired(conf)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auth = req.TURN.Auth
	return nil
}

func (s *Server) GetConfig() stnrv2.Config {
	c := s.Base.GetConfig().(*stnrv2.ServerConfig)
	s.mu.RLock()
	defer s.mu.RUnlock()
	c.TURN = &stnrv2.TURNServerConfig{Auth: s.auth}
	return c
}

func (s *Server) Status() stnrv2.Status {
	return &stnrv2.ServerStatus{ServerConfig: s.GetConfig().(*stnrv2.ServerConfig), Sessions: s.Sessions()}
}

// Start builds the pion instance, in the realm of the policy.
func (s *Server) Start() error {
	s.mu.RLock()
	name := s.auth
	s.mu.RUnlock()
	realm, stun := stnrv2.DefaultRealm, false
	if auth := s.rt.AuthConfig(name); auth != nil {
		t, _ := stnrv2.NewAuthType(auth.Type)
		realm, stun = auth.Realm, t == stnrv2.AuthTypeNone
	}
	queue, ct := server.NewAcceptQueue(server.Backlog), flow.NewConntrack(PreAllocationTimeout)
	pion, err := turn.NewServer(turn.ServerConfig{
		Realm:        realm,
		AuthHandler:  s.AuthHandler(),
		EventHandler: s.eventHandler(ct),
		QuotaHandler: s.quota.QuotaHandler(),
		ListenerConfigs: []turn.ListenerConfig{{
			Listener:              queue,
			RelayAddressGenerator: relayAddressGenerator{s},
			PermissionHandler:     s.permissionHandler,
		}},
		LoggerFactory: s.rt.Logger,
	})
	if err != nil {
		return fmt.Errorf("cannot set up TURN server %s: %w", s.Name(), err)
	}
	s.mu.Lock()
	s.pion, s.queue, s.conntrack, s.realm, s.stun = pion, queue, ct, realm, stun
	s.mu.Unlock()
	s.log.Infof("server %s: TURN server running", s.Name())
	return nil
}

// Close tears down the pion instance and every conn the server serves.
func (s *Server) Close(_ bool) error {
	s.mu.Lock()
	pion, ct := s.pion, s.conntrack
	s.pion, s.queue, s.realm = nil, nil, ""
	s.mu.Unlock()

	var err error
	if pion != nil {
		err = pion.Close()
	}
	if ct != nil {
		ct.Close()
	}
	return err
}

func (s *Server) Sessions() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.pion == nil {
		return 0
	}
	return s.pion.AllocationCount()
}

// Serve hands a client conn to pion's accept loop. Stream conns get STUN framing here, outermost,
// where RFC 6062 ConnectionBind finds the stream underneath.
func (s *Server) Serve(c api.Conn) (api.Conn, api.Verdict, error) {
	s.mu.RLock()
	queue, ct := s.queue, s.conntrack
	s.mu.RUnlock()
	if queue == nil {
		return nil, api.Consumed, server.ErrNotServing
	}

	e := &flow.Entry{Client: c}
	if !ct.Add(e) {
		return nil, api.Consumed, server.ErrNotServing
	}
	// pion closes the conn when its read loop exits, allocated or not: that untracks it
	var conn net.Conn = &trackedConn{Conn: c, entry: e}
	switch c.Tag().Proto {
	case stnrv2.ProtocolTCP, stnrv2.ProtocolTLS:
		// streams hang up: no pre-allocation timeout, and RFC 6062 data connections never
		// allocate
		e.Disarm()
		conn = turn.NewSTUNStreamConn(conn)
	case stnrv2.ProtocolUDP, stnrv2.ProtocolDTLS:
	default:
		ct.Remove(flow.Key(c.RemoteAddr(), c.LocalAddr()))
		return nil, api.Consumed, fmt.Errorf("server %s: cannot serve a %s conn of listener %s "+
			"as TURN", s.Name(), c.Tag().Proto.String(), c.Tag().Name)
	}
	if err := queue.Push(conn); err != nil {
		ct.Remove(flow.Key(c.RemoteAddr(), c.LocalAddr()))
		return nil, api.Consumed, err
	}
	return nil, api.Consumed, nil
}

// trackedConn closes its conntrack entry, and the client conn with it, when it closes.
type trackedConn struct {
	api.Conn
	entry *flow.Entry
}

func (c *trackedConn) Close() error {
	c.entry.Close("client conn closed")
	return nil
}

// dialers returns the dialers of the clusters the server uses, skipping missing ones.
func (s *Server) dialers() []api.Dialer {
	clusters := s.Clusters()
	ret := make([]api.Dialer, 0, len(clusters))
	for _, name := range clusters {
		if d, ok := s.rt.Dialer(name); ok {
			ret = append(ret, d)
		}
	}
	return ret
}
