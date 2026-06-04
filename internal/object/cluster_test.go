package object

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

func TestClusterInspectRestartDecision(t *testing.T) {
	tests := []struct {
		name    string
		cluster *Cluster
		req     *stnrv1.ClusterConfig
		want    Action
	}{
		{
			name: "strict dns domain change requires restart",
			cluster: &Cluster{
				Name:    "strict-dns",
				Type:    stnrv1.ClusterTypeStrictDNS,
				Domains: []string{"echo-server.l7mp.io"},
			},
			req: &stnrv1.ClusterConfig{
				Name: "strict-dns",
				Type: stnrv1.ClusterTypeStrictDNS.String(),
				Endpoints: []string{
					"echo-server.l7mp.io",
					"dummy.l7mp.io",
				},
			},
			want: ActionRestart,
		},
		{
			name: "strict dns to static transition requires restart",
			cluster: &Cluster{
				Name:    "strict-dns",
				Type:    stnrv1.ClusterTypeStrictDNS,
				Domains: []string{"echo-server.l7mp.io"},
			},
			req: &stnrv1.ClusterConfig{
				Name:      "strict-dns",
				Type:      stnrv1.ClusterTypeStatic.String(),
				Endpoints: []string{"0.0.0.0/0"},
			},
			want: ActionRestart,
		},
		{
			name: "static endpoint update is reconcile only",
			cluster: &Cluster{
				Name:      "static",
				Type:      stnrv1.ClusterTypeStatic,
				Endpoints: nil,
			},
			req: &stnrv1.ClusterConfig{
				Name:      "static",
				Type:      stnrv1.ClusterTypeStatic.String(),
				Endpoints: []string{"1.2.3.4"},
			},
			want: ActionReconcile,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action, err := tc.cluster.Inspect(tc.cluster.GetConfig(), tc.req, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, action)
		})
	}
}
