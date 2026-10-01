package client

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// v1Conf is a v1 config as the gateway operator renders it: a TURN listener routing to a UDP
// cluster.
const v1Conf = `{"version":"v1","admin":{"name":"ns/gw1"},"auth":{"type":"static","realm":"stunner.l7mp.io","credentials":{"username":"user","password":"pass"}},"listeners":[{"name":"ns/gw1/udp","protocol":"TURN-UDP","address":"1.2.3.4","port":3478,"routes":["ns/media"]}],"clusters":[{"name":"ns/media","type":"STATIC","protocol":"UDP","endpoints":["10.0.0.0/8"]}]}`

// v2Conf is a v2 config with a transport listener feeding a TURN server.
const v2Conf = `version: v2
admin:
  name: ns/gw2
auth:
  type: static
  realm: stunner.l7mp.io
  credentials:
    username: user
    password: pass
listeners:
  - name: ns/gw2/tcp
    protocol: TCP
    servers: [ns/gw2/tcp]
    address: 1.2.3.5
    port: 3478
servers:
  - name: ns/gw2/tcp
    type: turn
    clusters: [ns/media]
clusters:
  - name: ns/media
    type: STATIC
    endpoints: ["10.0.0.0/8"]
    protocol: UDP
`

// TestParseRaw pins the version dispatch of the config parser: a v1 config converts to v2, a v2
// config parses as is, and any other version, the legacy v1alpha1 included, fails.
func TestParseRaw(t *testing.T) {
	t.Run("v1 converts", func(t *testing.T) {
		c, err := parseRaw([]byte(v1Conf))
		require.NoError(t, err)

		v1 := stnrv1.StunnerConfig{}
		require.NoError(t, json.Unmarshal([]byte(v1Conf), &v1))
		want, err := stnrv1.ConvertToV2(&v1)
		require.NoError(t, err)
		assert.True(t, c.DeepEqual(want), "converted")

		assert.Equal(t, stnrv2.ApiVersion, c.ApiVersion, "version")
		require.Len(t, c.Listeners, 1)
		assert.Equal(t, "UDP", c.Listeners[0].Protocol, "transport listener")
		assert.Equal(t, []string{"ns/gw1/udp"}, c.Listeners[0].Servers, "listener feeds its TURN server")
		s, err := c.GetServerConfig("ns/gw1/udp")
		require.NoError(t, err)
		assert.Equal(t, stnrv2.ServerTypeTURN.String(), s.Type, "server type")
		r, err := c.GetClusterConfig("ns/media")
		require.NoError(t, err)
		assert.Equal(t, []string{"10.0.0.0/8"}, r.Endpoints, "cluster endpoints")
	})

	t.Run("v2 parses", func(t *testing.T) {
		c, err := parseRaw([]byte(v2Conf))
		require.NoError(t, err)
		assert.True(t, c.DeepEqual(&stnrv2.StunnerConfig{
			ApiVersion: stnrv2.ApiVersion,
			Admin:      stnrv2.AdminConfig{Name: "ns/gw2"},
			Auth: stnrv2.AuthConfig{Type: "static", Realm: "stunner.l7mp.io",
				Credentials: map[string]string{"username": "user", "password": "pass"}},
			Listeners: []stnrv2.ListenerConfig{{Name: "ns/gw2/tcp", Protocol: "TCP",
				Servers: []string{"ns/gw2/tcp"}, Addr: "1.2.3.5", Port: 3478}},
			Servers: []stnrv2.ServerConfig{{Name: "ns/gw2/tcp", Type: "turn",
				Clusters: []string{"ns/media"}}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "ns/media",
				Type:      "STATIC",
				Endpoints: []string{"10.0.0.0/8"},
				Protocol:  "UDP",
			}},
		}), "parsed: %s", c.String())
	})

	for _, version := range []string{"v3", "v1alpha1", ""} {
		t.Run(fmt.Sprintf("version %q fails", version), func(t *testing.T) {
			_, err := parseRaw([]byte(`{"version":"` + version + `","admin":{"name":"ns/gw"}}`))
			assert.Error(t, err)
		})
	}
}

// TestDecodeConfigList pins that a CDS config list decodes item by item, so one list may mix
// config versions.
func TestDecodeConfigList(t *testing.T) {
	v2, err := parseRaw([]byte(v2Conf))
	require.NoError(t, err)
	v2JSON, err := json.Marshal(v2)
	require.NoError(t, err)

	cs, err := decodeConfigList([]byte(`{"version":"v1","items":[` + v1Conf + `,` + string(v2JSON) + `]}`))
	require.NoError(t, err)
	require.Len(t, cs, 2)
	assert.Equal(t, "ns/gw1", cs[0].Admin.Name, "v1 item")
	assert.Equal(t, stnrv2.ApiVersion, cs[0].ApiVersion, "v1 item converted")
	assert.Equal(t, "ns/gw2", cs[1].Admin.Name, "v2 item")

	_, err = decodeConfigList([]byte(`{"version":"v1","items":[` + v1Conf + `,{"version":"v1alpha1"}]}`))
	assert.Error(t, err, "one bad item fails the list")
}
