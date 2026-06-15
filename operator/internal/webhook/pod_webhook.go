/*
Copyright 2026 KubeFisher.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	quotav1alpha1 "github.com/m2khosravi/kubefisher/operator/api/v1alpha1"
)

const (
	gpuResourceName       = "nvidia.com/gpu"
	reasonAuditGPUAllowed = "AuditGPUAllowed"
)

// +kubebuilder:webhook:path=/validate--v1-pod,mutating=false,failurePolicy=ignore,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=vpod.kb.io,admissionReviewVersions=v1

// PodGPUQuotaValidator validates Pod creation against TeamInferenceQuota spend.
type PodGPUQuotaValidator struct {
	client.Client
	Recorder record.EventRecorder
}

var _ admission.Validator[*corev1.Pod] = &PodGPUQuotaValidator{}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=quota.kubefisher.io,resources=teaminferencequotas,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// ValidateCreate enforces GPU pod admission when TeamInferenceQuota is Exceeded.
func (v *PodGPUQuotaValidator) ValidateCreate(ctx context.Context, pod *corev1.Pod) (admission.Warnings, error) {
	if !podRequestsGPU(pod) {
		return nil, nil
	}

	logger := log.FromContext(ctx).WithValues("namespace", pod.Namespace, "pod", pod.Name)

	var quotaList quotav1alpha1.TeamInferenceQuotaList
	if err := v.List(ctx, &quotaList, client.InNamespace(pod.Namespace)); err != nil {
		logger.Error(err, "Could not list TeamInferenceQuota; allowing GPU pod")
		return nil, nil
	}

	if len(quotaList.Items) == 0 {
		return nil, nil
	}

	for i := range quotaList.Items {
		quota := &quotaList.Items[i]
		switch quota.Status.Phase {
		case quotav1alpha1.QuotaPhaseUnknown:
			logger.Info("Enforcement suspended; Prometheus unreachable",
				"teaminferencequota", quota.Name,
				"phase", quota.Status.Phase,
			)
			return nil, nil
		case quotav1alpha1.QuotaPhaseActive, quotav1alpha1.QuotaPhaseWarning:
			continue
		case quotav1alpha1.QuotaPhaseExceeded:
			mode := quota.Spec.EnforcementMode
			if mode == "" {
				mode = quotav1alpha1.EnforcementModeEnforce
			}
			if mode == quotav1alpha1.EnforcementModeAudit {
				v.emitAuditGPUAllowedEvent(quota, pod.Name)
				continue
			}
			return nil, fmt.Errorf("%s", denyMessage(quota))
		default:
			continue
		}
	}

	return nil, nil
}

// ValidateUpdate allows all pod updates; running pods are never touched.
func (v *PodGPUQuotaValidator) ValidateUpdate(_ context.Context, _, _ *corev1.Pod) (admission.Warnings, error) {
	return nil, nil
}

// ValidateDelete allows all pod deletions.
func (v *PodGPUQuotaValidator) ValidateDelete(_ context.Context, _ *corev1.Pod) (admission.Warnings, error) {
	return nil, nil
}

// SetupWithManager registers the Pod validating webhook with the manager.
func (v *PodGPUQuotaValidator) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &corev1.Pod{}).
		WithValidator(v).
		Complete()
}

// podRequestsGPU reports whether any container requests or limits nvidia.com/gpu > 0.
func podRequestsGPU(pod *corev1.Pod) bool {
	if slices.ContainsFunc(pod.Spec.InitContainers, containerRequestsGPU) {
		return true
	}
	return slices.ContainsFunc(pod.Spec.Containers, containerRequestsGPU)
}

func containerRequestsGPU(c corev1.Container) bool {
	gpuName := corev1.ResourceName(gpuResourceName)
	for _, rl := range []corev1.ResourceList{c.Resources.Requests, c.Resources.Limits} {
		if rl == nil {
			continue
		}
		q, ok := rl[gpuName]
		if !ok {
			continue
		}
		if quantityGreaterThanZero(q) {
			return true
		}
	}
	return false
}

func quantityGreaterThanZero(q resource.Quantity) bool {
	return q.Cmp(resource.MustParse("0")) > 0
}

func denyMessage(quota *quotav1alpha1.TeamInferenceQuota) string {
	summary := spendSummary(quota)
	return fmt.Sprintf(
		"GPU pod admission denied: namespace %q exceeded TeamInferenceQuota %q (phase %s). %s To adjust limits, run: kubectl edit teaminferencequota %s -n %s",
		quota.Namespace,
		quota.Name,
		quota.Status.Phase,
		summary,
		quota.Name,
		quota.Namespace,
	)
}

// spendSummary formats token/cost utilization and next reset for denial and audit messages.
func spendSummary(quota *quotav1alpha1.TeamInferenceQuota) string {
	return fmt.Sprintf(
		"Tokens (rolling 24h): %s of %s daily budget. Cost (month-to-date, UTC): %s of %s monthly limit. Next reset: %s.",
		formatCount(quota.Status.TokensUsedToday),
		formatCount(quota.Spec.DailyTokenBudget),
		normalizeUSD(quota.Status.CostUsedThisMonth),
		normalizeUSD(quota.Spec.MonthlyCostLimitUSD),
		formatNextReset(quota.Status.NextResetTime),
	)
}

func normalizeUSD(amount string) string {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return "$0.00"
	}
	if strings.HasPrefix(amount, "$") {
		return amount
	}
	return "$" + amount
}

func formatCount(n int64) string {
	if n < 0 {
		n = 0
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func formatNextReset(t *metav1.Time) string {
	if t == nil || t.Time.IsZero() {
		return "unknown (controller has not computed next reset yet)"
	}
	utc := t.Time.UTC()
	return fmt.Sprintf("%s (%s UTC)", utc.Format("2006-01-02 15:04"), utc.Format(time.RFC3339))
}

func (v *PodGPUQuotaValidator) emitAuditGPUAllowedEvent(quota *quotav1alpha1.TeamInferenceQuota, podName string) {
	if v.Recorder == nil {
		return
	}
	msg := fmt.Sprintf(
		"[Audit] GPU pod %q allowed; quota would be exceeded in Enforce mode (namespace %q, TeamInferenceQuota %q). %s",
		podName,
		quota.Namespace,
		quota.Name,
		spendSummary(quota),
	)
	v.Recorder.Event(quota, corev1.EventTypeNormal, reasonAuditGPUAllowed, msg)
}
