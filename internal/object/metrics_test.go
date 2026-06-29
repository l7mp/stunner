package object_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

func TestMetricsObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "metrics",
		setup: func(t *testing.T) (runtime.Object, stnrv1.Config, *stnrv1.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewMetrics(nil, env.rt)
			require.NoError(t, err)
			return obj, &object.MetricsConfig{Endpoint: ""}, &stnrv1.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{name: "endpoint-change-restart", conf: &object.MetricsConfig{Endpoint: "http://:8080/metrics"}, want: runtime.ActionRestart},
			{name: "same-config-none", conf: &object.MetricsConfig{Endpoint: ""}, want: runtime.ActionNone},
		},
	})
}
