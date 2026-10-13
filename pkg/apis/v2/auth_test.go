package v2

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateCredentials(t *testing.T) {
	t.Run("nil policy means anonymous", func(t *testing.T) {
		var auth *AuthConfig
		u, p, err := auth.GenerateCredentials("", time.Time{})
		assert.NoError(t, err)
		assert.Empty(t, u)
		assert.Empty(t, p)
	})

	t.Run("explicit none is identical to nil", func(t *testing.T) {
		u, p, err := (&AuthConfig{Type: "none"}).GenerateCredentials("", time.Time{})
		assert.NoError(t, err)
		assert.Empty(t, u)
		assert.Empty(t, p)
	})

	t.Run("static returns the configured pair", func(t *testing.T) {
		u, p, err := (&AuthConfig{Type: "static", Credentials: map[string]string{
			"username": "user", "password": "pass"}}).GenerateCredentials("", time.Time{})
		assert.NoError(t, err)
		assert.Equal(t, "user", u)
		assert.Equal(t, "pass", p)
	})

	t.Run("ephemeral expires after the configured lifetime", func(t *testing.T) {
		auth := &AuthConfig{Type: "ephemeral", Lifetime: "30m",
			Credentials: map[string]string{"secret": "my-secret"}}
		u, p, err := auth.GenerateCredentials("", time.Time{})
		require.NoError(t, err)

		// the username is the expiry timestamp: in the future, within the lifetime
		expiry, err := strconv.ParseInt(u, 10, 64)
		require.NoError(t, err, "username is a unix timestamp")
		now := time.Now().Unix()
		assert.Greater(t, expiry, now, "credential expires in the future")
		assert.LessOrEqual(t, expiry, now+int64((30*time.Minute).Seconds())+5,
			"expiry honors the configured lifetime")

		// the password is what the server-side checker derives from the same secret
		want, err := GetLongTermCredential(u, "my-secret")
		require.NoError(t, err)
		assert.Equal(t, want, p)
	})

	t.Run("ephemeral lifetime defaults", func(t *testing.T) {
		auth := &AuthConfig{Type: "ephemeral", Credentials: map[string]string{"secret": "s"}}
		u, _, err := auth.GenerateCredentials("", time.Time{})
		require.NoError(t, err)
		expiry, err := strconv.ParseInt(u, 10, 64)
		require.NoError(t, err)
		assert.Greater(t, expiry, time.Now().Unix()+int64((59*time.Minute).Seconds()),
			"default lifetime is 1h")
	})

	t.Run("ephemeral honors the user id and the expiry", func(t *testing.T) {
		auth := &AuthConfig{Type: "ephemeral", Lifetime: "30m",
			Credentials: map[string]string{"secret": "my-secret"}}
		expiry := time.Now().Add(10 * time.Minute)
		u, p, err := auth.GenerateCredentials("alice", expiry)
		require.NoError(t, err)
		ts, userid, ok := strings.Cut(u, UsernameSeparator)
		require.True(t, ok, "username is timestamp:userid")
		assert.Equal(t, "alice", userid)
		assert.Equal(t, strconv.FormatInt(expiry.Unix(), 10), ts)
		want, err := GetLongTermCredential(u, "my-secret")
		require.NoError(t, err)
		assert.Equal(t, want, p)

		u2, p2, err := auth.GenerateCredentials("alice", expiry)
		require.NoError(t, err)
		assert.Equal(t, [2]string{u, p}, [2]string{u2, p2}, "the same expiry, the same pair")
	})

	t.Run("static without a password fails", func(t *testing.T) {
		_, _, err := (&AuthConfig{Type: "static", Credentials: map[string]string{
			"username": "user"}}).GenerateCredentials("", time.Time{})
		assert.Error(t, err)
	})

	t.Run("ephemeral without a secret fails", func(t *testing.T) {
		_, _, err := (&AuthConfig{Type: "ephemeral"}).GenerateCredentials("", time.Time{})
		assert.Error(t, err)
	})

	t.Run("unknown auth type fails", func(t *testing.T) {
		_, _, err := (&AuthConfig{Type: "bogus"}).GenerateCredentials("", time.Time{})
		assert.Error(t, err)
	})
}
