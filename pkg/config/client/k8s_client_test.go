package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLeaderPod(t *testing.T) {
	pods := []corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Name: "stunner-gateway-operator-7d9f-abcde", Namespace: "stunner-system"}},
		{ObjectMeta: metav1.ObjectMeta{Name: "stunner-gateway-operator-7d9f-fghij", Namespace: "stunner-system"}},
	}

	// the identity written by controller-runtime: hostname, underscore, uuid
	leader := LeaderPod("stunner-gateway-operator-7d9f-fghij_0d383bfe-10e1-4d76-947f-d66e599cb0d7", pods)
	if assert.NotNil(t, leader) {
		assert.Equal(t, "stunner-gateway-operator-7d9f-fghij", leader.GetName())
	}

	// a bare pod name works too
	assert.Equal(t, &pods[0], LeaderPod("stunner-gateway-operator-7d9f-abcde", pods))

	// a released Lease, a foreign holder, or a pod that is gone yield nothing
	assert.Nil(t, LeaderPod("", pods))
	assert.Nil(t, LeaderPod("someone-else_1234", pods))
	assert.Nil(t, LeaderPod("stunner-gateway-operator-7d9f-zzzzz_1234", pods))
}
