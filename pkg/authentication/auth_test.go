package authentication

import (
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

	_, err = CheckTimeWindowedUsername(stnrv2.GenerateTimeWindowedUsername(time.Now().Add(time.Hour), ""))
	assert.NoError(t, err, "an anonymous username")

	_, err = CheckTimeWindowedUsername(
		stnrv2.GenerateTimeWindowedUsername(time.Now().Add(-time.Minute), "alice"))
	assert.Error(t, err, "expired")

	_, err = CheckTimeWindowedUsername("alice")
	assert.Error(t, err, "no timestamp")
}
