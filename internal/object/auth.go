package object

import (
	// "fmt"
	"errors"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Auth is the STUNner authenticator
type Auth struct {
	Type                              v1alpha1.AuthType
	Realm, Username, Password, Secret string
	Log                               logging.LeveledLogger
}

// NewAuth creates a new authenticator.
func NewAuth(conf v1alpha1.Config, logger logging.LoggerFactory) (Object, error) {
	req, ok := conf.(*v1alpha1.AuthConfig)
	if !ok {
		return nil, v1alpha1.ErrInvalidConf
	}

	auth := Auth{Log: logger.NewLogger("stunner-auth")}
	auth.Log.Tracef("NewAuth: %s", req.String())

	if err := auth.Reconcile(req); err != nil && !errors.Is(err, ErrRestartRequired) {
		return nil, err
	}

	return &auth, nil
}

// Inspect examines whether a configuration change requires a reconciliation (returns true if it
// does) or restart (returns ErrRestartRequired).
func (auth *Auth) Inspect(old, new, full v1alpha1.Config) (bool, error) {
	return !old.DeepEqual(new), nil
}

// Reconcile updates the authenticator for a new configuration.
func (auth *Auth) Reconcile(conf v1alpha1.Config) error {
	req, ok := conf.(*v1alpha1.AuthConfig)
	if !ok {
		return v1alpha1.ErrInvalidConf
	}

	if err := req.Validate(); err != nil {
		return err
	}

	// type already validated
	atype, _ := v1alpha1.NewAuthType(req.Type)

	auth.Log.Debugf("using authentication: %s", atype.String())

	// no error: update
	auth.Type = atype
	auth.Realm = req.Realm
	switch atype {
	case v1alpha1.AuthTypePlainText:
		auth.Username = req.Credentials["username"]
		auth.Password = req.Credentials["password"]
	case v1alpha1.AuthTypeLongTerm:
		auth.Secret = req.Credentials["secret"]
	}

	return nil
}

// ObjectName returns the name of the object
func (auth *Auth) ObjectName() string {
	// singleton!
	return v1alpha1.DefaultAuthName
}

// ObjectType returns the type of the object
func (a *Auth) ObjectType() string {
	return "auth"
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

// AuthFactory can create now Auth objects
type AuthFactory struct {
	logger logging.LoggerFactory
}

// NewAuthFactory creates a new factory for Auth objects
func NewAuthFactory(logger logging.LoggerFactory) Factory {
	return &AuthFactory{logger: logger}
}

// New can produce a new Auth object from the given configuration. A nil config will create an
// empty auth object (useful for creating throwaway objects for, e.g., calling Inpect)
func (f *AuthFactory) New(conf v1alpha1.Config) (Object, error) {
	if conf == nil {
		return &Auth{}, nil
	}

	return NewAuth(conf, f.logger)
}
