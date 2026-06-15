package platform

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestGPUCountFromPod(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("4"),
						},
					},
				},
			},
		},
	}
	require.Equal(t, int64(4), GPUCountFromPod(pod))
}

func TestGPUCountFromUnstructured(t *testing.T) {
	obj := map[string]interface{}{
		"spec": map[string]interface{}{
			"predictor": map[string]interface{}{
				"model": map[string]interface{}{
					"resources": map[string]interface{}{
						"limits": map[string]interface{}{
							"nvidia.com/gpu": "2",
						},
					},
				},
			},
		},
	}
	require.Equal(t, int64(2), GPUCountFromUnstructured(obj))
}
