package platform

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CostResult holds values to write as annotations (nil = omit that annotation).
type CostResult struct {
	CostPerHour   *float64
	CostPerToken  *float64
	GPUCount      *int64
	ReplicaCount  *int32
	LastUpdatedAt time.Time
}

// ReplicasFromTarget reads the current replica count from a resolved owner object.
// It prefers status.readyReplicas (Deployment/StatefulSet) to capture what is actually
// running, but falls back to spec.replicas and finally 1 so the annotation is always
// populated.
func ReplicasFromTarget(target client.Object) int32 {
	switch t := target.(type) {
	case *appsv1.Deployment:
		if t.Status.ReadyReplicas > 0 {
			return t.Status.ReadyReplicas
		}
		if t.Spec.Replicas != nil {
			return *t.Spec.Replicas
		}
		return 1
	case *appsv1.StatefulSet:
		if t.Status.ReadyReplicas > 0 {
			return t.Status.ReadyReplicas
		}
		if t.Spec.Replicas != nil {
			return *t.Spec.Replicas
		}
		return 1
	case *unstructured.Unstructured:
		// Try status.readyReplicas first, then status.replicas, then spec.replicas.
		for _, path := range [][]string{
			{"status", "readyReplicas"},
			{"status", "replicas"},
			{"spec", "replicas"},
		} {
			if v, ok, _ := unstructured.NestedInt64(t.Object, path...); ok && v > 0 {
				return int32(v)
			}
		}
		return 1
	}
	return 1
}

// Adapter maps a GPU pod to the user-owned resource that should receive cost annotations.
type Adapter interface {
	Name() string
	Detect(pod *corev1.Pod) bool
	ResolveTarget(ctx context.Context, c client.Client, pod *corev1.Pod) (client.Object, error)
	WriteCost(ctx context.Context, c client.Client, target client.Object, res CostResult) error
}

// OwnerReconciler is an optional extension for adapters that can reconcile owner
// resources when no pods are running (e.g. scale-to-zero).
type OwnerReconciler interface {
	ReconcileOwners(ctx context.Context, c client.Client) error
}
