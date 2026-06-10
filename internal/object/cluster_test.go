package object_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

func TestClusterStrictDNSObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "cluster-strict-dns",
		setup: func(t *testing.T) (runtime.Object, stnrv1.Config, *stnrv1.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewCluster(nil, env.rt)
			require.NoError(t, err)

			base := &stnrv1.ClusterConfig{
				Name:      "cluster-a",
				Type:      stnrv1.ClusterTypeStrictDNS.String(),
				Protocol:  stnrv1.ClusterProtocolUDP.String(),
				Endpoints: []string{"Echo-Server.L7MP.io"},
			}
			return obj, base, &stnrv1.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{
				name: "strictdns-case-change-reconcile",
				conf: &stnrv1.ClusterConfig{
					Name:      "cluster-a",
					Type:      stnrv1.ClusterTypeStrictDNS.String(),
					Protocol:  stnrv1.ClusterProtocolUDP.String(),
					Endpoints: []string{"echo-server.l7mp.io"},
				},
				want: runtime.ActionReconcile,
			},
			{
				name: "strictdns-endpoint-change-restart",
				conf: &stnrv1.ClusterConfig{
					Name:      "cluster-a",
					Type:      stnrv1.ClusterTypeStrictDNS.String(),
					Protocol:  stnrv1.ClusterProtocolUDP.String(),
					Endpoints: []string{"echo-server.l7mp.io", "dummy.l7mp.io"},
				},
				want: runtime.ActionRestart,
			},
			{
				name: "strictdns-to-static-restart",
				conf: &stnrv1.ClusterConfig{
					Name:      "cluster-a",
					Type:      stnrv1.ClusterTypeStatic.String(),
					Protocol:  stnrv1.ClusterProtocolUDP.String(),
					Endpoints: []string{"0.0.0.0/0"},
				},
				want: runtime.ActionRestart,
			},
		},
	})
}

func TestClusterStaticObjectSemantics(t *testing.T) {
	runObjectSemanticsCase(t, objectSemanticsCase{
		name: "cluster-static",
		setup: func(t *testing.T) (runtime.Object, stnrv1.Config, *stnrv1.StunnerConfig) {
			env := newTestEnv()
			obj, err := object.NewCluster(nil, env.rt)
			require.NoError(t, err)

			base := &stnrv1.ClusterConfig{
				Name:      "cluster-static",
				Type:      stnrv1.ClusterTypeStatic.String(),
				Protocol:  stnrv1.ClusterProtocolUDP.String(),
				Endpoints: []string{"1.2.3.4"},
			}
			return obj, base, &stnrv1.StunnerConfig{}
		},
		expectations: []inspectExpectation{
			{
				name: "static-endpoint-update-reconcile",
				conf: &stnrv1.ClusterConfig{
					Name:      "cluster-static",
					Type:      stnrv1.ClusterTypeStatic.String(),
					Protocol:  stnrv1.ClusterProtocolUDP.String(),
					Endpoints: []string{"5.6.7.8"},
				},
				want: runtime.ActionReconcile,
			},
		},
	})
}
