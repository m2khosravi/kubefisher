package testharness

import (
	"testing"
	"time"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
	"github.com/m2khosravi/kubefisher/internal/costpatcher/platform"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const kubeflowTrainerCRDDir = "testdata"

func TestKubeflowTrainerRunningJob(t *testing.T) {
	runEnvtest(t, []string{kubeflowTrainerCRDDir}, func(etc *envtestContext) {
		const ns = "default"
		tj := trainJobWithStatus("running-job", ns, map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{
					"type":               "Running",
					"status":             "True",
					"lastTransitionTime": "2026-05-05T10:00:00Z",
				},
			},
		})
		require.NoError(t, etc.Client.Create(etc.Ctx, tj))

		pod := kubeflowTrainerPod("running-pod", ns, "running-job")
		target, err := platform.KubeflowTrainer{}.ResolveTarget(etc.Ctx, etc.Client, pod)
		require.NoError(t, err)

		hour := 3.5
		res := platform.CostResult{
			CostPerHour:   &hour,
			LastUpdatedAt: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		}
		require.NoError(t, platform.KubeflowTrainer{}.WriteCost(etc.Ctx, etc.Client, target, res))

		refetched := target.DeepCopyObject().(client.Object)
		require.NoError(t, etc.Client.Get(etc.Ctx, client.ObjectKeyFromObject(target), refetched))
		anns := refetched.GetAnnotations()
		require.Equal(t, "3.5", anns[contract.AnnCostPerHour])
		_, hasTotal := anns[contract.AnnTotalJobCostUSD]
		require.False(t, hasTotal, "running job should not write total-job-cost-usd")
	})
}

func TestKubeflowTrainerCompletedJob(t *testing.T) {
	runEnvtest(t, []string{kubeflowTrainerCRDDir}, func(etc *envtestContext) {
		const ns = "default"
		tj := trainJobWithStatus("completed-job", ns, map[string]interface{}{
			"startTime":      "2026-05-05T10:00:00Z",
			"completionTime": "2026-05-05T12:00:00Z",
			"conditions": []interface{}{
				map[string]interface{}{
					"type":               "Complete",
					"status":             "True",
					"lastTransitionTime": "2026-05-05T12:00:00Z",
				},
			},
		})
		require.NoError(t, etc.Client.Create(etc.Ctx, tj))

		pod := kubeflowTrainerPod("completed-pod", ns, "completed-job")
		target, err := platform.KubeflowTrainer{}.ResolveTarget(etc.Ctx, etc.Client, pod)
		require.NoError(t, err)

		hour := 3.5
		res := platform.CostResult{
			CostPerHour:   &hour,
			LastUpdatedAt: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		}
		require.NoError(t, platform.KubeflowTrainer{}.WriteCost(etc.Ctx, etc.Client, target, res))

		refetched := target.DeepCopyObject().(client.Object)
		require.NoError(t, etc.Client.Get(etc.Ctx, client.ObjectKeyFromObject(target), refetched))
		anns := refetched.GetAnnotations()
		require.Equal(t, "3.5", anns[contract.AnnCostPerHour])
		require.Equal(t, "7", anns[contract.AnnTotalJobCostUSD], "2h × $3.50/hr = $7")
	})
}

func TestKubeflowTrainerTotalCostIdempotent(t *testing.T) {
	runEnvtest(t, []string{kubeflowTrainerCRDDir}, func(etc *envtestContext) {
		const ns = "default"
		tj := trainJobWithStatus("idempotent-job", ns, map[string]interface{}{
			"startTime":      "2026-05-05T10:00:00Z",
			"completionTime": "2026-05-05T12:00:00Z",
			"conditions": []interface{}{
				map[string]interface{}{
					"type":               "Complete",
					"status":             "True",
					"lastTransitionTime": "2026-05-05T12:00:00Z",
				},
			},
		})
		tj.SetAnnotations(map[string]string{
			contract.AnnTotalJobCostUSD: "99.99",
		})
		require.NoError(t, etc.Client.Create(etc.Ctx, tj))

		pod := kubeflowTrainerPod("idempotent-pod", ns, "idempotent-job")
		target, err := platform.KubeflowTrainer{}.ResolveTarget(etc.Ctx, etc.Client, pod)
		require.NoError(t, err)

		hour := 3.5
		res := platform.CostResult{
			CostPerHour:   &hour,
			LastUpdatedAt: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		}
		require.NoError(t, platform.KubeflowTrainer{}.WriteCost(etc.Ctx, etc.Client, target, res))

		refetched := target.DeepCopyObject().(client.Object)
		require.NoError(t, etc.Client.Get(etc.Ctx, client.ObjectKeyFromObject(target), refetched))
		require.Equal(t, "99.99", refetched.GetAnnotations()[contract.AnnTotalJobCostUSD])
	})
}

func trainJobWithStatus(name, ns string, status map[string]interface{}) *unstructured.Unstructured {
	u := trainJob(name, ns)
	if err := unstructured.SetNestedMap(u.Object, status, "status"); err != nil {
		panic(err)
	}
	return u
}

func kubeflowTrainerPod(name, ns, trainJobName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				"trainer.kubeflow.org/trainjob-name":          trainJobName,
				"trainer.kubeflow.org/trainjob-ancestor-step": "trainer",
			},
		},
	}
}
