package stunner

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v5/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/utils/turnclient"
)

// TestStunnerStatus pins that the status of the daemon, from GetStatus and from the /status
// endpoint of the health check alike, counts the live sessions and reports the readiness.
func TestStunnerStatus(t *testing.T) {
	lim := test.TimeOut(30 * time.Second)
	defer lim.Stop()

	s := NewStunner(Options{LogOptions: LogOptions{Level: stunnerTestLoglevel}})
	defer s.Close()

	conf := stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin:      stnrv2.AdminConfig{LogLevel: stunnerTestLoglevel},
		Auths: []stnrv2.AuthConfig{{
			Type:        "static",
			Credentials: map[string]string{"username": "user1", "password": "passwd1"},
		}},
		Listeners: []stnrv2.ListenerConfig{{
			Name: "udp", Protocol: "UDP", Servers: []string{"udp"}, Addr: "127.0.0.1", Port: 23478,
		}},
		Servers: []stnrv2.ServerConfig{{
			ServerSpec: stnrv2.ServerSpec{Name: "udp", Type: "turn", Clusters: []string{"allow-any"}},
		}},
		Clusters: []stnrv2.ClusterConfig{{Name: "allow-any", Protocol: "UDP", Addrs: []string{"127.0.0.1"}}},
	}
	require.NoError(t, s.Reconcile(allowPeers(&conf)), "reconcile")

	// the status from the API and from the health check
	statuses := func() []*stnrv2.StunnerStatus {
		res, err := http.Get("http://127.0.0.1:8086/status") //nolint:noctx
		require.NoError(t, err, "GET /status")
		defer res.Body.Close() //nolint:errcheck
		served := &stnrv2.StunnerStatus{}
		require.NoError(t, json.NewDecoder(res.Body).Decode(served), "decode /status")
		return []*stnrv2.StunnerStatus{s.GetStatus().(*stnrv2.StunnerStatus), served}
	}

	for _, st := range statuses() {
		assert.Equal(t, 0, st.AllocationCount, "no session yet")
		assert.Equal(t, "READY", st.Status, "ready")
	}

	d := turnclient.Dialer{Config: turnclient.Config{
		Protocol:      stnrv2.ProtocolTURNUDP,
		ServerAddr:    "127.0.0.1:23478",
		Username:      "user1",
		Password:      "passwd1",
		Realm:         stnrv2.DefaultRealm,
		LoggerFactory: logging.NewDefaultLoggerFactory(),
	}}
	relay, err := d.ListenPacket(context.Background(), "udp", "")
	require.NoError(t, err, "allocate")
	for _, st := range statuses() {
		assert.Equal(t, 1, st.AllocationCount, "one live allocation")
	}

	require.NoError(t, relay.Close(), "deallocate")
	assert.Eventually(t, func() bool {
		for _, st := range statuses() {
			if st.AllocationCount != 0 {
				return false
			}
		}
		return true
	}, 5*time.Second, 50*time.Millisecond, "the allocation is gone")

	s.Shutdown()
	assert.Equal(t, "TERMINATING", s.GetStatus().(*stnrv2.StunnerStatus).Status, "terminating")
}
