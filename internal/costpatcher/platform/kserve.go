package platform

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
)

var inferenceServiceGVK = schema.GroupVersionKind{
	Group:   "serving.kserve.io",
	Version: "v1beta1",
	Kind:    "InferenceService",
}

// KServe writes annotations on serving.kserve.io/v1beta1 InferenceService when detectable.
type KServe struct{}

var _ OwnerReconciler = KServe{}

func (KServe) Name() string { return contract.PlatformKServe }

func (KServe) Detect(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	if pod.Labels == nil {
		return false
	}
	if pod.Labels["kubefisher.io/platform"] == "kserve" {
		return true
	}
	if pod.Labels["serving.kserve.io/inferenceservice"] != "" {
		return true
	}
	return false
}

func (KServe) ResolveTarget(ctx context.Context, c client.Client, pod *corev1.Pod) (client.Object, error) {
	name := inferenceServiceNameFromPod(pod)
	if name == "" {
		return nil, fmt.Errorf("kserve: missing serving.kserve.io/inferenceservice label on pod %s/%s", pod.Namespace, pod.Name)
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(inferenceServiceGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: name}, u); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("kserve: InferenceService %s/%s not found", pod.Namespace, name)
		}
		return nil, err
	}
	return u, nil
}

func (KServe) WriteCost(ctx context.Context, c client.Client, target client.Object, res CostResult) error {
	return PatchTargetAnnotations(ctx, c, target, res)
}

// ReconcileOwners writes scale-to-zero cost annotations on InferenceServices with no ready replicas.
func (KServe) ReconcileOwners(ctx context.Context, c client.Client) error {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   inferenceServiceGVK.Group,
		Version: inferenceServiceGVK.Version,
		Kind:    inferenceServiceGVK.Kind + "List",
	})
	if err := c.List(ctx, list); err != nil {
		return fmt.Errorf("kserve: list InferenceServices: %w", err)
	}

	now := time.Now().UTC()
	zero := 0.0

	for i := range list.Items {
		is := &list.Items[i]
		ns, name := is.GetNamespace(), is.GetName()
		if ns == "" || name == "" {
			continue
		}

		ready, ok, err := predictorReadyReplicas(is)
		if err != nil {
			return fmt.Errorf("kserve: read readyReplicas for %s/%s: %w", ns, name, err)
		}
		if !ok || ready > 0 {
			continue
		}

		hasPods, err := kserveHasActivePods(ctx, c, ns, name)
		if err != nil {
			return fmt.Errorf("kserve: list pods for %s/%s: %w", ns, name, err)
		}
		if hasPods {
			continue
		}

		var gpuCount *int64
		if n := GPUCountFromUnstructured(is.Object); n > 0 {
			gpuCount = &n
		}

		res := CostResult{
			CostPerHour:   &zero,
			CostPerToken:  nil,
			GPUCount:      gpuCount,
			LastUpdatedAt: now,
		}
		if err := PatchTargetAnnotations(ctx, c, is, res); err != nil {
			return fmt.Errorf("kserve: patch scale-to-zero %s/%s: %w", ns, name, err)
		}
	}
	return nil
}

func inferenceServiceNameFromPod(pod *corev1.Pod) string {
	if pod == nil || pod.Labels == nil {
		return ""
	}
	return pod.Labels["serving.kserve.io/inferenceservice"]
}

func predictorReadyReplicas(is *unstructured.Unstructured) (int64, bool, error) {
	// KServe v1beta1: status.components["predictor"].readyReplicas
	components, found, err := unstructured.NestedMap(is.Object, "status", "components")
	if err != nil {
		return 0, false, err
	}
	if !found {
		// Fallback: top-level status.readyReplicas
		v, ok, err := unstructured.NestedInt64(is.Object, "status", "readyReplicas")
		return v, ok, err
	}
	pred, ok := components["predictor"].(map[string]interface{})
	if !ok {
		v, ok, err := unstructured.NestedInt64(is.Object, "status", "readyReplicas")
		return v, ok, err
	}
	v, ok := pred["readyReplicas"]
	if !ok {
		return 0, false, nil
	}
	switch n := v.(type) {
	case int64:
		return n, true, nil
	case int:
		return int64(n), true, nil
	case float64:
		return int64(n), true, nil
	default:
		return 0, false, nil
	}
}

func kserveHasActivePods(ctx context.Context, c client.Client, ns, isName string) (bool, error) {
	var pods corev1.PodList
	if err := c.List(ctx, &pods,
		client.InNamespace(ns),
		client.MatchingLabels(map[string]string{
			"serving.kserve.io/inferenceservice": isName,
		}),
	); err != nil {
		return false, err
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.DeletionTimestamp != nil {
			continue
		}
		switch p.Status.Phase {
		case corev1.PodRunning, corev1.PodPending:
			return true, nil
		}
	}
	return false, nil
}
