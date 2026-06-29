package object

import (
	"sync/atomic"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Auth is the STUNner authenticator. TURN handlers read the live auth config per request via
// rt.GetConfig(TypeAuth, ""), which loads the atomic snapshot published by Reconcile.
type Auth struct {
	authType                          stnrv1.AuthType
	realm, username, password, secret string

	// conf is the atomic snapshot read by the auth handler on the request path.
	conf atomic.Pointer[stnrv1.AuthConfig]

	log logging.LeveledLogger
}

// NewAuth creates an Auth object.
func NewAuth(conf stnrv1.Config, rt *runtime.Runtime) (runtime.Object, error) {
	a := &Auth{
		log: rt.Logger.NewLogger("auth"),
	}
	if conf == nil {
		return a, nil
	}
	req, ok := conf.(*stnrv1.AuthConfig)
	if !ok {
		return nil, stnrv1.ErrInvalidConf
	}
	if err := a.Reconcile(req); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Auth) Name() string             { return stnrv1.DefaultAuthName }
func (a *Auth) Type() runtime.ObjectType { return runtime.TypeAuth }

func (a *Auth) Inspect(old, new stnrv1.Config, _ *stnrv1.StunnerConfig) (runtime.Action, error) {
	req, ok := new.(*stnrv1.AuthConfig)
	if !ok {
		return runtime.ActionNone, stnrv1.ErrInvalidConf
	}
	cur := old.(*stnrv1.AuthConfig)
	if !cur.DeepEqual(req) {
		return runtime.ActionReconcile, nil
	}
	return runtime.ActionNone, nil
}

func (a *Auth) Reconcile(conf stnrv1.Config) error {
	req, ok := conf.(*stnrv1.AuthConfig)
	if !ok {
		return stnrv1.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return err
	}
	atype, _ := stnrv1.NewAuthType(req.Type)
	a.log.Debugf("using authentication: %s", atype.String())

	a.authType = atype
	a.realm = req.Realm
	a.username, a.password, a.secret = "", "", ""
	switch atype {
	case stnrv1.AuthTypeNone:
	case stnrv1.AuthTypeStatic:
		a.username = req.Credentials["username"]
		a.password = req.Credentials["password"]
	case stnrv1.AuthTypeEphemeral:
		a.secret = req.Credentials["secret"]
	}

	// Publish the snapshot for the request path.
	snap := &stnrv1.AuthConfig{
		Type:        atype.String(),
		Realm:       a.realm,
		Credentials: make(map[string]string),
	}
	switch atype {
	case stnrv1.AuthTypeNone:
	case stnrv1.AuthTypeStatic:
		snap.Credentials["username"] = a.username
		snap.Credentials["password"] = a.password
	case stnrv1.AuthTypeEphemeral:
		snap.Credentials["secret"] = a.secret
	}
	a.conf.Store(snap)
	return nil
}

// GetConfig returns a copy of the live auth config. Safe for concurrent use.
func (a *Auth) GetConfig() stnrv1.Config {
	snap := a.conf.Load()
	if snap == nil {
		return &stnrv1.AuthConfig{
			Type:        stnrv1.AuthTypeNone.String(),
			Credentials: map[string]string{},
		}
	}
	out := stnrv1.AuthConfig{
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
func (a *Auth) Status() stnrv1.Status {
	return a.GetConfig()
}
