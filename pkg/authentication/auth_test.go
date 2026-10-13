package authentication

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// TestCheckTimeWindowedUsername pins the server-side check of the time-windowed usernames the
// ephemeral auth policies generate.
func TestCheckTimeWindowedUsername(t *testing.T) {
	userid, err := CheckTimeWindowedUsername(
		stnrv2.GenerateTimeWindowedUsername(time.Now().Add(time.Hour), "alice"))
	require.NoError(t, err)
	assert.Equal(t, "alice", userid)

	expiry := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	for _, username := range []string{expiry, expiry + stnrv2.UsernameSeparator} {
		userid, err = CheckTimeWindowedUsername(username)
		require.NoError(t, err, "a username without a user id: %q", username)
		assert.Equal(t, stnrv2.DefaultUserID, userid, "the default user: %q", username)
	}

	_, err = CheckTimeWindowedUsername(
		stnrv2.GenerateTimeWindowedUsername(time.Now().Add(-time.Minute), "alice"))
	assert.Error(t, err, "expired")

	_, err = CheckTimeWindowedUsername("alice")
	assert.Error(t, err, "no timestamp")
}
