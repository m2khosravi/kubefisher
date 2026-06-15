package platform

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
)

const (
	labelRayServeDeployment = "ray.io/serve-deployment"
	labelRayClusterName     = "ray.io/cluster-name"
)

var rayServiceGVK = schema.GroupVersionKind{
	Group:   "ray.io",
	Version: "v1",
	Kind:    "RayService",
}

// RayServe writes annotations on ray.io/v1 RayService when detectable.
type RayServe struct{}

func (RayServe) Name() string { return contract.PlatformRayServe }

func (RayServe) Detect(pod *corev1.Pod) bool {
	if pod == nil || pod.Labels == nil {
		return false
	}
	if pod.Labels[contract.AnnPlatform] == contract.PlatformRayServe {
		return true
	}
	return pod.Labels[labelRayServeDeployment] != ""
}

func (RayServe) ResolveTarget(ctx context.Context, c client.Client, pod *corev1.Pod) (client.Object, error) {
	clusterName := rayClusterNameFromPod(pod)
	if clusterName == "" {
		return nil, fmt.Errorf("rayserve: missing %s label on pod %s/%s", labelRayClusterName, pod.Namespace, pod.Name)
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   rayServiceGVK.Group,
		Version: rayServiceGVK.Version,
		Kind:    rayServiceGVK.Kind + "List",
	})
	if err := c.List(ctx, list, client.InNamespace(pod.Namespace)); err != nil {
		return nil, fmt.Errorf("rayserve: list RayServices in %s: %w", pod.Namespace, err)
	}

	for i := range list.Items {
		rs := &list.Items[i]
		activeCluster, found, err := unstructured.NestedString(rs.Object, "status", "activeServiceStatus", "rayClusterName")
		if err != nil {
			return nil, fmt.Errorf("rayserve: read rayClusterName for %s/%s: %w", rs.GetNamespace(), rs.GetName(), err)
		}
		if found && activeCluster == clusterName {
			return rs, nil
		}
	}

	return nil, fmt.Errorf("rayserve: no RayService in %s with active cluster %q", pod.Namespace, clusterName)
}

func (RayServe) WriteCost(ctx context.Context, c client.Client, target client.Object, res CostResult) error {
	return PatchTargetAnnotations(ctx, c, target, res)
}

func rayClusterNameFromPod(pod *corev1.Pod) string {
	if pod == nil || pod.Labels == nil {
		return ""
	}
	return pod.Labels[labelRayClusterName]
}
