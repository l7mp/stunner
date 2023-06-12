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

	"github.com/pion/turn/v2"
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
func CheckTimeWindowedUsername(username string) error {
	var timestamp int = 0
	for _, ts := range strings.Split(username, UsernameSeparator) {
		t, err := strconv.Atoi(ts)
		if err == nil {
			timestamp = t
			break
		}
	}

	if timestamp == 0 {
		return fmt.Errorf("invalid time-windowed username %q", username)
	}

	if int64(timestamp) < time.Now().Unix() {
		return fmt.Errorf("expired time-windowed username %q", username)
	}

	return nil
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
// AuthHandler. Re-exported from `pion/turn/v2` so that our callers will have a single import.
func GenerateAuthKey(username, realm, password string) []byte {
	return turn.GenerateAuthKey(username, realm, password)
}
