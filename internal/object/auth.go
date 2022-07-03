package object

import (
	"fmt"

	"github.com/pion/logging"
	// "github.com/pion/turn/v2"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Auth is the STUNner authenticator
type Auth struct {
	Type                              v1alpha1.AuthType
	Realm, Username, Password, Secret string
	Log                               logging.LeveledLogger
}

// NewAuth creates a new authenticator. Requires a server restart (returns v1alpha1.ErrRestartRequired)
func NewAuth(conf v1alpha1.Config, logger logging.LoggerFactory) (Object, error) {
	req, ok := conf.(*v1alpha1.AuthConfig)
	if !ok {
		return nil, v1alpha1.ErrInvalidConf
	}

	auth := Auth{Log: logger.NewLogger("stunner-auth")}
	auth.Log.Tracef("NewAuth: %#v", req)

	if err := auth.Reconcile(req); err != nil && err != v1alpha1.ErrRestartRequired {
		return nil, err
	}

	return &auth, v1alpha1.ErrRestartRequired
}

// Reconcile updates the authenticator for a new configuration. Does not requre a server restart but existing TURN connections may fail to refresh permissions
func (auth *Auth) Reconcile(conf v1alpha1.Config) error {
	req, ok := conf.(*v1alpha1.AuthConfig)
	if !ok {
		return v1alpha1.ErrInvalidConf
	}

	auth.Log.Tracef("Reconcile: %#v", req)

	if err := req.Validate(); err != nil {
		return err
	}

	// type already validated
	atype, _ := v1alpha1.NewAuthType(req.Type)
	auth.Log.Infof("using authentication: %s", atype.String())

	switch atype {
	case v1alpha1.AuthTypePlainText:
		_, userFound := req.Credentials["username"]
		_, passFound := req.Credentials["password"]
		if !userFound || !passFound {
			return fmt.Errorf("%s: empty username or password", atype.String())
		}

	case v1alpha1.AuthTypeLongTerm:
		_, secretFound := req.Credentials["secret"]
		if !secretFound {
			return fmt.Errorf("cannot handle auth config for type %s: invalid secret",
				auth.Type.String())
		}
	}

	// no error: update
	auth.Type = atype
	auth.Realm = req.Realm
	switch atype {
	case v1alpha1.AuthTypePlainText:
		auth.Username, _ = req.Credentials["username"]
		auth.Password, _ = req.Credentials["password"]
	case v1alpha1.AuthTypeLongTerm:
		auth.Secret, _ = req.Credentials["secret"]
	}

	return nil
}

// Name returns the name of the object
func (auth *Auth) ObjectName() string {
	// singleton!
	return v1alpha1.DefaultAuthName
}

// GetConfig returns the configuration of the running authenticator
func (auth *Auth) GetConfig() v1alpha1.Config {
	auth.Log.Tracef("GetConfig")
	r := v1alpha1.AuthConfig{
		Type:        auth.Type.String(),
		Realm:       auth.Realm,
		Credentials: make(map[string]string),
	}
	switch auth.Type {
	case v1alpha1.AuthTypePlainText:
		r.Credentials["username"] = auth.Username
		r.Credentials["password"] = auth.Password
	case v1alpha1.AuthTypeLongTerm:
		r.Credentials["secret"] = auth.Secret
	}

	return &r
}

// Close closes the authenticator
func (auth *Auth) Close() error {
	auth.Log.Tracef("Close")
	return nil
}
