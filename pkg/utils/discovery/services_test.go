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

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func TestGetStunnerdStatus(t *testing.T) {
	want := stnrv2.StunnerStatus{
		Admin:  &stnrv2.AdminStatus{Name: "ns/gw", OffloadStatus: "TC[all]"},
		Status: "Ready",
		Listeners: []*stnrv2.ListenerStatus{{
			ListenerConfig: &stnrv2.ListenerConfig{Name: "ns/gw/udp"},
			Stats:          stnrv2.Stats{Total: stnrv2.DirStat{Rx: stnrv2.StatInfo{Pkts: 3}}},
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
	assert.Equal(t, uint64(3), got.Listeners[0].Stats.Total.Rx.Pkts)

	// an instance that is not there
	_, err = GetStunnerdStatus(context.Background(), "127.0.0.1:1")
	assert.Error(t, err)
}

func TestGetAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("service") != "turn" {
			http.Error(w, "no service", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(r.URL.Path + "?" + r.URL.RawQuery))
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	b, err := GetAuth(context.Background(), addr, AuthQuery{Namespace: "ns", Gateway: "gw"})
	require.NoError(t, err)
	assert.Equal(t, "/ice?gateway=gw&namespace=ns&service=turn", string(b), "an ICE server config, empty fields left out")
	b, err = GetAuth(context.Background(), addr, AuthQuery{Username: "u", TURNCredential: true})
	require.NoError(t, err)
	assert.Equal(t, "/?service=turn&username=u", string(b), "a TURN credential, at the root")

	_, err = GetAuth(context.Background(), "127.0.0.1:1", AuthQuery{})
	assert.Error(t, err, "no auth service")
}
