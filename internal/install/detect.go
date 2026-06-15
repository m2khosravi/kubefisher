package install

import (
	"context"
	"log/slog"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InstalledState captures what is already present in the cluster before each
// install step. All fields default to false/0 — a detection error is treated
// as "not found" so install proceeds (idempotent on re-run via helm upgrade).
type InstalledState struct {
	Prometheus   bool
	DCGM         bool
	CostPatcher  bool
	GPUNodeCount int
	// Serving platforms — probed for the end-of-install summary only.
	KServe   bool
	BentoML  bool
	RayTrain bool
}

// detectInstalled queries the cluster for each component and returns the
// current state. Errors per-component are logged and treated as "not present".
func detectInstalled(ctx context.Context, cl client.Client, disc discovery.DiscoveryInterface) InstalledState {
	var state InstalledState
	state.Prometheus = detectPrometheus(ctx, cl)
	state.DCGM = detectDCGM(ctx, cl)
	state.CostPatcher = detectCostPatcher(ctx, cl)
	state.GPUNodeCount = countGPUNodes(ctx, cl)
	state.KServe, state.BentoML, state.RayTrain = detectServingCRDs(disc)
	return state
}

// detectPrometheus returns true when a Deployment labelled
// app.kubernetes.io/name=prometheus exists in any namespace.
func detectPrometheus(ctx context.Context, cl client.Client) bool {
	var list appsv1.DeploymentList
	if err := cl.List(ctx, &list,
		client.MatchingLabels{"app.kubernetes.io/name": "prometheus"},
	); err != nil {
		slog.Debug("prometheus detection error", "err", err)
		return false
	}
	if len(list.Items) > 0 {
		slog.Info("Prometheus detected", "deployments", len(list.Items))
		return true
	}
	// Also check for the prometheus-operator CRD presence via a StatefulSet
	// that kube-prometheus-stack creates (app=prometheus).
	var ssList appsv1.StatefulSetList
	if err := cl.List(ctx, &ssList,
		client.MatchingLabels{"app.kubernetes.io/name": "prometheus"},
	); err != nil {
		slog.Debug("prometheus statefulset detection error", "err", err)
		return false
	}
	if len(ssList.Items) > 0 {
		slog.Info("Prometheus detected (StatefulSet)", "statefulsets", len(ssList.Items))
		return true
	}
	return false
}

// detectDCGM returns true when a DaemonSet labelled app=dcgm-exporter exists
// in any namespace, or when the gpu-operator namespace is present.
func detectDCGM(ctx context.Context, cl client.Client) bool {
	var list appsv1.DaemonSetList
	if err := cl.List(ctx, &list,
		client.MatchingLabels{"app": "dcgm-exporter"},
	); err != nil {
		slog.Debug("DCGM DaemonSet detection error", "err", err)
	} else if len(list.Items) > 0 {
		slog.Info("DCGM exporter detected", "daemonsets", len(list.Items))
		return true
	}

	// Also accept the gpu-operator namespace as a signal that DCGM is managed
	// by the GPU Operator.
	var nsList corev1.NamespaceList
	if err := cl.List(ctx, &nsList,
		client.MatchingLabels{"kubernetes.io/metadata.name": "gpu-operator"},
	); err != nil {
		slog.Debug("gpu-operator namespace detection error", "err", err)
		return false
	}
	for _, ns := range nsList.Items {
		if ns.Name == "gpu-operator" {
			slog.Info("GPU Operator namespace detected")
			return true
		}
	}
	return false
}

// detectCostPatcher returns true when a Deployment labelled
// app.kubernetes.io/component=cost-patcher exists in any namespace.
func detectCostPatcher(ctx context.Context, cl client.Client) bool {
	var list appsv1.DeploymentList
	if err := cl.List(ctx, &list,
		client.MatchingLabels{"app.kubernetes.io/component": "cost-patcher"},
	); err != nil {
		slog.Debug("cost-patcher detection error", "err", err)
		return false
	}
	if len(list.Items) > 0 {
		slog.Info("Cost-patcher detected", "deployments", len(list.Items))
		return true
	}
	return false
}

// countGPUNodes lists all nodes and counts those with nvidia.com/gpu capacity > 0.
func countGPUNodes(ctx context.Context, cl client.Client) int {
	var nodes corev1.NodeList
	if err := cl.List(ctx, &nodes); err != nil {
		slog.Debug("node list error", "err", err)
		return 0
	}
	count := 0
	for _, n := range nodes.Items {
		if qty, ok := n.Status.Capacity["nvidia.com/gpu"]; ok {
			if qty.Cmp(resource.MustParse("0")) > 0 {
				count++
			}
		}
	}
	if count > 0 {
		slog.Info("GPU nodes detected", "count", count)
	}
	return count
}

// detectServingCRDs probes the discovery API for well-known serving platform
// CRDs. Errors are silently ignored — absence is the safe default.
func detectServingCRDs(disc discovery.DiscoveryInterface) (kserve, bentoml, rayTrain bool) {
	resourceLists, err := disc.ServerPreferredResources()
	if err != nil {
		slog.Debug("discovery ServerPreferredResources error", "err", err)
		return
	}
	for _, rl := range resourceLists {
		group := strings.SplitN(rl.GroupVersion, "/", 2)[0]
		for _, r := range rl.APIResources {
			switch {
			case strings.Contains(group, "serving.kserve.io") && r.Name == "inferenceservices":
				kserve = true
			case strings.Contains(group, "serving.yatai.ai") && r.Name == "bentodeployments",
				strings.Contains(group, "serving.bento.ai") && r.Name == "bentodeployments":
				bentoml = true
			case strings.Contains(group, "ray.io") && r.Name == "rayjobs":
				rayTrain = true
			}
		}
	}
	return
}
