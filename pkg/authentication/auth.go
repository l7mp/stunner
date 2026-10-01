// Package auth contains variuos routines to generate and check STUNner authentication credentials.
package authentication

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec,gci
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pion/turn/v5"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// UsernameSeparator is the separator character used in time-windowed TURN authentication as
// defined in "REST API For Access To TURN Services"
// (https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00).
const UsernameSeparator = ":"

// AuthHandler specifies type of the TURN authentication handler used in Stunner. Re-exported from pion/turn for completeness.
type AuthHandler = turn.AuthHandler

// PermissionHandler specifies type of the TURN permission handler used in Stunner. Re-exported from pion/turn for completeness.
type PermissionHandler = turn.PermissionHandler

// GenerateTimeWindowedUsername creates a time-windowed username consisting of a validity timestamp
// and an arbitrary user id, as per the "REST API For Access To TURN Services"
// (https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00) spec.
func GenerateTimeWindowedUsername(startTime time.Time, duration time.Duration, userid string) string {
	endTime := startTime.Add(duration).Unix()
	timeUsername := strconv.FormatInt(endTime, 10)
	return timeUsername + ":" + userid
}

// CheckTimeWindowedUsername checks the validity of an authentication credential. The method mostly
// follows the "REST API For Access To TURN Services"
// (https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00) spec with the exception
// that we make more effort to find out which component of the username is the UNIX timestamp: we
// find the first thing that looks like a UNIX timestamp in the username and use that for checking
// the time-windowed credential, and reject everything else.
func CheckTimeWindowedUsername(username string) (string, error) {
	timestamp := 0
	parts := strings.Split(username, UsernameSeparator)
	timestampIdx := -1
	for i, ts := range parts {
		t, err := strconv.Atoi(ts)
		if err == nil {
			timestamp = t
			timestampIdx = i
			break
		}
	}

	if timestamp == 0 {
		return "", fmt.Errorf("invalid time-windowed username %q", username)
	}

	if int64(timestamp) < time.Now().Unix() {
		return "", fmt.Errorf("expired time-windowed username %q", username)
	}

	// Default format is timestamp:userID
	userID := username
	if len(parts) == 2 {
		if timestampIdx == 0 {
			userID = parts[1]
		}
	}

	return userID, nil
}

// GetLongTermCredential creates a password given a username and a shared secret.
func GetLongTermCredential(username string, sharedSecret string) (string, error) {
	mac := hmac.New(sha1.New, []byte(sharedSecret))
	_, err := mac.Write([]byte(username))
	if err != nil {
		return "", err // Not sure if this will ever happen
	}
	password := mac.Sum(nil)
	return base64.StdEncoding.EncodeToString(password), nil
}

// GenerateAuthKey is a convenience function to easily generate keys in the format used by
// AuthHandler. Re-exported from `pion/turn` so that our callers will have a single import.
func GenerateAuthKey(username, realm, password string) []byte {
	return turn.GenerateAuthKey(username, realm, password)
}

// GenerateCredentials turns an auth config into the credentials a TURN client authenticates with,
// generating a fresh pair on every call: for "static" the configured username/password, for
// "ephemeral" a time-windowed credential from the shared secret, valid for the configured
// lifetime. A nil config and an explicit "none" both yield empty credentials, an anonymous
// session. This is the client-side counterpart of the AuthHandler: everywhere STUNner dials an
// upstream TURN server (a TURN-* relay cluster), the credentials come from here.
func GenerateCredentials(auth *stnrv2.AuthConfig) (string, string, error) {
	if auth == nil {
		return "", "", nil
	}

	atype, err := stnrv2.NewAuthType(auth.Type)
	if err != nil {
		return "", "", err
	}

	switch atype {
	case stnrv2.AuthTypeNone:
		return "", "", nil

	case stnrv2.AuthTypeStatic:
		return auth.Credentials["username"], auth.Credentials["password"], nil

	case stnrv2.AuthTypeEphemeral:
		secret, found := auth.Credentials["secret"]
		if !found {
			return "", "", fmt.Errorf("no secret found in %s auth config", atype.String())
		}
		return turn.GenerateLongTermCredentials(secret, auth.CredentialLifetime())

	default:
		return "", "", fmt.Errorf("unknown authentication type %q", auth.Type)
	}
}
