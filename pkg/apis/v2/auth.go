package v2

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec
	"encoding/base64"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// AuthConfig is an authentication policy: the credentials a TURN server checks its clients
// against, a tunnel presents to its upstream TURN server.
type AuthConfig struct {
	// Name of the policy, the name servers refer to it by. Default is "default".
	Name string `json:"name,omitempty"`
	// Type of the STUN/TURN authentication mechanism ("static" or "ephemeral"). The deprecated
	// type name "plaintext" is accepted for "static" and the deprecated type name "longterm"
	// is accepted for "ephemeral" for compatibility with older versions.
	Type string `json:"type,omitempty"`
	// Realm defines the STUN/TURN authentication realm.
	Realm string `json:"realm,omitempty"`
	// Credentials specifies the authentication credentials: for "static" at least the keys
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

// GenerateCredentials generates the credentials a TURN client of the policy authenticates with,
// a fresh pair on every call: for "static" the configured username and password, for "ephemeral"
// a time-windowed credential of the user id from the shared secret, expiring at expiry, or after
// the configured lifetime for a zero expiry. A nil policy and "none" both yield empty credentials,
// an anonymous session.
func (req *AuthConfig) GenerateCredentials(userid string, expiry time.Time) (string, string, error) {
	if req == nil {
		return "", "", nil
	}

	atype, err := NewAuthType(req.Type)
	if err != nil {
		return "", "", err
	}

	switch atype {
	case AuthTypeNone:
		return "", "", nil

	case AuthTypeStatic:
		username, userFound := req.Credentials["username"]
		password, passFound := req.Credentials["password"]
		if !userFound || !passFound {
			return "", "", fmt.Errorf("no username or password found in %s auth config",
				atype.String())
		}
		return username, password, nil

	case AuthTypeEphemeral:
		secret, found := req.Credentials["secret"]
		if !found {
			return "", "", fmt.Errorf("no secret found in %s auth config", atype.String())
		}
		if expiry.IsZero() {
			expiry = time.Now().Add(req.CredentialLifetime())
		}
		username := GenerateTimeWindowedUsername(expiry, userid)
		password, err := GetLongTermCredential(username, secret)
		return username, password, err

	default:
		return "", "", fmt.Errorf("unknown authentication type %q", req.Type)
	}
}

// UsernameSeparator separates the expiry timestamp and the user id in a time-windowed username, as
// per the "REST API For Access To TURN Services"
// (https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00) spec.
const UsernameSeparator = ":"

// GenerateTimeWindowedUsername creates a time-windowed username of the expiry UNIX timestamp and
// the user id, or only the timestamp for an empty user id, as per the "REST API For Access To
// TURN Services" (https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00) spec.
func GenerateTimeWindowedUsername(expiry time.Time, userid string) string {
	timeUsername := strconv.FormatInt(expiry.Unix(), 10)
	if userid == "" {
		return timeUsername
	}
	return timeUsername + UsernameSeparator + userid
}

// GetLongTermCredential creates the password of a time-windowed username from the shared secret.
func GetLongTermCredential(username string, sharedSecret string) (string, error) {
	mac := hmac.New(sha1.New, []byte(sharedSecret))
	if _, err := mac.Write([]byte(username)); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
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

// ConfigName returns the name of the object to be configured.
func (req *AuthConfig) ConfigName() string {
	return req.Name
}

// DeepEqual compares two configurations.
func (req *AuthConfig) DeepEqual(other Config) bool {
	return reflect.DeepEqual(req, other)
}

// DeepCopyInto copies a configuration.
func (req *AuthConfig) DeepCopyInto(dst Config) {
	ret := dst.(*AuthConfig)
	*ret = *req
	ret.Credentials = maps.Clone(req.Credentials)
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

	n := ""
	if req.Name != "" {
		n = fmt.Sprintf("%q:", req.Name)
	}
	return fmt.Sprintf("%s%s-auth:{%s}", n, req.Type, strings.Join(status, ","))
}

// AuthStatus represents the authentication status.
type AuthStatus = AuthConfig
