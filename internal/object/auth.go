package object

import (
	"sync/atomic"

	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Auth is an authentication policy. The servers referring to it read its live config per request
// through rt.AuthConfig, which loads the snapshot Reconcile publishes.
type Auth struct {
	name string
	conf atomic.Pointer[stnrv2.AuthConfig]
}

// NewAuth creates an authentication policy object.
func NewAuth(conf stnrv2.Config) (runtime.Object, error) {
	req, ok := conf.(*stnrv2.AuthConfig)
	if !ok {
		return nil, stnrv2.ErrInvalidConf
	}
	a := &Auth{name: req.Name}
	if err := a.Reconcile(req); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Auth) Name() string             { return a.name }
func (a *Auth) Type() runtime.ObjectType { return runtime.TypeAuth }

// Inspect reconciles every change in place.
func (a *Auth) Inspect(old, new stnrv2.Config, _ *stnrv2.StunnerConfig) (runtime.Action, error) {
	req, ok := new.(*stnrv2.AuthConfig)
	if !ok {
		return runtime.ActionNone, stnrv2.ErrInvalidConf
	}
	if old.(*stnrv2.AuthConfig).DeepEqual(req) {
		return runtime.ActionNone, nil
	}
	return runtime.ActionReconcile, nil
}

func (a *Auth) Reconcile(conf stnrv2.Config) error {
	req, ok := conf.(*stnrv2.AuthConfig)
	if !ok {
		return stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return err
	}
	cp := &stnrv2.AuthConfig{}
	req.DeepCopyInto(cp)
	a.conf.Store(cp)
	return nil
}

// GetConfig returns a copy of the live config. Safe for concurrent use.
func (a *Auth) GetConfig() stnrv2.Config {
	cp := &stnrv2.AuthConfig{Name: a.name}
	if c := a.conf.Load(); c != nil {
		c.DeepCopyInto(cp)
	}
	return cp
}

func (a *Auth) Start() error       { return nil }
func (a *Auth) Close(_ bool) error { return nil }

func (a *Auth) Status() stnrv2.Status {
	return a.GetConfig()
}
