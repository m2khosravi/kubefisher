package cost

import (
	"testing"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestIsBentoDeploymentGVR(t *testing.T) {
	require.True(t, IsBentoDeploymentGVR("serving.yatai.ai", "bentodeployments"))
	require.True(t, IsBentoDeploymentGVR("serving.bento.ai", "bentodeployments"))
	require.False(t, IsBentoDeploymentGVR("serving.yatai.ai", "inferenceservices"))
	require.False(t, IsBentoDeploymentGVR("apps", "deployments"))
}

func TestAddWorkloadRowBentoDeployment(t *testing.T) {
	byKey := map[rowKey]CostRow{}
	addWorkloadRow(byKey, "default", "llama3", "BentoDeployment",
		nil,
		map[string]string{
			contract.AnnCostPerHour: "1.25",
			contract.AnnPlatform:  contract.PlatformBentoML,
		},
		nil, 1, corev1.PodSpec{})

	row, ok := byKey[rowKey{namespace: "default", kind: "BentoDeployment", name: "llama3"}]
	require.True(t, ok)
	require.Equal(t, contract.PlatformBentoML, row.Platform)
	require.Equal(t, contract.WorkloadTypeInference, row.WorkloadType)
	require.NotNil(t, row.CostPerHour)
	require.Equal(t, 1.25, *row.CostPerHour)
}

func TestIsBentoChildDeployment(t *testing.T) {
	dep := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"yatai.bentoml.com/bento-deployment": "llama3"},
				},
			},
		},
	}
	require.True(t, isBentoChildDeployment(dep))
	require.False(t, isBentoChildDeployment(&appsv1.Deployment{}))
}

func TestPodSpecFromUnstructuredBentoResources(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"spec": map[string]interface{}{
			"resources": map[string]interface{}{
				"limits": map[string]interface{}{
					"nvidia.com/gpu": "2",
				},
			},
		},
	}}
	spec := podSpecFromUnstructured(u)
	require.True(t, podSpecRequestsGPU(spec))
}
