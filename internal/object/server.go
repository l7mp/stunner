package object

import (
	"fmt"
	"sync/atomic"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	"github.com/l7mp/stunner/v2/internal/server/l4"
	"github.com/l7mp/stunner/v2/internal/server/turn"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Server is a server config and the server it runs.
type Server struct {
	name string

	conf atomic.Pointer[stnrv2.ServerConfig]
	// realm is the auth realm a running TURN server was started with.
	realm string

	server atomic.Pointer[api.Server]

	rt  *runtime.Runtime
	log logging.LeveledLogger
}

// NewServer creates a server object.
func NewServer(conf stnrv2.Config, rt *runtime.Runtime) (runtime.Object, error) {
	req, ok := conf.(*stnrv2.ServerConfig)
	if !ok {
		return nil, stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	s := &Server{
		name: req.Name,
		rt:   rt,
		log:  rt.Logger.NewLogger(fmt.Sprintf("server-%s", req.Name)),
	}
	if err := s.Reconcile(req); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) Name() string             { return s.name }
func (s *Server) Type() runtime.ObjectType { return runtime.TypeServer }

// Inspect restarts on a type change, and a TURN server on a realm change.
func (s *Server) Inspect(old, new stnrv2.Config, full *stnrv2.StunnerConfig) (runtime.Action, error) {
	req, ok := new.(*stnrv2.ServerConfig)
	if !ok {
		return runtime.ActionNone, stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return runtime.ActionNone, err
	}
	cur := old.(*stnrv2.ServerConfig)
	if cur.Type != req.Type {
		return runtime.ActionRestart, nil
	}
	if req.Type == stnrv2.ServerTypeTURN.String() && full != nil && s.realm != "" &&
		full.Auth.Realm != s.realm {
		return runtime.ActionRestart, nil
	}
	if cur.DeepEqual(req) {
		return runtime.ActionNone, nil
	}
	return runtime.ActionReconcile, nil
}

func (s *Server) Reconcile(conf stnrv2.Config) error {
	req, ok := conf.(*stnrv2.ServerConfig)
	if !ok {
		return stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return err
	}
	if s.name != req.Name {
		return fmt.Errorf("cannot rename server %q to %q", s.name, req.Name)
	}
	cp := &stnrv2.ServerConfig{}
	req.DeepCopyInto(cp)
	s.conf.Store(cp)
	return nil
}

func (s *Server) GetConfig() stnrv2.Config {
	cp := &stnrv2.ServerConfig{Name: s.name}
	if c := s.conf.Load(); c != nil {
		c.DeepCopyInto(cp)
	}
	return cp
}

// Start starts the server.
func (s *Server) Start() error {
	var srv api.Server
	t, _ := stnrv2.NewServerType(s.conf.Load().Type)
	switch t {
	case stnrv2.ServerTypeTURN:
		srv = turn.NewServer(s.name, s.rt)
	case stnrv2.ServerTypeL4:
		srv = l4.NewServer(s.name, s.rt)
	default:
		return fmt.Errorf("unsupported server type %q", s.conf.Load().Type)
	}
	if err := srv.Start(); err != nil {
		return err
	}
	s.realm = stnrv2.DefaultRealm
	if auth, ok := s.rt.GetConfig(runtime.TypeAuth, "").(*stnrv2.AuthConfig); ok && auth != nil {
		s.realm = auth.Realm
	}
	s.server.Store(&srv)
	return nil
}

// Close stops the server; it refuses conns until it starts again.
func (s *Server) Close(shutdown bool) error {
	s.realm = ""
	if old := s.server.Swap(nil); old != nil {
		return (*old).Close(shutdown)
	}
	return nil
}

func (s *Server) Status() stnrv2.Status {
	return &stnrv2.ServerStatus{
		ServerConfig: s.GetConfig().(*stnrv2.ServerConfig),
		Sessions:     s.Sessions(),
	}
}

func (s *Server) Sessions() int {
	if srv := s.server.Load(); srv != nil {
		return (*srv).Sessions()
	}
	return 0
}

func (s *Server) Serve(c api.Conn) (api.Conn, api.Verdict, error) {
	if srv := s.server.Load(); srv != nil {
		return (*srv).Serve(c)
	}
	return nil, api.Consumed, server.ErrNotServing
}
