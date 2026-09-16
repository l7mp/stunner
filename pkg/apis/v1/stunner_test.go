package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStunnerConfigValidateRoutes covers the cross-object rule that a listener relays either
// directly or through a single upstream TURN server, never both and never through two.
func TestStunnerConfigValidateRoutes(t *testing.T) {
	turnServer := &TURNServer{Address: "turn.example.com", Port: 3478}
	testCases := []struct {
		name     string
		routes   []string
		clusters []ClusterConfig
		err      string
	}{
		{
			name:   "plain clusters only",
			routes: []string{"udp-cluster", "tcp-cluster"},
			clusters: []ClusterConfig{
				{Name: "udp-cluster", Protocol: "udp", Endpoints: []string{"1.2.3.4"}},
				{Name: "tcp-cluster", Protocol: "tcp", Endpoints: []string{"1.2.3.4"}},
			},
		},
		{
			name:     "a single TURN cluster",
			routes:   []string{"turn-cluster"},
			clusters: []ClusterConfig{{Name: "turn-cluster", Protocol: "turn-udp", TURNServer: turnServer}},
		},
		{
			name:   "two TURN clusters",
			routes: []string{"turn-a", "turn-b"},
			clusters: []ClusterConfig{
				{Name: "turn-a", Protocol: "turn-udp", TURNServer: turnServer},
				{Name: "turn-b", Protocol: "turn-tcp", TURNServer: turnServer},
			},
			err: "multiple TURN clusters",
		},
		{
			name:   "plain and TURN mixed",
			routes: []string{"udp-cluster", "turn-cluster"},
			clusters: []ClusterConfig{
				{Name: "udp-cluster", Protocol: "udp", Endpoints: []string{"1.2.3.4"}},
				{Name: "turn-cluster", Protocol: "turn-udp", TURNServer: turnServer},
			},
			err: "both plain",
		},
		{
			name:     "a route naming no cluster is not this rule's business",
			routes:   []string{"turn-cluster", "gone"},
			clusters: []ClusterConfig{{Name: "turn-cluster", Protocol: "turn-udp", TURNServer: turnServer}},
		},
		{
			name:     "a listener routing nowhere",
			clusters: []ClusterConfig{{Name: "turn-cluster", Protocol: "turn-udp", TURNServer: turnServer}},
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			conf := StunnerConfig{
				ApiVersion: ApiVersion,
				Auth:       AuthConfig{Type: "static", Credentials: map[string]string{"username": "u", "password": "p"}},
				Listeners:  []ListenerConfig{{Name: "listener", Protocol: "turn-udp", Routes: c.routes}},
				Clusters:   c.clusters,
			}
			err := conf.Validate()
			if c.err == "" {
				assert.NoError(t, err, "config accepted")
				return
			}
			require.Error(t, err, "config rejected")
			assert.Contains(t, err.Error(), c.err, "rejection names the fault")
			assert.Contains(t, err.Error(), "listener", "rejection names the listener")
		})
	}
}
