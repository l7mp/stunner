package object_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

func TestStunnerObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "stunner",
		setup: func(t *testing.T) (runtime.Object, stnrv1.Config, *stnrv1.StunnerConfig) {
			env := newTestEnv()

			health, err := object.NewHealth(&object.HealthConfig{Endpoint: ""}, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, health)

			metrics, err := object.NewMetrics(&object.MetricsConfig{Endpoint: ""}, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, metrics)

			offload, err := object.NewOffload(&object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String(), Interfaces: []string{}}, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, offload)

			authConf := staticAuthConfig()
			auth, err := object.NewAuth(authConf, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, auth)

			adminConf := &stnrv1.AdminConfig{
				Name:                stnrv1.DefaultStunnerName,
				LogLevel:            stnrv1.DefaultLogLevel,
				MetricsEndpoint:     "",
				HealthCheckEndpoint: strPtr(""),
				UserQuota:           0,
				OffloadEngine:       stnrv1.OffloadEngineNone.String(),
				OffloadInterfaces:   []string{},
			}
			admin, err := object.NewAdmin(adminConf, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, admin)

			clusterConf := &stnrv1.ClusterConfig{
				Name:      "cluster-a",
				Type:      stnrv1.ClusterTypeStatic.String(),
				Protocol:  stnrv1.ClusterProtocolUDP.String(),
				Endpoints: []string{"1.2.3.4"},
			}
			cluster, err := object.NewCluster(clusterConf, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, cluster)

			listenerConf := &stnrv1.ListenerConfig{
				Name:     "listener-a",
				Protocol: stnrv1.ListenerProtocolTURNUDP.String(),
				Addr:     "127.0.0.1",
				Port:     3478,
				Routes:   []string{"cluster-a"},
			}
			listener, err := object.NewListener(listenerConf, env.rt)
			require.NoError(t, err)
			mustAdd(t, env, listener)

			obj, err := object.NewStunner(nil, env.rt)
			require.NoError(t, err)

			base := &stnrv1.StunnerConfig{
				ApiVersion: stnrv1.ApiVersion,
				Admin:      *admin.GetConfig().(*stnrv1.AdminConfig),
				Auth:       *auth.GetConfig().(*stnrv1.AuthConfig),
				Listeners:  []stnrv1.ListenerConfig{*listener.GetConfig().(*stnrv1.ListenerConfig)},
				Clusters:   []stnrv1.ClusterConfig{*cluster.GetConfig().(*stnrv1.ClusterConfig)},
			}
			return obj, base, base
		},
		expectations: []inspectExpectation{{name: "always-none", conf: &stnrv1.StunnerConfig{ApiVersion: stnrv1.ApiVersion}, want: runtime.ActionNone}},
	})
}
