package object_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/object"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

func TestAuthObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "auth",
		setup: func(t *testing.T) (runtime.Object, stnrv2.Config, *stnrv2.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewAuth(nil, env.rt)
			require.NoError(t, err)
			return obj, staticAuthConfig(), &stnrv2.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{
				name: "realm-change-reconcile",
				conf: &stnrv2.AuthConfig{
					Type:  stnrv2.AuthTypeStatic.String(),
					Realm: "example.org",
					Credentials: map[string]string{
						"username": "user",
						"password": "pass",
					},
				},
				want: runtime.ActionReconcile,
			},
			{name: "same-config-none", conf: staticAuthConfig(), want: runtime.ActionNone},
		},
	})
}
