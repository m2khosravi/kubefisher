package platform

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestKubeflowTrainerDetect(t *testing.T) {
	adapter := KubeflowTrainer{}

	require.False(t, adapter.Detect(nil))
	require.False(t, adapter.Detect(&corev1.Pod{}))

	require.True(t, adapter.Detect(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{labelTrainJobStep: "trainer"},
		},
	}))
	require.True(t, adapter.Detect(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{labelTrainJobName: "my-job"},
		},
	}))
	require.False(t, adapter.Detect(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"app": "other"},
		},
	}))
}

func TestTrainJobNameFromPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				labelTrainJobName: "explicit-name",
				labelJobSetName:   "jobset-name",
			},
		},
	}
	require.Equal(t, "explicit-name", trainJobNameFromPod(pod))

	pod.Labels = map[string]string{labelJobSetName: "jobset-only"}
	require.Equal(t, "jobset-only", trainJobNameFromPod(pod))
}
