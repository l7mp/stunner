package object_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

func TestAuthObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "auth",
		setup: func(t *testing.T) (runtime.Object, stnrv1.Config, *stnrv1.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewAuth(nil, env.rt)
			require.NoError(t, err)
			return obj, staticAuthConfig(), &stnrv1.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{
				name: "realm-change-reconcile",
				conf: &stnrv1.AuthConfig{
					Type:  stnrv1.AuthTypeStatic.String(),
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
