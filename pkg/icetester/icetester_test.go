package icetester

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/stretchr/testify/assert"
)

// TestViaService pins the ICE servers of the backend in a symmetric test: every TURN URI points
// at the Gateway's Service, whatever the public address, an IPv6 one included.
func TestViaService(t *testing.T) {
	got := viaService([]webrtc.ICEServer{{
		URLs:     []string{"turn:1.2.3.4:3478?transport=udp", "turn:[2001:db8::1]:3478?transport=tcp", "not a uri"},
		Username: "u", Credential: "p",
	}}, "gw.ns.svc.cluster.local")
	assert.Equal(t, []webrtc.ICEServer{{
		URLs: []string{"turn:gw.ns.svc.cluster.local:3478?transport=udp",
			"turn:gw.ns.svc.cluster.local:3478?transport=tcp", "not a uri"},
		Username: "u", Credential: "p",
	}}, got)
}

func TestEventually(t *testing.T) {
	n := 0
	assert.NoError(t, eventually(context.Background(), time.Second, func(context.Context) (bool, error) {
		n++
		return n == 3, nil
	}), "holds on the third check")

	boom := errors.New("boom")
	assert.ErrorIs(t, eventually(context.Background(), time.Second, func(context.Context) (bool, error) {
		return false, boom
	}), boom, "an error ends the wait")

	assert.ErrorIs(t, eventually(context.Background(), 300*time.Millisecond, func(context.Context) (bool, error) {
		return false, nil
	}), context.DeadlineExceeded, "the timeout ends the wait")
}
