package stunner

import (
	"net"
	"fmt"

	"github.com/pion/logging"
	"github.com/pion/turn/v2"
)

// authType
type AuthType int

const (
	AuthTypePlainText AuthType = iota + 1
	AuthTypeLongTerm
	AuthTypeUnknown
)

const (
	authTypePlainTextStr = "plaintext"
	authTypeLongTermStr = "longterm"
)

func NewAuthType(raw string) (AuthType, error) {
	switch raw {
	case authTypePlainTextStr:
		return AuthTypePlainText, nil
	case authTypeLongTermStr:
		return AuthTypeLongTerm, nil
	default:
		return AuthTypeUnknown, fmt.Errorf("unknown authentication type: %s", raw)
	}
}

func (a AuthType) String() string {
	switch a {
	case AuthTypePlainText:
		return authTypePlainTextStr
	case AuthTypeLongTerm:
		return authTypeLongTermStr
	default:
		return "<unknown>"
	}
}

// Authenticator
type authenticator struct {
	atype AuthType
	log logging.LeveledLogger
	realm, username, password, secret string
	key []byte
	handler turn.AuthHandler	
}

func  (s *Stunner) newAuthenticator(req AuthConfig) (*authenticator, error) {
	auth := authenticator {
		log: s.logger.NewLogger("stunner-auth"),
		realm: s.realm,
	}
	auth.log.Tracef("newAuthenticator: %#v", req)

	// authtype
	atype, err := NewAuthType(req.Type)
	if err != nil {
		return nil, err
	}
	auth.atype = atype

	switch atype {
	case AuthTypePlainText:
		username, userFound := req.Credentials["username"]
		password, passFound := req.Credentials["password"]
		if !userFound || !passFound {
			return nil, fmt.Errorf("cannot handle auth config for type %s: invalid username or password",
				authTypePlainTextStr)
		}
		auth.username = username
		auth.password = password
		auth.key      = turn.GenerateAuthKey(auth.username, auth.realm, auth.password)
		auth.handler  = func(username string, realm string, srcAddr net.Addr) ([]byte, bool) {
			auth.log.Tracef("plain text auth: username=%q realm=%q srcAddr=%v\n", username, realm, srcAddr)
			if username == auth.username {
				return auth.key, true
			}
			return nil, false
		}

	case AuthTypeLongTerm:
		secret, secretFound := req.Credentials["secret"]
		if !secretFound {
			return nil, fmt.Errorf("cannot handle auth config for type %s: invalid secret",
				authTypeLongTermStr)
		}
		auth.secret = secret
		auth.handler = turn.NewLongTermAuthHandler(auth.secret, auth.log)
	}		
	
	auth.log.Infof("using authentication: %s", auth.atype.String())
	return &auth, nil
}

func (auth *authenticator) getConfig() AuthConfig {
	r := AuthConfig{
		Type: auth.atype.String(),
		Credentials: make(map[string]string),
	}
	switch auth.atype {
	case AuthTypePlainText:
		r.Credentials["username"] = auth.username
		r.Credentials["password"] = auth.password
	case AuthTypeLongTerm:
		r.Credentials["secret"] = auth.secret
	}

	return r
}
