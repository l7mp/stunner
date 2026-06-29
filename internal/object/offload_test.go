package object_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

func TestOffloadObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "offload",
		setup: func(t *testing.T) (runtime.Object, stnrv1.Config, *stnrv1.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewOffload(nil, env.rt)
			require.NoError(t, err)
			return obj, &object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String()}, &stnrv1.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{name: "engine-change-restart", conf: &object.OffloadConfig{Engine: stnrv1.OffloadEngineAuto.String()}, want: runtime.ActionRestart},
			{name: "interfaces-change-restart", conf: &object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String(), Interfaces: []string{"eth0"}}, want: runtime.ActionRestart},
			{name: "same-config-none", conf: &object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String()}, want: runtime.ActionNone},
		},
	})
}

func TestOffloadWithInterfacesObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "offload-with-interfaces",
		setup: func(t *testing.T) (runtime.Object, stnrv1.Config, *stnrv1.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewOffload(nil, env.rt)
			require.NoError(t, err)
			return obj, &object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String(), Interfaces: []string{"eth0"}}, &stnrv1.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{name: "same-config-none", conf: &object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String(), Interfaces: []string{"eth0"}}, want: runtime.ActionNone},
			{name: "engine-change-restart", conf: &object.OffloadConfig{Engine: stnrv1.OffloadEngineAuto.String(), Interfaces: []string{"eth0"}}, want: runtime.ActionRestart},
			{name: "interfaces-change-restart", conf: &object.OffloadConfig{Engine: stnrv1.OffloadEngineNone.String(), Interfaces: []string{"eth1"}}, want: runtime.ActionRestart},
		},
	})
}
