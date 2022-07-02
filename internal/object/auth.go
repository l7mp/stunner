package object

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec,gci
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v2"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Auth is the STUNner authenticator
type Auth struct {
	Type                              v1alpha1.AuthType
	Realm, Username, Password, Secret string
	key                               []byte
	Handler                           turn.AuthHandler
	log                               logging.LeveledLogger
}

// NewAuth creates a new authenticator. Requires a server restart (returns v1alpha1.ErrRestartRequired)
func NewAuth(conf v1alpha1.Config, logger logging.LoggerFactory) (Object, error) {
	req, ok := conf.(*v1alpha1.AuthConfig)
	if !ok {
		return nil, v1alpha1.ErrInvalidConf
	}

	auth := Auth{log: logger.NewLogger("stunner-auth")}
	auth.log.Tracef("NewAuth: %#v", req)

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

	auth.log.Tracef("Reconcile: %#v", req)

	if err := req.Validate(); err != nil {
		return err
	}

	atype, _ := v1alpha1.NewAuthType(req.Type)
	auth.log.Infof("using authentication: %s", atype.String())
	var key []byte
	var handler turn.AuthHandler

	switch atype {
	case v1alpha1.AuthTypePlainText:
		username, userFound := req.Credentials["username"]
		password, passFound := req.Credentials["password"]
		if !userFound || !passFound {
			return fmt.Errorf("%s: empty username or password", atype.String())
		}

		key = turn.GenerateAuthKey(username, req.Realm, password)
		handler = func(username string, realm string, srcAddr net.Addr) ([]byte, bool) {
			auth.log.Infof("plaintext auth request: username=%q realm=%q srcAddr=%v\n",
				username, realm, srcAddr)

			if username == auth.Username {
				return auth.key, true
			}

			return nil, false
		}
	case v1alpha1.AuthTypeLongTerm:
		secret, secretFound := req.Credentials["secret"]
		if !secretFound {
			return fmt.Errorf("cannot handle auth config for type %s: invalid secret",
				auth.Type.String())
		}
		// handler = turn.NewLongTermAuthHandler(secret, auth.log)
		handler = func(username, realm string, srcAddr net.Addr) (key []byte, ok bool) {
			auth.log.Infof("longterm auth request: username=%q realm=%q srcAddr=%v",
				username, realm, srcAddr)

			t, err := strconv.Atoi(username)
			if err != nil {
				auth.log.Errorf("invalid time-windowed username %q", username)
				return nil, false
			}

			if int64(t) < time.Now().Unix() {
				auth.log.Errorf("expired time-windowed username %q", username)
				return nil, false
			}

			// password, err := longTermCredentials(username, secret)
			mac := hmac.New(sha1.New, []byte(secret))
			_, err = mac.Write([]byte(username))
			if err != nil {
				auth.log.Errorf("failed do hash username: %w", err.Error())
				return nil, false
			}
			password := base64.StdEncoding.EncodeToString(mac.Sum(nil))

			return turn.GenerateAuthKey(username, realm, password), true
		}
	}

	// no error: update
	auth.Type = atype
	auth.Realm = req.Realm
	auth.Handler = handler
	switch atype {
	case v1alpha1.AuthTypePlainText:
		auth.Username, _ = req.Credentials["username"]
		auth.Password, _ = req.Credentials["password"]
		auth.key = key
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
	auth.log.Tracef("GetConfig")
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
	auth.log.Tracef("Close")
	return nil
}
