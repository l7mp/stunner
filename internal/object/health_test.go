package object_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

func TestHealthObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "health",
		setup: func(t *testing.T) (runtime.Object, stnrv1.Config, *stnrv1.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewHealth(nil, env.rt)
			require.NoError(t, err)
			return obj, &object.HealthConfig{Endpoint: ""}, &stnrv1.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{name: "endpoint-change-restart", conf: &object.HealthConfig{Endpoint: "http://:8086"}, want: runtime.ActionRestart},
			{name: "same-config-none", conf: &object.HealthConfig{Endpoint: ""}, want: runtime.ActionNone},
		},
	})
}
