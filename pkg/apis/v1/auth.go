package v1

import (
	"fmt"
	"reflect"
	"strings"
	"time"
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
	// Lifetime bounds the validity of a generated "ephemeral" credential, as a duration string
	// ("1h"). It applies only where credentials are generated, that is, where STUNner acts as a
	// TURN client authenticating to a server; a server checking a credential reads the expiry
	// from the credential itself. Default: 1h.
	Lifetime string `json:"lifetime,omitempty"`
}

// CredentialLifetime returns the validity of generated ephemeral credentials, falling back to the
// default for an unset or unparseable value. Validate rejects the latter, so a validated config
// always yields what it says.
func (req *AuthConfig) CredentialLifetime() time.Duration {
	d, err := time.ParseDuration(req.Lifetime)
	if err != nil {
		return DefaultCredentialLifetime
	}
	return d
}

// Validate checks a configuration and injects defaults.
func (req *AuthConfig) Validate() error {
	if req.Type == "" {
		req.Type = DefaultAuthType
	}

	// Normalize. Missing credentials are no config error: a TURN server refuses every client.
	atype, err := NewAuthType(req.Type)
	if err != nil {
		return err
	}
	req.Type = atype.String()

	if req.Lifetime != "" {
		if _, err := time.ParseDuration(req.Lifetime); err != nil {
			return fmt.Errorf("invalid credential lifetime %q: %w", req.Lifetime, err)
		}
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
		case AuthTypeNone:
			// no auth
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
			status = append(status, fmt.Sprintf("username=%q,password=%q", u, p))

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

			status = append(status, fmt.Sprintf("secret=%q", s))
			if req.Lifetime != "" {
				status = append(status, fmt.Sprintf("lifetime=%q", req.Lifetime))
			}
		}
	}

	return fmt.Sprintf("%s-auth:{%s}", req.Type, strings.Join(status, ","))
}

// AuthStatus represents the authentication status.
type AuthStatus = AuthConfig
