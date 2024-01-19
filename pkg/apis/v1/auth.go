package v1

import (
	"fmt"
	"reflect"
	"strings"
)

// Auth specifies the STUN/TURN authentication mechanism used by STUNner.
type AuthConfig struct {
	// Type of the STUN/TURN authentication mechanism ("static" or "ephemeral"). The deprecated
	// type name "plaintext" is accepted for "static" and the deprecated type name "longterm"
	// is accepted for "ephemeral" for compatibility with older versions.
	Type string `json:"type,omitempty"`
	// Realm defines the STUN/TURN authentication realm.
	Realm string `json:"realm,omitempty"`
	// Credentials specifies the authententication credentials: for "static" at least the keys
	// "username" and "password" must be set, for "ephemeral" the key "secret" specifying the
	// shared authentication secret must be set.
	Credentials map[string]string `json:"credentials"`
}

// Validate checks a configuration and injects defaults.
func (req *AuthConfig) Validate() error {
	if req.Type == "" {
		req.Type = DefaultAuthType
	}

	// Normalize
	atype, err := NewAuthType(req.Type)
	if err != nil {
		return err
	}
	req.Type = atype.String()

	switch atype {
	case AuthTypeStatic:
		_, userFound := req.Credentials["username"]
		_, passFound := req.Credentials["password"]
		if !userFound || !passFound {
			return fmt.Errorf("%s: empty username or password", atype.String())
		}

	case AuthTypeEphemeral:
		_, secretFound := req.Credentials["secret"]
		if !secretFound {
			return fmt.Errorf("cannot handle auth config for type %s: invalid secret",
				atype.String())
		}
	default:
		return fmt.Errorf("invalid authentication type %q", req.Type)
	}

	if req.Realm == "" {
		req.Realm = DefaultRealm
	}

	if req.Credentials == nil {
		req.Credentials = map[string]string{}
	}

	return nil
}

// Name returns the name of the object to be configured.
func (req *AuthConfig) ConfigName() string {
	// Singleton!
	return DefaultAuthName
}

// DeepEqual compares two configurations.
func (req *AuthConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// DeepCopyInto copies a configuration.
func (req *AuthConfig) DeepCopyInto(dst Config) {
	ret := dst.(*AuthConfig)
	*ret = *req
	ret.Credentials = make(map[string]string, len(req.Credentials))
	for k, v := range req.Credentials {
		ret.Credentials[k] = v
	}
}

// String stringifies the configuration.
func (req *AuthConfig) String() string {
	status := []string{}
	if req.Realm != "" {
		status = append(status, fmt.Sprintf("realm=%q", req.Realm))
	}

	if atype, err := NewAuthType(req.Type); err == nil {
		switch atype {
		case AuthTypeStatic:
			u, userFound := req.Credentials["username"]
			if userFound {
				if u == "" {
					u = "<MISSING>"
				} else {
					u = "<SECRET>"
				}
			} else {
				u = "-"
			}
			p, passFound := req.Credentials["password"]
			if passFound {
				if p == "" {
					p = "<MISSING>"
				} else {
					p = "<SECRET>"
				}
			} else {
				p = "-"
			}
			status = append(status, fmt.Sprintf("type=%q,username=%q,password=%q",
				atype.String(), u, p))

		case AuthTypeEphemeral:
			s, secretFound := req.Credentials["secret"]
			if secretFound {
				if s == "" {
					s = "<MISSING>"
				} else {
					s = "<SECRET>"
				}
			} else {
				s = "-"
			}

			status = append(status, fmt.Sprintf("type=%q,shared-secret=%q",
				atype.String(), s))
		}
	}

	return fmt.Sprintf("auth:{%s}", strings.Join(status, ","))
}
