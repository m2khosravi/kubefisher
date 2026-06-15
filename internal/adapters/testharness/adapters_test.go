package testharness

import (
	"testing"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/platform"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGenericAdapterSuite(t *testing.T) {
	const ns = "default"
	dep := minimalDeployment("generic-cost-target", ns)
	pod := PodWithControllerOwner(dep, "generic-pod")

	AdapterTestSuite(t, platform.Generic{}, pod, dep)
}

func TestBentoMLAdapterSuite(t *testing.T) {
	const ns = "default"
	const name = "llama3-deployment"
	bd := bentoDeployment(name, ns)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bento-pod",
			Namespace: ns,
			Labels: map[string]string{
				"yatai.bentoml.com/bento-deployment": name,
			},
		},
	}

	AdapterTestSuite(t, platform.BentoML{}, pod, bd,
		WithCRDDirectoryPaths("testdata"),
		WithNonMatchPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": "not-bento"},
			},
		}),
	)
}

func TestKubeflowTrainerAdapterSuite(t *testing.T) {
	const ns = "default"
	const name = "fine-tune-llama"
	tj := trainJob(name, ns)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "trainer-pod",
			Namespace: ns,
			Labels: map[string]string{
				"trainer.kubeflow.org/trainjob-name":          name,
				"trainer.kubeflow.org/trainjob-ancestor-step": "trainer",
			},
		},
	}

	AdapterTestSuite(t, platform.KubeflowTrainer{}, pod, tj,
		WithCRDDirectoryPaths("testdata"),
		WithNonMatchPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": "not-trainer"},
			},
		}),
	)
}

func TestRayServeAdapterSuite(t *testing.T) {
	const ns = "default"
	const clusterName = "my-svc-raycluster-abc"
	rs := rayService("my-svc", ns, clusterName)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "serve-pod",
			Namespace: ns,
			Labels: map[string]string{
				"ray.io/serve-deployment": "my-app",
				"ray.io/cluster-name":     clusterName,
			},
		},
	}

	AdapterTestSuite(t, platform.RayServe{}, pod, rs,
		WithCRDDirectoryPaths("testdata"),
		WithNonMatchPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": "not-ray"},
			},
		}),
	)
}

func TestKServeAdapterSuite(t *testing.T) {
	const ns = "default"
	const name = "my-model"
	is := kserveInferenceService(name, ns)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kserve-pod",
			Namespace: ns,
			Labels: map[string]string{
				"serving.kserve.io/inferenceservice": name,
			},
		},
	}

	AdapterTestSuite(t, platform.KServe{}, pod, is,
		WithCRDDirectoryPaths("testdata"),
		WithNonMatchPod(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"kubefisher.io/platform": "vllm"},
			},
		}),
	)
}

func minimalDeployment(name, ns string) *appsv1.Deployment {
	labels := map[string]string{"app": name}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "pause", Image: "registry.k8s.io/pause:3.9"},
					},
				},
			},
		},
	}
}

func kserveInferenceService(name, ns string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("serving.kserve.io/v1beta1")
	u.SetKind("InferenceService")
	u.SetName(name)
	u.SetNamespace(ns)
	return u
}

func bentoDeployment(name, ns string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("serving.yatai.ai/v2alpha1")
	u.SetKind("BentoDeployment")
	u.SetName(name)
	u.SetNamespace(ns)
	return u
}

func trainJob(name, ns string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("trainer.kubeflow.org/v1alpha1")
	u.SetKind("TrainJob")
	u.SetName(name)
	u.SetNamespace(ns)
	return u
}

func rayService(name, ns, clusterName string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("ray.io/v1")
	u.SetKind("RayService")
	u.SetName(name)
	u.SetNamespace(ns)
	if err := unstructured.SetNestedField(u.Object, clusterName, "status", "activeServiceStatus", "rayClusterName"); err != nil {
		panic(err)
	}
	return u
}
