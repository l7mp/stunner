package object_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func TestStunnerObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "stunner",
		setup: func(t *testing.T) (runtime.Object, stnrv2.Config, *stnrv2.StunnerConfig) {
			env := newTestEnv()

			health, err := object.NewHealth(&object.HealthConfig{Endpoint: ""}, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, health)

			metrics, err := object.NewMetrics(&object.MetricsConfig{Endpoint: ""}, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, metrics)

			offload, err := object.NewOffload(&object.OffloadConfig{Engine: stnrv2.OffloadEngineNone.String(), Interfaces: []string{}}, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, offload)

			auth, err := object.NewAuth(staticAuthConfig(), env.rt)
			require.NoError(t, err)
			mustAdd(t, env, auth)

			adminConf := &stnrv2.AdminConfig{
				Name:                stnrv2.DefaultStunnerName,
				LogLevel:            stnrv2.DefaultLogLevel,
				MetricsEndpoint:     "",
				HealthCheckEndpoint: strPtr(""),
				UserQuota:           0,
				OffloadEngine:       stnrv2.OffloadEngineNone.String(),
				OffloadInterfaces:   []string{},
			}
			admin, err := object.NewAdmin(adminConf, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, admin)

			cluster, err := object.NewCluster(&stnrv2.ClusterConfig{
				Name:      "cluster-a",
				Type:      stnrv2.ClusterTypeStatic.String(),
				Endpoints: []string{"1.2.3.4"},
				Protocol:  stnrv2.ProtocolUDP.String(),
			}, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, cluster)

			server, err := object.NewServer(&stnrv2.ServerConfig{
				Name:     "server-a",
				Type:     stnrv2.ServerTypeTURN.String(),
				Clusters: []string{"cluster-a"},
			}, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, server)

			listener, err := object.NewListener(&stnrv2.ListenerConfig{
				Name:     "listener-a",
				Protocol: stnrv2.ProtocolUDP.String(),
				Servers:  []string{"server-a"},
				Addr:     "127.0.0.1",
				Port:     3478,
			}, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, listener)

			obj, err := object.NewStunner(nil, env.rt)
			require.NoError(t, err)

			base := &stnrv2.StunnerConfig{
				ApiVersion: stnrv2.ApiVersion,
				Admin:      *admin.GetConfig().(*stnrv2.AdminConfig),
				Auth:       *auth.GetConfig().(*stnrv2.AuthConfig),
				Listeners:  []stnrv2.ListenerConfig{*listener.GetConfig().(*stnrv2.ListenerConfig)},
				Servers:    []stnrv2.ServerConfig{*server.GetConfig().(*stnrv2.ServerConfig)},
				Clusters:   []stnrv2.ClusterConfig{*cluster.GetConfig().(*stnrv2.ClusterConfig)},
			}
			return obj, base, base
		},
		expectations: []inspectExpectation{{name: "always-none", conf: &stnrv2.StunnerConfig{ApiVersion: stnrv2.ApiVersion}, want: runtime.ActionNone}},
	})
}

// TestStunnerStatus pins the shape of the aggregated status: one entry per server, cluster and
// listener, and empty lists rather than nil for a kind with no objects.
func TestStunnerStatus(t *testing.T) {
	env := newTestEnv()
	obj, err := object.NewStunner(nil, env.rt)
	require.NoError(t, err)

	status := obj.Status().(*stnrv2.StunnerStatus)
	assert.NotNil(t, status.Listeners)
	assert.NotNil(t, status.Servers)
	assert.NotNil(t, status.Clusters)

	cluster, err := object.NewCluster(&stnrv2.ClusterConfig{Name: "cluster-a",
		Endpoints: []string{"1.2.3.4"}, Protocol: "UDP"}, env.rt)
	require.NoError(t, err)
	mustAdd(t, env, cluster)
	server, err := object.NewServer(&stnrv2.ServerConfig{Name: "server-a",
		Clusters: []string{"cluster-a"}}, env.rt)
	require.NoError(t, err)
	mustAdd(t, env, server)
	listener, err := object.NewListener(&stnrv2.ListenerConfig{Name: "listener-a",
		Servers: []string{"server-a"}}, env.rt)
	require.NoError(t, err)
	mustAdd(t, env, listener)

	status = obj.Status().(*stnrv2.StunnerStatus)
	require.Len(t, status.Servers, 1)
	assert.Equal(t, "server-a", status.Servers[0].Name)
	assert.Equal(t, []string{"cluster-a"}, status.Servers[0].Clusters)
	assert.Zero(t, status.Servers[0].Sessions)
	require.Len(t, status.Clusters, 1)
	assert.Equal(t, "cluster-a", status.Clusters[0].Name)
	assert.Equal(t, "UDP", status.Clusters[0].Protocol)
	require.Len(t, status.Listeners, 1)
	assert.Equal(t, []string{"server-a"}, status.Listeners[0].Servers)
}
