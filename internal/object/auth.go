package object

import (
	"sync/atomic"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Auth is the STUNner authenticator. TURN handlers read the live auth config per request via
// rt.GetConfig(TypeAuth, ""), which loads the atomic snapshot published by Reconcile.
type Auth struct {
	authType                          stnrv2.AuthType
	realm, username, password, secret string

	// conf is the atomic snapshot read by the auth handler on the request path.
	conf atomic.Pointer[stnrv2.AuthConfig]

	log logging.LeveledLogger
}

// NewAuth creates an Auth object.
func NewAuth(conf stnrv2.Config, rt *runtime.Runtime) (runtime.Object, error) {
	a := &Auth{
		log: rt.Logger.NewLogger("auth"),
	}
	if conf == nil {
		return a, nil
	}
	req, ok := conf.(*stnrv2.AuthConfig)
	if !ok {
		return nil, stnrv2.ErrInvalidConf
	}
	if err := a.Reconcile(req); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Auth) Name() string             { return stnrv2.DefaultAuthName }
func (a *Auth) Type() runtime.ObjectType { return runtime.TypeAuth }

func (a *Auth) Inspect(old, new stnrv2.Config, _ *stnrv2.StunnerConfig) (runtime.Action, error) {
	req, ok := new.(*stnrv2.AuthConfig)
	if !ok {
		return runtime.ActionNone, stnrv2.ErrInvalidConf
	}
	cur := old.(*stnrv2.AuthConfig)
	if !cur.DeepEqual(req) {
		return runtime.ActionReconcile, nil
	}
	return runtime.ActionNone, nil
}

func (a *Auth) Reconcile(conf stnrv2.Config) error {
	req, ok := conf.(*stnrv2.AuthConfig)
	if !ok {
		return stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return err
	}
	atype, _ := stnrv2.NewAuthType(req.Type)
	a.log.Debugf("using authentication: %s", atype.String())

	a.authType = atype
	a.realm = req.Realm
	a.username, a.password, a.secret = "", "", ""
	switch atype {
	case stnrv2.AuthTypeNone:
	case stnrv2.AuthTypeStatic:
		a.username = req.Credentials["username"]
		a.password = req.Credentials["password"]
	case stnrv2.AuthTypeEphemeral:
		a.secret = req.Credentials["secret"]
	}

	// Publish the snapshot for the request path.
	snap := &stnrv2.AuthConfig{
		Type:        atype.String(),
		Realm:       a.realm,
		Credentials: make(map[string]string),
	}
	switch atype {
	case stnrv2.AuthTypeNone:
	case stnrv2.AuthTypeStatic:
		snap.Credentials["username"] = a.username
		snap.Credentials["password"] = a.password
	case stnrv2.AuthTypeEphemeral:
		snap.Credentials["secret"] = a.secret
	}
	a.conf.Store(snap)
	return nil
}

// GetConfig returns a copy of the live auth config. Safe for concurrent use.
func (a *Auth) GetConfig() stnrv2.Config {
	snap := a.conf.Load()
	if snap == nil {
		return &stnrv2.AuthConfig{
			Type:        stnrv2.AuthTypeNone.String(),
			Credentials: map[string]string{},
		}
	}
	out := stnrv2.AuthConfig{
		Type:        snap.Type,
		Realm:       snap.Realm,
		Credentials: make(map[string]string, len(snap.Credentials)),
	}
	for k, v := range snap.Credentials {
		out.Credentials[k] = v
	}
	return &out
}

func (a *Auth) Start() error       { return nil }
func (a *Auth) Close(_ bool) error { return nil }
func (a *Auth) Status() stnrv2.Status {
	return a.GetConfig()
}
