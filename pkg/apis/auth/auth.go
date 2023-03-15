// Package auth contains variuos routines to generate and check STUNner authentication credentials.
package auth

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

// GenerateTimeWindowedUsername creates a time-windowed username used for autentication as per the
// "REST API For Access To TURN Services"
// (https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00) spec.
func GenerateTimeWindowedUsername(startTime time.Time, timeout time.Duration, username string) string {
	endTime := startTime.Add(timeout).Unix()
	timeUsername := strconv.FormatInt(endTime, 10)
	return timeUsername + ":" + username
}

// CheckTimeWindowedUsername checks the validity of an authentication credential. The method mostly
// follows the "REST API For Access To TURN Services"
// (https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00) spec with the exception
// that we make more effort to find out which component of the username is the UNIX timestamp: we
// find the first thing that looks like a UNIX timestamp in the username and use that for checking
// the time-windowed credential, and drop everything else.
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
// AuthHandler; re-exporteed from pion/turn/v2 so that our callers will have a single import.
func GenerateAuthKey(username, realm, password string) []byte {
	return turn.GenerateAuthKey(username, realm, password)
}
