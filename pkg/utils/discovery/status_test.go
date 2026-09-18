package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

func TestGetStunnerdStatus(t *testing.T) {
	want := stnrv1.StunnerStatus{
		Admin:  &stnrv1.AdminStatus{Name: "ns/gw", OffloadStatus: "TC[all]"},
		Status: "Ready",
		Listeners: []*stnrv1.ListenerStatus{{
			ListenerConfig: &stnrv1.ListenerConfig{Name: "ns/gw/udp"},
			Stats:          stnrv1.OffloadDirStat{Rx: stnrv1.OffloadStatInfo{Pkts: 3}},
		}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	got, err := GetStunnerdStatus(context.Background(), addr)
	require.NoError(t, err)
	assert.Equal(t, "ns/gw", got.Admin.Name)
	assert.Equal(t, "TC[all]", got.Admin.OffloadStatus)
	assert.Equal(t, uint64(3), got.Listeners[0].Stats.Rx.Pkts)

	// an instance that is not there
	_, err = GetStunnerdStatus(context.Background(), "127.0.0.1:1")
	assert.Error(t, err)
}
