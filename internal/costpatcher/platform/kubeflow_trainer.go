package platform

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
)

const (
	labelTrainJobName      = "trainer.kubeflow.org/trainjob-name"
	labelTrainJobStep      = "trainer.kubeflow.org/trainjob-ancestor-step"
	labelJobSetName        = "jobset.sigs.k8s.io/jobset-name"
	trainJobCompleteType   = "Complete"
	trainJobFailedType     = "Failed"
)

var trainJobGVK = schema.GroupVersionKind{
	Group:   "trainer.kubeflow.org",
	Version: "v1alpha1",
	Kind:    "TrainJob",
}

// KubeflowTrainer writes cost annotations on trainer.kubeflow.org/v1alpha1 TrainJob.
type KubeflowTrainer struct{}

func (KubeflowTrainer) Name() string { return contract.PlatformKubeflowTrainer }

func (KubeflowTrainer) Detect(pod *corev1.Pod) bool {
	if pod == nil || pod.Labels == nil {
		return false
	}
	if pod.Labels[contract.AnnPlatform] == contract.PlatformKubeflowTrainer {
		return true
	}
	return pod.Labels[labelTrainJobStep] != "" || pod.Labels[labelTrainJobName] != ""
}

func (KubeflowTrainer) ResolveTarget(ctx context.Context, c client.Client, pod *corev1.Pod) (client.Object, error) {
	name := trainJobNameFromPod(pod)
	if name == "" {
		return nil, fmt.Errorf("kubeflow-trainer: missing %s (or %s) label on pod %s/%s",
			labelTrainJobName, labelJobSetName, pod.Namespace, pod.Name)
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(trainJobGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: name}, u); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("kubeflow-trainer: TrainJob %s/%s not found", pod.Namespace, name)
		}
		return nil, err
	}
	return u, nil
}

func (KubeflowTrainer) WriteCost(ctx context.Context, c client.Client, target client.Object, res CostResult) error {
	if err := PatchTargetAnnotations(ctx, c, target, res); err != nil {
		return err
	}
	return patchTrainJobTotalCostIfComplete(ctx, c, target, res)
}

func trainJobNameFromPod(pod *corev1.Pod) string {
	if pod == nil || pod.Labels == nil {
		return ""
	}
	if name := pod.Labels[labelTrainJobName]; name != "" {
		return name
	}
	// JobSet name matches TrainJob name in Kubeflow Trainer v2.
	return pod.Labels[labelJobSetName]
}

func patchTrainJobTotalCostIfComplete(ctx context.Context, c client.Client, target client.Object, res CostResult) error {
	u, ok := target.(*unstructured.Unstructured)
	if !ok {
		return nil
	}
	if u.GroupVersionKind() != trainJobGVK {
		return nil
	}

	anns := u.GetAnnotations()
	if anns != nil && anns[contract.AnnTotalJobCostUSD] != "" {
		return nil
	}

	complete, failed := trainJobTerminalState(u)
	if !complete && !failed {
		return nil
	}
	if res.CostPerHour == nil {
		return nil
	}

	duration, ok := trainJobDuration(u)
	if !ok || duration <= 0 {
		return nil
	}

	hours := duration.Hours()
	total := *res.CostPerHour * hours
	if total < 0 {
		total = 0
	}

	orig := u.DeepCopy()
	patch := client.MergeFrom(orig)
	if anns == nil {
		anns = map[string]string{}
	}
	anns[contract.AnnTotalJobCostUSD] = formatCost(total)
	u.SetAnnotations(anns)
	return c.Patch(ctx, u, patch)
}

func trainJobTerminalState(u *unstructured.Unstructured) (complete, failed bool) {
	conditions, found, err := unstructured.NestedSlice(u.Object, "status", "conditions")
	if err != nil || !found {
		return false, false
	}
	for _, item := range conditions {
		cond, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := cond["type"].(string)
		status, _ := cond["status"].(string)
		if !strings.EqualFold(status, "true") {
			continue
		}
		switch typ {
		case trainJobCompleteType, "Succeeded":
			complete = true
		case trainJobFailedType:
			failed = true
		}
	}
	return complete, failed
}

func trainJobDuration(u *unstructured.Unstructured) (time.Duration, bool) {
	if start, end, ok := trainJobTimesFromStatus(u); ok {
		return end.Sub(start), true
	}
	return trainJobDurationFromConditions(u)
}

func trainJobTimesFromStatus(u *unstructured.Unstructured) (start, end time.Time, ok bool) {
	startStr, startFound, err := unstructured.NestedString(u.Object, "status", "startTime")
	if err != nil || !startFound || startStr == "" {
		return time.Time{}, time.Time{}, false
	}
	endStr, endFound, err := unstructured.NestedString(u.Object, "status", "completionTime")
	if err != nil || !endFound || endStr == "" {
		return time.Time{}, time.Time{}, false
	}
	start, err = time.Parse(time.RFC3339, startStr)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	end, err = time.Parse(time.RFC3339, endStr)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

func trainJobDurationFromConditions(u *unstructured.Unstructured) (time.Duration, bool) {
	conditions, found, err := unstructured.NestedSlice(u.Object, "status", "conditions")
	if err != nil || !found {
		return 0, false
	}

	var start, end time.Time
	for _, item := range conditions {
		cond, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := cond["type"].(string)
		status, _ := cond["status"].(string)
		ts := parseConditionTime(cond["lastTransitionTime"])
		if ts.IsZero() {
			continue
		}

		if start.IsZero() || ts.Before(start) {
			start = ts
		}

		if strings.EqualFold(status, "true") {
			switch typ {
			case trainJobCompleteType, trainJobFailedType, "Succeeded":
				if end.IsZero() || ts.After(end) {
					end = ts
				}
			}
		}
	}
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return 0, false
	}
	return end.Sub(start), true
}

func parseConditionTime(v interface{}) time.Time {
	switch t := v.(type) {
	case string:
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return time.Time{}
		}
		return parsed
	default:
		return time.Time{}
	}
}
