package object_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

func TestAdminObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "admin",
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

			obj, err := object.NewAdmin(nil, env.rt)
			require.NoError(t, err)

			base := &stnrv1.AdminConfig{
				Name:                stnrv1.DefaultStunnerName,
				LogLevel:            stnrv1.DefaultLogLevel,
				MetricsEndpoint:     "",
				HealthCheckEndpoint: strPtr(""),
				UserQuota:           10,
				OffloadEngine:       stnrv1.OffloadEngineNone.String(),
				OffloadInterfaces:   []string{},
			}

			return obj, base, &stnrv1.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{
				name: "loglevel-change-reconcile",
				conf: &stnrv1.AdminConfig{
					Name:                stnrv1.DefaultStunnerName,
					LogLevel:            "all:DEBUG",
					MetricsEndpoint:     "",
					HealthCheckEndpoint: strPtr(""),
					UserQuota:           10,
					OffloadEngine:       stnrv1.OffloadEngineNone.String(),
					OffloadInterfaces:   []string{},
				},
				want: runtime.ActionReconcile,
			},
			{
				name: "same-config-none",
				conf: &stnrv1.AdminConfig{
					Name:                stnrv1.DefaultStunnerName,
					LogLevel:            stnrv1.DefaultLogLevel,
					MetricsEndpoint:     "",
					HealthCheckEndpoint: strPtr(""),
					UserQuota:           10,
					OffloadEngine:       stnrv1.OffloadEngineNone.String(),
					OffloadInterfaces:   []string{},
				},
				want: runtime.ActionNone,
			},
		},
	})
}
