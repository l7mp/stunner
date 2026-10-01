package object_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func TestHealthObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "health",
		setup: func(t *testing.T) (runtime.Object, stnrv2.Config, *stnrv2.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewHealth(nil, env.rt)
			require.NoError(t, err)
			return obj, &object.HealthConfig{Endpoint: ""}, &stnrv2.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{name: "endpoint-change-restart", conf: &object.HealthConfig{Endpoint: "http://:8086"}, want: runtime.ActionRestart},
			{name: "same-config-none", conf: &object.HealthConfig{Endpoint: ""}, want: runtime.ActionNone},
		},
	})
}
