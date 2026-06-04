package object

import (
	"github.com/pion/logging"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Auth is the STUNner authenticator. It is queried by TURN handlers at request time via the
// Registry (LookupOne(TypeAuth)) — the runtime cross-reference that used to go through Router.
type Auth struct {
	Type                              stnrv1.AuthType
	Realm, Username, Password, Secret string
	reg                               Registry
	Log                               logging.LeveledLogger
}

// NewAuth creates an Auth object.
func NewAuth(conf stnrv1.Config, reg Registry, rt *Runtime) (Object, error) {
	a := &Auth{
		reg: reg,
		Log: rt.Logger.NewLogger("auth"),
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

func (a *Auth) ObjectName() string { return stnrv1.DefaultAuthName }
func (a *Auth) ObjectType() string { return TypeAuth }

func (a *Auth) Extract(c *stnrv1.StunnerConfig) (stnrv1.Config, error) {
	cp := c.Auth
	return &cp, nil
}

func (a *Auth) Inspect(old, new stnrv1.Config, _ *stnrv1.StunnerConfig) (Action, error) {
	req, ok := new.(*stnrv1.AuthConfig)
	if !ok {
		return ActionNone, stnrv1.ErrInvalidConf
	}
	cur := old.(*stnrv1.AuthConfig)
	if !cur.DeepEqual(req) {
		return ActionReconcile, nil
	}
	return ActionNone, nil
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
	a.Log.Debugf("using authentication: %s", atype.String())

	a.Type = atype
	a.Realm = req.Realm
	a.Username, a.Password, a.Secret = "", "", ""
	switch atype {
	case stnrv1.AuthTypeNone:
	case stnrv1.AuthTypeStatic:
		a.Username = req.Credentials["username"]
		a.Password = req.Credentials["password"]
	case stnrv1.AuthTypeEphemeral:
		a.Secret = req.Credentials["secret"]
	}
	return nil
}

func (a *Auth) GetConfig() stnrv1.Config {
	r := stnrv1.AuthConfig{
		Type:        a.Type.String(),
		Realm:       a.Realm,
		Credentials: make(map[string]string),
	}
	switch a.Type {
	case stnrv1.AuthTypeNone:
	case stnrv1.AuthTypeStatic:
		r.Credentials["username"] = a.Username
		r.Credentials["password"] = a.Password
	case stnrv1.AuthTypeEphemeral:
		r.Credentials["secret"] = a.Secret
	}
	return &r
}

func (a *Auth) Start() error       { return nil }
func (a *Auth) Close(_ bool) error { return nil }
func (a *Auth) Status() stnrv1.Status {
	return a.GetConfig()
}
