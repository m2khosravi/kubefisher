package workload

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FindPodsForResource returns pods backing the workload (Running or Pending).
func FindPodsForResource(ctx context.Context, st *WorkloadStatus, cl client.Client) ([]corev1.Pod, error) {
	selector, err := podLabelSelector(ctx, st, cl)
	if err != nil {
		return nil, err
	}
	if len(selector) == 0 {
		return nil, fmt.Errorf("could not determine pod selector for %s/%s", st.Kind, st.Name)
	}

	var list corev1.PodList
	if err := cl.List(ctx, &list,
		client.InNamespace(st.Namespace),
		client.MatchingLabels(selector),
	); err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	out := make([]corev1.Pod, 0, len(list.Items))
	for i := range list.Items {
		p := list.Items[i]
		switch p.Status.Phase {
		case corev1.PodRunning, corev1.PodPending:
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no running or pending pods found for %s %q", st.Kind, st.Name)
	}
	return out, nil
}

func podLabelSelector(ctx context.Context, st *WorkloadStatus, cl client.Client) (map[string]string, error) {
	switch st.Kind {
	case "Deployment":
		var dep appsv1.Deployment
		if err := cl.Get(ctx, client.ObjectKey{Namespace: st.Namespace, Name: st.Name}, &dep); err != nil {
			return nil, err
		}
		if dep.Spec.Selector != nil && len(dep.Spec.Selector.MatchLabels) > 0 {
			return dep.Spec.Selector.MatchLabels, nil
		}
	case "StatefulSet":
		var sts appsv1.StatefulSet
		if err := cl.Get(ctx, client.ObjectKey{Namespace: st.Namespace, Name: st.Name}, &sts); err != nil {
			return nil, err
		}
		if sts.Spec.Selector != nil && len(sts.Spec.Selector.MatchLabels) > 0 {
			return sts.Spec.Selector.MatchLabels, nil
		}
	case "InferenceService":
		return map[string]string{"serving.kserve.io/inferenceservice": st.Name}, nil
	case "BentoDeployment":
		return map[string]string{"yatai.bentoml.com/bento-deployment": st.Name}, nil
	case "TrainJob", "PyTorchJob", "TFJob", "XGBoostJob", "MPIJob", "PaddleJob":
		return map[string]string{"training.kubeflow.org/job-name": st.Name}, nil
	case "RayService":
		return map[string]string{"ray.io/serve-deployment": st.Name}, nil
	case "RayJob":
		return map[string]string{"ray.io/job-name": st.Name}, nil
	case "RayCluster":
		return map[string]string{"ray.io/cluster-name": st.Name}, nil
	}
	return map[string]string{"app.kubernetes.io/name": st.Name}, nil
}
