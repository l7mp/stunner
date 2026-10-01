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
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// PreAllocationTimeout is how long a datagram conn may live without an allocation.
var PreAllocationTimeout = 30 * time.Second

// Server is a TURN server.
type Server struct {
	name  string
	rt    *objruntime.Runtime
	log   logging.LeveledLogger
	quota *Quota

	mu    sync.Mutex
	pion  *turn.Server
	queue *server.AcceptQueue // the listener pion accepts from
	// conntrack tracks the served conns for the offload bookkeeping, and to close and reap the
	// ones pion never allocated (pion closes only allocated conns)
	conntrack *server.Conntrack
	offloads  sync.Map // offload key -> offloadPair
}

func NewServer(name string, rt *objruntime.Runtime) *Server {
	return &Server{
		name:  name,
		rt:    rt,
		log:   rt.Logger.NewLogger(fmt.Sprintf("server-%s", name)),
		quota: NewQuotaHandler(rt),
	}
}

func (s *Server) Name() string { return s.name }

// Start builds the pion instance.
func (s *Server) Start() error {
	realm := stnrv2.DefaultRealm
	if auth, ok := s.rt.GetConfig(objruntime.TypeAuth, "").(*stnrv2.AuthConfig); ok && auth != nil {
		realm = auth.Realm
	}
	// the handlers of a pion instance use its own conntrack: they outlive a restart
	queue, ct := server.NewAcceptQueue(server.Backlog), server.NewConntrack(PreAllocationTimeout)
	pion, err := turn.NewServer(turn.ServerConfig{
		Realm:        realm,
		AuthHandler:  NewAuthHandler(s.rt, s.log),
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
		return fmt.Errorf("cannot set up TURN server %s: %w", s.name, err)
	}
	s.mu.Lock()
	s.pion, s.queue, s.conntrack = pion, queue, ct
	s.mu.Unlock()
	s.log.Infof("server %s: TURN server running", s.name)
	return nil
}

// Close tears down the pion instance and every conn the server serves.
func (s *Server) Close(_ bool) error {
	s.mu.Lock()
	pion, ct := s.pion, s.conntrack
	s.pion, s.queue = nil, nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pion == nil {
		return 0
	}
	return s.pion.AllocationCount()
}

// Serve hands a client conn to pion's accept loop. Stream conns get STUN framing here, outermost,
// where RFC 6062 ConnectionBind finds the stream underneath.
func (s *Server) Serve(c api.Conn) (api.Conn, api.Verdict, error) {
	s.mu.Lock()
	queue, ct := s.queue, s.conntrack
	s.mu.Unlock()
	if queue == nil {
		return nil, api.Consumed, server.ErrNotServing
	}

	e := &server.Entry{Client: c}
	if !ct.Add(e) {
		return nil, api.Consumed, server.ErrNotServing
	}
	var conn net.Conn = c
	switch c.Tag().Proto {
	case stnrv2.ProtocolTCP, stnrv2.ProtocolTLS:
		// streams hang up: no pre-allocation timeout, and RFC 6062 data connections never
		// allocate
		e.Disarm()
		conn = turn.NewSTUNStreamConn(c)
	case stnrv2.ProtocolUDP, stnrv2.ProtocolDTLS:
	default:
		s.log.Warnf("server %s: serving a %s client conn from listener %s as TURN", s.name,
			c.Tag().Proto.String(), c.Tag().Name)
	}
	if err := queue.Push(conn); err != nil {
		ct.Remove(server.Key(c.RemoteAddr(), c.LocalAddr()))
		return nil, api.Consumed, err
	}
	return nil, api.Consumed, nil
}

// dialers returns the dialers of the clusters the server's config names, skipping missing ones.
func (s *Server) dialers() []api.Dialer {
	conf := s.rt.ServerConfig(s.name)
	if conf == nil {
		return nil
	}
	ret := make([]api.Dialer, 0, len(conf.Clusters))
	for _, name := range conf.Clusters {
		if d, ok := s.rt.Dialer(name); ok {
			ret = append(ret, d)
		}
	}
	return ret
}
