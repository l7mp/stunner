package authentication

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func TestGenerateCredentials(t *testing.T) {
	t.Run("nil config means anonymous", func(t *testing.T) {
		u, p, err := GenerateCredentials(nil)
		assert.NoError(t, err)
		assert.Empty(t, u)
		assert.Empty(t, p)
	})

	t.Run("explicit none is identical to nil", func(t *testing.T) {
		u, p, err := GenerateCredentials(&stnrv2.AuthConfig{Type: "none"})
		assert.NoError(t, err)
		assert.Empty(t, u)
		assert.Empty(t, p)
	})

	t.Run("static returns the configured pair", func(t *testing.T) {
		u, p, err := GenerateCredentials(&stnrv2.AuthConfig{Type: "static",
			Credentials: map[string]string{"username": "user", "password": "pass"}})
		assert.NoError(t, err)
		assert.Equal(t, "user", u)
		assert.Equal(t, "pass", p)
	})

	t.Run("ephemeral generates a time-windowed credential", func(t *testing.T) {
		auth := &stnrv2.AuthConfig{Type: "ephemeral", Lifetime: "30m",
			Credentials: map[string]string{"secret": "my-secret"}}
		u, p, err := GenerateCredentials(auth)
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
		auth := &stnrv2.AuthConfig{Type: "ephemeral",
			Credentials: map[string]string{"secret": "my-secret"}}
		u, _, err := GenerateCredentials(auth)
		require.NoError(t, err)
		expiry, err := strconv.ParseInt(u, 10, 64)
		require.NoError(t, err)
		now := time.Now().Unix()
		assert.Greater(t, expiry, now+int64((59*time.Minute).Seconds()),
			"default lifetime is 1h")
	})

	t.Run("ephemeral without a secret fails", func(t *testing.T) {
		_, _, err := GenerateCredentials(&stnrv2.AuthConfig{Type: "ephemeral"})
		assert.Error(t, err)
	})

	t.Run("unknown auth type fails", func(t *testing.T) {
		_, _, err := GenerateCredentials(&stnrv2.AuthConfig{Type: "bogus"})
		assert.Error(t, err)
	})
}
