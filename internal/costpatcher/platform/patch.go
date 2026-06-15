package platform

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
)

func formatCost(v float64) string {
	s := strconv.FormatFloat(v, 'f', 12, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func buildAnnotations(res CostResult) map[string]string {
	out := map[string]string{}
	if res.CostPerHour != nil {
		perReplica := formatCost(*res.CostPerHour)
		out[contract.AnnCostPerHourPerReplica] = perReplica
		// Deprecated dual-write for migration; remove on next MAJOR bump.
		out[contract.AnnCostPerHour] = perReplica
		if res.ReplicaCount != nil && *res.ReplicaCount > 0 {
			out[contract.AnnCostPerHourTotal] = formatCost(*res.CostPerHour * float64(*res.ReplicaCount))
		}
	}
	if res.CostPerToken != nil {
		out[contract.AnnCostPerToken] = formatCost(*res.CostPerToken)
	}
	if res.GPUCount != nil {
		out[contract.AnnGPUCount] = strconv.FormatInt(*res.GPUCount, 10)
	}
	out[contract.AnnLastUpdated] = res.LastUpdatedAt.UTC().Format(time.RFC3339)
	return out
}

func shouldPatchCostAnnotations(existing map[string]string, res CostResult) bool {
	if res.CostPerHour != nil {
		want := formatCost(*res.CostPerHour)
		if existing[contract.AnnCostPerHourPerReplica] != want {
			return true
		}
		if existing[contract.AnnCostPerHour] != want {
			return true
		}
		if res.ReplicaCount != nil && *res.ReplicaCount > 0 {
			wantTotal := formatCost(*res.CostPerHour * float64(*res.ReplicaCount))
			if existing[contract.AnnCostPerHourTotal] != wantTotal {
				return true
			}
		}
	}
	if res.CostPerToken != nil {
		want := formatCost(*res.CostPerToken)
		if existing[contract.AnnCostPerToken] != want {
			return true
		}
	} else if _, exists := existing[contract.AnnCostPerToken]; exists {
		// Prometheus returned no cost_per_token series; stale annotation must be removed.
		return true
	}
	if res.GPUCount != nil {
		want := strconv.FormatInt(*res.GPUCount, 10)
		if existing[contract.AnnGPUCount] != want {
			return true
		}
	}
	return false
}

// RemoveCostAnnotations deletes the named annotation keys from target and patches if any
// of those keys were present. Used to clear stale cost annotations when Prometheus returns
// no series for a previously-annotated owner.
func RemoveCostAnnotations(ctx context.Context, c client.Client, target client.Object, keys ...string) error {
	anns := target.GetAnnotations()
	if len(anns) == 0 {
		return nil
	}
	changed := false
	for _, k := range keys {
		if _, exists := anns[k]; exists {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	orig := target.DeepCopyObject().(client.Object)
	patch := client.MergeFrom(orig)
	for _, k := range keys {
		delete(anns, k)
	}
	target.SetAnnotations(anns)
	switch t := target.(type) {
	case *appsv1.Deployment:
		return c.Patch(ctx, t, patch)
	case *appsv1.StatefulSet:
		return c.Patch(ctx, t, patch)
	case *unstructured.Unstructured:
		return c.Patch(ctx, t, patch)
	default:
		return fmt.Errorf("unsupported patch target type %T", target)
	}
}

// PatchTargetAnnotations merge-patches only annotations on supported target types.
func PatchTargetAnnotations(ctx context.Context, c client.Client, target client.Object, res CostResult) error {
	existing := target.GetAnnotations()
	if !shouldPatchCostAnnotations(existing, res) {
		return nil
	}
	orig := target.DeepCopyObject().(client.Object)
	patch := client.MergeFrom(orig)

	anns := target.GetAnnotations()
	if anns == nil {
		anns = map[string]string{}
	}
	for k, v := range buildAnnotations(res) {
		anns[k] = v
	}
	// If Prometheus returned no cost_per_token series, remove any stale annotation.
	if res.CostPerToken == nil {
		delete(anns, contract.AnnCostPerToken)
	}
	target.SetAnnotations(anns)

	switch t := target.(type) {
	case *appsv1.Deployment:
		return c.Patch(ctx, t, patch)
	case *appsv1.StatefulSet:
		return c.Patch(ctx, t, patch)
	case *unstructured.Unstructured:
		return c.Patch(ctx, t, patch)
	default:
		return fmt.Errorf("unsupported patch target type %T", target)
	}
}
