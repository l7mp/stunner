package object_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/logger"
)

type inspectExpectation struct {
	name string
	conf stnrv1.Config
	full *stnrv1.StunnerConfig
	want runtime.Action
}

type objectSemanticsCase struct {
	name         string
	setup        func(*testing.T) (runtime.Object, stnrv1.Config, *stnrv1.StunnerConfig)
	expectations []inspectExpectation
}

func runObjectSemanticsCase(t *testing.T, tc objectSemanticsCase) {
	t.Helper()

	obj, base, full := tc.setup(t)

	require.NoError(t, obj.Reconcile(base))
	got := obj.GetConfig()
	require.Truef(t, got.DeepEqual(base), "roundtrip mismatch: got=%s want=%s", got.String(), base.String())

	old := obj.GetConfig()
	for _, exp := range tc.expectations {
		t.Run(exp.name, func(t *testing.T) {
			fullConf := exp.full
			if fullConf == nil {
				fullConf = full
			}
			action, err := obj.Inspect(old, exp.conf, fullConf)
			require.NoError(t, err)
			require.Equal(t, exp.want, action)
		})
	}
}

type testEnv struct {
	rt *runtime.Runtime
}

func newTestEnv() *testEnv {
	log := logger.NewLoggerFactory(stnrv1.DefaultLogLevel)
	r := resolver.NewMockResolver(map[string][]string{}, log)
	rt := runtime.New(runtime.Config{Logger: log, DryRun: true, Resolver: r})
	return &testEnv{rt: rt}
}

func mustAdd(t *testing.T, env *testEnv, o runtime.Runnable) {
	t.Helper()
	require.NoError(t, env.rt.Registry.Add(o, nil))
}

func staticAuthConfig() *stnrv1.AuthConfig {
	return &stnrv1.AuthConfig{
		Type:  stnrv1.AuthTypeStatic.String(),
		Realm: stnrv1.DefaultRealm,
		Credentials: map[string]string{
			"username": "user",
			"password": "pass",
		},
	}
}

func strPtr(s string) *string { return &s }
