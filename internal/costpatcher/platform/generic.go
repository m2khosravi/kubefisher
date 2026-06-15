package platform

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Generic resolves Pod → ReplicaSet → Deployment, or Pod → StatefulSet.
type Generic struct{}

func (Generic) Name() string { return "generic" }

func (Generic) Detect(*corev1.Pod) bool { return true }

func (Generic) ResolveTarget(ctx context.Context, c client.Client, pod *corev1.Pod) (client.Object, error) {
	obj := client.Object(pod)
	for i := 0; i < 16; i++ {
		metaObj, ok := obj.(metav1.Object)
		if !ok {
			return nil, fmt.Errorf("object is not metav1.Object: %T", obj)
		}
		ns := metaObj.GetNamespace()
		ref := metav1.GetControllerOf(metaObj)
		if ref == nil {
			return nil, fmt.Errorf("no controller owner for %T %s/%s", obj, ns, metaObj.GetName())
		}

		switch ref.Kind {
		case "ReplicaSet":
			rs := &appsv1.ReplicaSet{}
			if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: ref.Name}, rs); err != nil {
				if apierrors.IsNotFound(err) {
					return nil, fmt.Errorf("replicaset %s/%s not found", ns, ref.Name)
				}
				return nil, err
			}
			obj = rs
			continue
		case "StatefulSet":
			sts := &appsv1.StatefulSet{}
			if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: ref.Name}, sts); err != nil {
				return nil, err
			}
			return sts, nil
		case "Deployment":
			dep := &appsv1.Deployment{}
			if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: ref.Name}, dep); err != nil {
				return nil, err
			}
			return dep, nil
		case "Job", "DaemonSet":
			return nil, fmt.Errorf("unsupported owner kind %s for cost annotations (namespace=%s name=%s)", ref.Kind, ns, ref.Name)
		default:
			return nil, fmt.Errorf("unsupported owner kind %s (namespace=%s name=%s)", ref.Kind, ns, ref.Name)
		}
	}
	return nil, fmt.Errorf("owner reference walk exceeded depth for pod %s/%s", pod.Namespace, pod.Name)
}

func (Generic) WriteCost(ctx context.Context, c client.Client, target client.Object, res CostResult) error {
	return PatchTargetAnnotations(ctx, c, target, res)
}
