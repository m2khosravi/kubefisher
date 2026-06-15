package platform

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRayServeDetect(t *testing.T) {
	adapter := RayServe{}

	require.False(t, adapter.Detect(nil))
	require.False(t, adapter.Detect(&corev1.Pod{}))

	require.True(t, adapter.Detect(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{labelRayServeDeployment: "my-app"},
		},
	}))
	require.False(t, adapter.Detect(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"ray.io/cluster-name": "cluster-only"},
		},
	}))
}

func TestRayClusterNameFromPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{labelRayClusterName: "my-svc-raycluster-abc"},
		},
	}
	require.Equal(t, "my-svc-raycluster-abc", rayClusterNameFromPod(pod))
	require.Empty(t, rayClusterNameFromPod(&corev1.Pod{}))
}
