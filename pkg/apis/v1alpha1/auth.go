package v1alpha1

import (
	"fmt"
	"reflect"
)

// Auth defines the specification of the STUN/TURN authentication mechanism used by STUNner
type AuthConfig struct {
	// Type is the type of the STUN/TURN authentication mechanism ("plaintext" or "longterm")
	Type string `json:"type,omitempty"`
	// Realm defines the STUN/TURN realm to be used for STUNner
	Realm string `json:"realm,omitempty"`
	// Credentials specifies the authententication credentials: for "plaintext" at least the
	// keys "username" and "password" must be set, for "longterm" the key "secret" will hold
	// the shared authentication secret
	Credentials map[string]string `json:"credentials"`
}

// Validate checks a configuration and injects defaults
func (req *AuthConfig) Validate() error {
	if req.Type == "" {
		req.Type = DefaultAuthType
	}
	if _, err := NewAuthType(req.Type); err != nil {
		return err
	}
	if req.Realm == "" {
		req.Realm = DefaultRealm
	}

	atype, err := NewAuthType(req.Type)
	if err != nil {
		return err
	}

	switch atype {
	case AuthTypePlainText:
		_, userFound := req.Credentials["username"]
		_, passFound := req.Credentials["password"]
		if !userFound || !passFound {
			return fmt.Errorf("%s: empty username or password", atype.String())
		}

	case AuthTypeLongTerm:
		_, secretFound := req.Credentials["secret"]
		if !secretFound {
			return fmt.Errorf("cannot handle auth config for type %s: invalid secret",
				atype.String())
		}
	default:
		return fmt.Errorf("invalid authentication type %q", req.Type)
	}

	return nil
}

// Name returns the name of the object to be configured
func (req *AuthConfig) ConfigName() string {
	// singleton!
	return DefaultAuthName
}

// DeepEqual compares two configurations
func (req *AuthConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// String stringifies the configuration
func (req *AuthConfig) String() string {
	return fmt.Sprintf("%#v", req)
}
