package testharness

import (
	"strconv"
	"testing"
	"time"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
	"github.com/m2khosravi/kubefisher/internal/costpatcher/platform"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const kserveCRDDir = "testdata"

// TestKServeLoadingState verifies WriteCost with hour-only (no token) as in loading pods.
func TestKServeLoadingState(t *testing.T) {
	runEnvtest(t, []string{kserveCRDDir}, func(etc *envtestContext) {
		const ns = "default"
		is := kserveInferenceService("loading-model", ns)
		require.NoError(t, etc.Client.Create(etc.Ctx, is))

		pod := kservePod("loading-pod", ns, "loading-model")
		target, err := platform.KServe{}.ResolveTarget(etc.Ctx, etc.Client, pod)
		require.NoError(t, err)

		hour := 1.5
		res := platform.CostResult{
			CostPerHour:   &hour,
			CostPerToken:  nil,
			LastUpdatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		}
		require.NoError(t, platform.KServe{}.WriteCost(etc.Ctx, etc.Client, target, res))

		refetched := target.DeepCopyObject().(client.Object)
		require.NoError(t, etc.Client.Get(etc.Ctx, client.ObjectKeyFromObject(target), refetched))
		anns := refetched.GetAnnotations()
		require.Equal(t, "1.5", anns[contract.AnnCostPerHour])
		_, hasToken := anns[contract.AnnCostPerToken]
		require.False(t, hasToken, "loading state should not write cost-per-token")
	})
}

// TestKServeScaleToZero verifies ReconcileOwners writes $0/hr when readyReplicas is 0.
func TestKServeScaleToZero(t *testing.T) {
	runEnvtest(t, []string{kserveCRDDir}, func(etc *envtestContext) {
		const ns = "default"
		is := kserveInferenceServiceWithStatus("scaled-down", ns, map[string]interface{}{
			"components": map[string]interface{}{
				"predictor": map[string]interface{}{
					"readyReplicas": int64(0),
				},
			},
		})
		require.NoError(t, etc.Client.Create(etc.Ctx, is))

		require.NoError(t, platform.KServe{}.ReconcileOwners(etc.Ctx, etc.Client))

		refetched := is.DeepCopy()
		require.NoError(t, etc.Client.Get(etc.Ctx, client.ObjectKeyFromObject(is), refetched))
		anns := refetched.GetAnnotations()
		require.Equal(t, "0", anns[contract.AnnCostPerHour])
		_, hasToken := anns[contract.AnnCostPerToken]
		require.False(t, hasToken)
	})
}

// TestKServeMultiGPU verifies gpu-count annotation from pod limits.
func TestKServeMultiGPU(t *testing.T) {
	runEnvtest(t, []string{kserveCRDDir}, func(etc *envtestContext) {
		const ns = "default"
		is := kserveInferenceServiceWithSpec("multi-gpu", ns, map[string]interface{}{
			"predictor": map[string]interface{}{
				"model": map[string]interface{}{
					"resources": map[string]interface{}{
						"limits": map[string]interface{}{
							"nvidia.com/gpu": "4",
						},
					},
				},
			},
		})
		require.NoError(t, etc.Client.Create(etc.Ctx, is))

		pod := kservePodWithGPU("multi-gpu-pod", ns, "multi-gpu", 4)
		target, err := platform.KServe{}.ResolveTarget(etc.Ctx, etc.Client, pod)
		require.NoError(t, err)

		hour := 5.0
		gpus := int64(4)
		res := platform.CostResult{
			CostPerHour:   &hour,
			GPUCount:      &gpus,
			LastUpdatedAt: time.Now().UTC(),
		}
		require.NoError(t, platform.KServe{}.WriteCost(etc.Ctx, etc.Client, target, res))

		refetched := target.DeepCopyObject().(client.Object)
		require.NoError(t, etc.Client.Get(etc.Ctx, client.ObjectKeyFromObject(target), refetched))
		require.Equal(t, "4", refetched.GetAnnotations()[contract.AnnGPUCount])
	})
}

// TestKServeCanary verifies stable and canary InferenceServices get independent annotations.
func TestKServeCanary(t *testing.T) {
	runEnvtest(t, []string{kserveCRDDir}, func(etc *envtestContext) {
		const ns = "default"
		stable := kserveInferenceService("my-model", ns)
		canary := kserveInferenceService("my-model-canary", ns)
		require.NoError(t, etc.Client.Create(etc.Ctx, stable))
		require.NoError(t, etc.Client.Create(etc.Ctx, canary))

		adapter := platform.KServe{}
		stableHour := 1.0
		canaryHour := 2.0
		now := time.Now().UTC()

		stableTarget, err := adapter.ResolveTarget(etc.Ctx, etc.Client, kservePod("stable-pod", ns, "my-model"))
		require.NoError(t, err)
		require.NoError(t, adapter.WriteCost(etc.Ctx, etc.Client, stableTarget, platform.CostResult{
			CostPerHour: &stableHour, LastUpdatedAt: now,
		}))

		canaryTarget, err := adapter.ResolveTarget(etc.Ctx, etc.Client, kservePod("canary-pod", ns, "my-model-canary"))
		require.NoError(t, err)
		require.NoError(t, adapter.WriteCost(etc.Ctx, etc.Client, canaryTarget, platform.CostResult{
			CostPerHour: &canaryHour, LastUpdatedAt: now,
		}))

		var stableGot, canaryGot unstructured.Unstructured
		stableGot.SetGroupVersionKind(stable.GroupVersionKind())
		canaryGot.SetGroupVersionKind(canary.GroupVersionKind())
		require.NoError(t, etc.Client.Get(etc.Ctx, client.ObjectKeyFromObject(stable), &stableGot))
		require.NoError(t, etc.Client.Get(etc.Ctx, client.ObjectKeyFromObject(canary), &canaryGot))
		require.Equal(t, "1", stableGot.GetAnnotations()[contract.AnnCostPerHour])
		require.Equal(t, "2", canaryGot.GetAnnotations()[contract.AnnCostPerHour])
	})
}

func kservePod(name, ns, isName string) *corev1.Pod {
	return kservePodWithGPU(name, ns, isName, 1)
}

func kservePodWithGPU(name, ns, isName string, gpus int64) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				"serving.kserve.io/inferenceservice": isName,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "model",
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse(strconv.FormatInt(gpus, 10)),
						},
					},
				},
			},
		},
	}
}

func kserveInferenceServiceWithStatus(name, ns string, status map[string]interface{}) *unstructured.Unstructured {
	u := kserveInferenceService(name, ns)
	if err := unstructured.SetNestedMap(u.Object, status, "status"); err != nil {
		panic(err)
	}
	return u
}

func kserveInferenceServiceWithSpec(name, ns string, spec map[string]interface{}) *unstructured.Unstructured {
	u := kserveInferenceService(name, ns)
	if err := unstructured.SetNestedMap(u.Object, spec, "spec"); err != nil {
		panic(err)
	}
	return u
}
