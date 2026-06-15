package costpatcher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
	"github.com/m2khosravi/kubefisher/internal/costpatcher/platform"
	"github.com/m2khosravi/kubefisher/pkg/promclient"
)

// Reconciler periodically patches owner workload annotations from Prometheus recording rules.
type Reconciler struct {
	K8s      client.Client
	Prom     *promclient.Client
	Adapters []platform.Adapter
	Log      *slog.Logger
}

func (r *Reconciler) Run(ctx context.Context, every time.Duration) error {
	t := time.NewTicker(every)
	defer t.Stop()

	if err := r.tick(ctx); err != nil {
		r.Log.Error("reconcile tick failed", "err", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := r.tick(ctx); err != nil {
				r.Log.Error("reconcile tick failed", "err", err)
			}
		}
	}
}

func (r *Reconciler) tick(ctx context.Context) error {
	var pods corev1.PodList
	if err := r.K8s.List(ctx, &pods); err != nil {
		return err
	}

	for i := range pods.Items {
		pod := pods.Items[i]
		if pod.Namespace == "" || pod.Name == "" {
			continue
		}
		gpus := platform.GPUCountFromPod(&pod)
		if gpus <= 0 {
			continue
		}

		var chosen platform.Adapter
		for _, a := range r.Adapters {
			if a.Detect(&pod) {
				chosen = a
				break
			}
		}
		if chosen == nil {
			continue
		}

		target, err := chosen.ResolveTarget(ctx, r.K8s, &pod)
		if err != nil {
			r.Log.Debug("skip pod: resolve target", "adapter", chosen.Name(), "namespace", pod.Namespace, "pod", pod.Name, "err", err)
			continue
		}

		cph, okHour, err := r.Prom.QueryInstant(ctx, fmt.Sprintf(`kubefisher:cost_per_hour{namespace=%q,pod=%q}`, pod.Namespace, pod.Name))
		if err != nil {
			return fmt.Errorf("prometheus cost_per_hour pod=%s/%s: %w", pod.Namespace, pod.Name, err)
		}
		if !okHour {
			r.Log.Debug("cost_per_hour absent; removing stale cost annotations", "namespace", pod.Namespace, "pod", pod.Name)
			if rmErr := platform.RemoveCostAnnotations(ctx, r.K8s, target,
				contract.AnnCostPerHourPerReplica, contract.AnnCostPerHourTotal, contract.AnnCostPerHour,
				contract.AnnCostPerToken, contract.AnnGPUCount, contract.AnnLastUpdated,
			); rmErr != nil {
				r.Log.Error("failed to remove stale cost annotations", "namespace", pod.Namespace, "pod", pod.Name, "err", rmErr)
			}
			continue
		}

		var cpt *float64
		if !isPodLoading(&pod) {
			if v, okTok, err := r.Prom.QueryInstant(ctx, fmt.Sprintf(`kubefisher:cost_per_token{namespace=%q,pod=%q}`, pod.Namespace, pod.Name)); err == nil && okTok {
				cpt = &v
			} else if err != nil {
				return fmt.Errorf("prometheus cost_per_token pod=%s/%s: %w", pod.Namespace, pod.Name, err)
			}
		}

		gpuCount := gpus
		replicas := platform.ReplicasFromTarget(target)
		res := platform.CostResult{
			CostPerHour:   &cph,
			CostPerToken:  cpt,
			GPUCount:      &gpuCount,
			ReplicaCount:  &replicas,
			LastUpdatedAt: time.Now().UTC(),
		}
		if err := chosen.WriteCost(ctx, r.K8s, target, res); err != nil {
			r.Log.Error("write cost failed", "adapter", chosen.Name(), "namespace", pod.Namespace, "pod", pod.Name, "err", err)
			continue
		}
		r.Log.Debug("patched cost annotations", "adapter", chosen.Name(), "namespace", pod.Namespace, "pod", pod.Name, "target", fmt.Sprintf("%T", target))
	}

	for _, a := range r.Adapters {
		or, ok := a.(platform.OwnerReconciler)
		if !ok {
			continue
		}
		if err := or.ReconcileOwners(ctx, r.K8s); err != nil {
			r.Log.Error("owner reconcile failed", "adapter", a.Name(), "err", err)
		}
	}
	return nil
}

// isPodLoading reports pods that are not yet serving (skip cost/token until running).
func isPodLoading(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	if pod.Status.Phase == corev1.PodRunning {
		return false
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting == nil {
			continue
		}
		switch cs.State.Waiting.Reason {
		case "ContainerCreating", "ImagePullBackOff", "ErrImagePull", "PodInitializing":
			return true
		}
	}
	return pod.Status.Phase == corev1.PodPending
}
