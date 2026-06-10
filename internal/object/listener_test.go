package object_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

func TestListenerObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "listener",
		setup: func(t *testing.T) (runtime.Object, stnrv1.Config, *stnrv1.StunnerConfig) {
			env := newTestEnv()

			auth, err := object.NewAuth(staticAuthConfig(), env.rt)
			require.NoError(t, err)
			mustAdd(t, env, auth)

			obj, err := object.NewListener(nil, env.rt)
			require.NoError(t, err)

			base := &stnrv1.ListenerConfig{
				Name:     "listener-a",
				Protocol: stnrv1.ListenerProtocolTURNUDP.String(),
				Addr:     "127.0.0.1",
				Port:     3478,
				Routes:   []string{"allow-a"},
			}

			full := &stnrv1.StunnerConfig{Auth: *staticAuthConfig()}
			return obj, base, full
		},
		expectations: []inspectExpectation{
			{
				name: "route-change-reconcile",
				conf: &stnrv1.ListenerConfig{
					Name:     "listener-a",
					Protocol: stnrv1.ListenerProtocolTURNUDP.String(),
					Addr:     "127.0.0.1",
					Port:     3478,
					Routes:   []string{"allow-b"},
				},
				want: runtime.ActionReconcile,
			},
			{
				name: "public-addr-change-reconcile",
				conf: &stnrv1.ListenerConfig{
					Name:       "listener-a",
					Protocol:   stnrv1.ListenerProtocolTURNUDP.String(),
					Addr:       "127.0.0.1",
					Port:       3478,
					PublicAddr: "198.51.100.1",
					Routes:     []string{"allow-a"},
				},
				want: runtime.ActionReconcile,
			},
			{
				name: "public-port-change-reconcile",
				conf: &stnrv1.ListenerConfig{
					Name:       "listener-a",
					Protocol:   stnrv1.ListenerProtocolTURNUDP.String(),
					Addr:       "127.0.0.1",
					Port:       3478,
					PublicPort: 12345,
					Routes:     []string{"allow-a"},
				},
				want: runtime.ActionReconcile,
			},
			{
				name: "port-change-restart",
				conf: &stnrv1.ListenerConfig{
					Name:     "listener-a",
					Protocol: stnrv1.ListenerProtocolTURNUDP.String(),
					Addr:     "127.0.0.1",
					Port:     3479,
					Routes:   []string{"allow-a"},
				},
				want: runtime.ActionRestart,
			},
			{
				name: "protocol-change-restart",
				conf: &stnrv1.ListenerConfig{
					Name:     "listener-a",
					Protocol: stnrv1.ListenerProtocolTURNTCP.String(),
					Addr:     "127.0.0.1",
					Port:     3478,
					Routes:   []string{"allow-a"},
				},
				want: runtime.ActionRestart,
			},
			{
				name: "address-change-restart",
				conf: &stnrv1.ListenerConfig{
					Name:     "listener-a",
					Protocol: stnrv1.ListenerProtocolTURNUDP.String(),
					Addr:     "127.0.0.2",
					Port:     3478,
					Routes:   []string{"allow-a"},
				},
				want: runtime.ActionRestart,
			},
			{
				name: "name-change-restart",
				conf: &stnrv1.ListenerConfig{
					Name:     "listener-b",
					Protocol: stnrv1.ListenerProtocolTURNUDP.String(),
					Addr:     "127.0.0.1",
					Port:     3478,
					Routes:   []string{"allow-a"},
				},
				want: runtime.ActionRestart,
			},
			{
				name: "auth-realm-change-restart",
				conf: &stnrv1.ListenerConfig{
					Name:     "listener-a",
					Protocol: stnrv1.ListenerProtocolTURNUDP.String(),
					Addr:     "127.0.0.1",
					Port:     3478,
					Routes:   []string{"allow-a"},
				},
				full: &stnrv1.StunnerConfig{Auth: stnrv1.AuthConfig{
					Type:  stnrv1.AuthTypeStatic.String(),
					Realm: "example.org",
					Credentials: map[string]string{
						"username": "user",
						"password": "pass",
					},
				}},
				want: runtime.ActionRestart,
			},
			{
				name: "same-config-none",
				conf: &stnrv1.ListenerConfig{
					Name:     "listener-a",
					Protocol: stnrv1.ListenerProtocolTURNUDP.String(),
					Addr:     "127.0.0.1",
					Port:     3478,
					Routes:   []string{"allow-a"},
				},
				want: runtime.ActionNone,
			},
		},
	})
}

func TestListenerTLSObjectSemantics(t *testing.T) {
	certA := base64.StdEncoding.EncodeToString([]byte("cert-a"))
	keyA := base64.StdEncoding.EncodeToString([]byte("key-a"))
	certB := base64.StdEncoding.EncodeToString([]byte("cert-b"))
	keyB := base64.StdEncoding.EncodeToString([]byte("key-b"))

	env := newTestEnv()

	auth, err := object.NewAuth(staticAuthConfig(), env.rt)
	require.NoError(t, err)
	mustAdd(t, env, auth)

	obj, err := object.NewListener(nil, env.rt)
	require.NoError(t, err)

	base := &stnrv1.ListenerConfig{
		Name:     "listener-tls",
		Protocol: stnrv1.ListenerProtocolTURNTLS.String(),
		Addr:     "127.0.0.1",
		Port:     5349,
		Cert:     certA,
		Key:      keyA,
		Routes:   []string{"allow-a"},
	}
	require.NoError(t, obj.Reconcile(base))

	old := obj.GetConfig()
	full := &stnrv1.StunnerConfig{Auth: *staticAuthConfig()}

	tests := []inspectExpectation{
		{
			name: "route-change-reconcile",
			conf: &stnrv1.ListenerConfig{
				Name:     "listener-tls",
				Protocol: stnrv1.ListenerProtocolTURNTLS.String(),
				Addr:     "127.0.0.1",
				Port:     5349,
				Cert:     certA,
				Key:      keyA,
				Routes:   []string{"allow-b"},
			},
			want: runtime.ActionReconcile,
		},
		{
			name: "cert-change-restart",
			conf: &stnrv1.ListenerConfig{
				Name:     "listener-tls",
				Protocol: stnrv1.ListenerProtocolTURNTLS.String(),
				Addr:     "127.0.0.1",
				Port:     5349,
				Cert:     certB,
				Key:      keyA,
				Routes:   []string{"allow-a"},
			},
			want: runtime.ActionRestart,
		},
		{
			name: "key-change-restart",
			conf: &stnrv1.ListenerConfig{
				Name:     "listener-tls",
				Protocol: stnrv1.ListenerProtocolTURNTLS.String(),
				Addr:     "127.0.0.1",
				Port:     5349,
				Cert:     certA,
				Key:      keyB,
				Routes:   []string{"allow-a"},
			},
			want: runtime.ActionRestart,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action, err := obj.Inspect(old, tc.conf, full)
			require.NoError(t, err)
			require.Equal(t, tc.want, action)
		})
	}
}
