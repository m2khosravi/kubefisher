package platform

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
)

const labelBentoDeployment = "yatai.bentoml.com/bento-deployment"

var bentoDeploymentGVK = schema.GroupVersionKind{
	Group:   "serving.yatai.ai",
	Version: "v2alpha1",
	Kind:    "BentoDeployment",
}

// BentoML writes annotations on serving.yatai.ai/v2alpha1 BentoDeployment when detectable.
type BentoML struct{}

func (BentoML) Name() string { return contract.PlatformBentoML }

func (BentoML) Detect(pod *corev1.Pod) bool {
	if pod == nil || pod.Labels == nil {
		return false
	}
	if pod.Labels[contract.AnnPlatform] == contract.PlatformBentoML {
		return true
	}
	return pod.Labels[labelBentoDeployment] != ""
}

func (BentoML) ResolveTarget(ctx context.Context, c client.Client, pod *corev1.Pod) (client.Object, error) {
	name := bentoDeploymentNameFromPod(pod)
	if name == "" {
		return nil, fmt.Errorf("bentoml: missing %s label on pod %s/%s", labelBentoDeployment, pod.Namespace, pod.Name)
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(bentoDeploymentGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: name}, u); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("bentoml: BentoDeployment %s/%s not found", pod.Namespace, name)
		}
		return nil, err
	}
	return u, nil
}

func (BentoML) WriteCost(ctx context.Context, c client.Client, target client.Object, res CostResult) error {
	return PatchTargetAnnotations(ctx, c, target, res)
}

func bentoDeploymentNameFromPod(pod *corev1.Pod) string {
	if pod == nil || pod.Labels == nil {
		return ""
	}
	return pod.Labels[labelBentoDeployment]
}
