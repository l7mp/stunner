package server

import (
	"fmt"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Unimplemented is a server of a type this build has no implementation of: it keeps the options
// of its type as they are, warns on start and serves no conn.
type Unimplemented struct {
	Base
	log logging.LeveledLogger

	sip *stnrv2.SIPServerConfig
	rtp *stnrv2.RTPServerConfig
}

// NewUnimplemented creates the server of a validated config of a type this build does not
// implement.
func NewUnimplemented(conf *stnrv2.ServerConfig, rt *runtime.Runtime) (*Unimplemented, error) {
	s := &Unimplemented{log: rt.Logger.NewLogger(fmt.Sprintf("server-%s", conf.Name))}
	s.Init(conf)
	if err := s.Reconcile(conf); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Unimplemented) Reconcile(conf stnrv2.Config) error {
	if err := s.Base.Reconcile(conf); err != nil {
		return err
	}
	req, _ := s.Desired(conf)
	cp := &stnrv2.ServerConfig{}
	req.DeepCopyInto(cp)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sip, s.rtp = cp.SIP, cp.RTP
	return nil
}

func (s *Unimplemented) GetConfig() stnrv2.Config {
	c := s.Base.GetConfig().(*stnrv2.ServerConfig)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.sip != nil {
		sip := *s.sip
		c.SIP = &sip
	}
	if s.rtp != nil {
		rtp := *s.rtp
		c.RTP = &rtp
	}
	return c
}

func (s *Unimplemented) Status() stnrv2.Status {
	return &stnrv2.ServerStatus{ServerConfig: s.GetConfig().(*stnrv2.ServerConfig)}
}

func (s *Unimplemented) Start() error {
	s.log.Warnf("server %s: type %s has no implementation in this build, its conns go unserved",
		s.Name(), s.typ.String())
	return nil
}
