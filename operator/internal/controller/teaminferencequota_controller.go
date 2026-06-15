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

package controller

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/m2khosravi/kubefisher/pkg/promclient"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	quotav1alpha1 "github.com/m2khosravi/kubefisher/operator/api/v1alpha1"
)

const (
	requeueInterval = 60 * time.Second

	conditionSpecValid        = "SpecValid"
	conditionPrometheusReach  = "PrometheusReachable"
	reasonInvalidSpec         = "InvalidMonthlyCostLimitUSD"
	reasonPrometheusError     = "PrometheusQueryFailed"
	reasonPrometheusReachable = "PrometheusReachable"
	reasonAuditWarning        = "AuditWarning"
	reasonAuditExceeded       = "AuditExceeded"
)

// TeamInferenceQuotaReconciler reconciles a TeamInferenceQuota object.
type TeamInferenceQuotaReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Prom     *promclient.Client
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=quota.kubefisher.io,resources=teaminferencequotas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=quota.kubefisher.io,resources=teaminferencequotas/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=quota.kubefisher.io,resources=teaminferencequotas/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile evaluates Prometheus spend and patches status.
func (r *TeamInferenceQuotaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("namespace", req.Namespace, "teaminferencequota", req.Name)

	var quota quotav1alpha1.TeamInferenceQuota
	if err := r.Get(ctx, req.NamespacedName, &quota); err != nil {
		if client.IgnoreNotFound(err) == nil {
			deleteTIQMetrics(req.Namespace, req.Name)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	base := quota.DeepCopy()
	prevPhase := base.Status.Phase

	now := time.Now().UTC()
	nextReset := metav1.NewTime(firstOfNextMonthUTC(now))
	quota.Status.NextResetTime = &nextReset

	monthlyLimit, specErr := parseMonthlyCostLimitUSD(quota.Spec.MonthlyCostLimitUSD)
	if specErr != nil {
		setSpecCondition(&quota, metav1.ConditionFalse, reasonInvalidSpec, specErr.Error())
		setPrometheusCondition(&quota, metav1.ConditionUnknown, reasonPrometheusError, "Skipped Prometheus: invalid spec.")
		applyUnknownStatus(&quota, now, "0.00")
		emitPhaseEventIfNeeded(r.Recorder, &quota, prevPhase, quota.Status.Phase, quota.Status.CostUsedThisMonth, quota.Status.NextResetTime)
		if patchErr := r.patchStatus(ctx, base, &quota); patchErr != nil {
			return ctrl.Result{RequeueAfter: requeueInterval}, fmt.Errorf("patch teaminferencequota status: %w", patchErr)
		}
		updateTIQMetrics(&quota)
		logger.Info("reconcile blocked on invalid spec", "reason", reasonInvalidSpec)
		return ctrl.Result{RequeueAfter: requeueInterval}, nil
	}
	setSpecCondition(&quota, metav1.ConditionTrue, "SpecValid", "MonthlyCostLimitUSD is parseable.")

	if r.Prom == nil {
		errMsg := "Prometheus client is not configured"
		setPrometheusCondition(&quota, metav1.ConditionFalse, reasonPrometheusError, errMsg)
		applyUnknownStatus(&quota, now, "0.00")
		emitPhaseEventIfNeeded(r.Recorder, &quota, prevPhase, quota.Status.Phase, quota.Status.CostUsedThisMonth, quota.Status.NextResetTime)
		if patchErr := r.patchStatus(ctx, base, &quota); patchErr != nil {
			return ctrl.Result{RequeueAfter: requeueInterval}, fmt.Errorf("patch teaminferencequota status: %w", patchErr)
		}
		updateTIQMetrics(&quota)
		logger.Info("reconcile waiting for prometheus client", "reason", reasonPrometheusError)
		return ctrl.Result{RequeueAfter: requeueInterval}, nil
	}

	ns := quota.Namespace
	// Prefer generation_tokens_total (vLLM / repo mocks and recording rules); OR num_generation for older scrapes.
	tokQuery := fmt.Sprintf(
		`sum(increase(vllm:generation_tokens_total{namespace=%q}[24h])) or sum(increase(vllm:num_generation_tokens_total{namespace=%q}[24h]))`,
		ns, ns)
	hours := hoursSinceMonthStartUTC(now)
	costQuery := fmt.Sprintf(`sum(kubefisher:cost_per_hour{namespace=%q}) / 3600 * %g`, ns, hours)

	var tokenVal float64
	tv, tokOK, tokErr := r.Prom.QueryInstant(ctx, tokQuery)
	promErr := tokErr != nil
	if !promErr {
		if tokOK {
			tokenVal = tv
		}
	}

	var cost float64
	if v, ok, err := r.Prom.QueryInstant(ctx, costQuery); err != nil {
		promErr = true
	} else if ok {
		cost = v
	}

	if promErr {
		setPrometheusCondition(&quota, metav1.ConditionFalse, reasonPrometheusError, "One or more Prometheus instant queries failed.")
		applyUnknownStatus(&quota, now, "$0.00")
	} else {
		setPrometheusCondition(&quota, metav1.ConditionTrue, reasonPrometheusReachable, "Prometheus queries succeeded.")
		tokensUsed := int64(math.Round(tokenVal))
		quota.Status.TokensUsedToday = tokensUsed
		quota.Status.CostUsedThisMonth = formatUSD(cost)
		quota.Status.TokenBudgetRemainingPct = tokenBudgetRemainingPct(float64(tokensUsed), quota.Spec.DailyTokenBudget)
		quota.Status.CostBudgetRemainingPct = costBudgetRemainingPct(cost, monthlyLimit)
		last := metav1.NewTime(now)
		quota.Status.LastEvaluationTime = &last

		phaseIn := phaseInputs{
			PrometheusError: false,
			TokensUsed:      tokenVal,
			CostUsedUSD:     cost,
			DailyTokenLimit: quota.Spec.DailyTokenBudget,
			MonthlyLimitUSD: monthlyLimit,
			AlertThreshold:  effectiveAlertThreshold(quota.Spec.AlertThresholdPct),
		}
		quota.Status.Phase = computePhase(phaseIn)
	}

	emitPhaseEventIfNeeded(r.Recorder, &quota, prevPhase, quota.Status.Phase, quota.Status.CostUsedThisMonth, quota.Status.NextResetTime)
	emitAuditEventIfNeeded(r.Recorder, &quota, prevPhase, quota.Status.Phase, quota.Spec.MonthlyCostLimitUSD, quota.Status.NextResetTime)

	if patchErr := r.patchStatus(ctx, base, &quota); patchErr != nil {
		return ctrl.Result{RequeueAfter: requeueInterval}, fmt.Errorf("patch teaminferencequota status: %w", patchErr)
	}
	updateTIQMetrics(&quota)

	logger.Info("reconciled team inference quota",
		"phase", quota.Status.Phase,
		"tokensUsedToday", quota.Status.TokensUsedToday,
		"costUsedThisMonth", quota.Status.CostUsedThisMonth,
	)
	return ctrl.Result{RequeueAfter: requeueInterval}, nil
}

func (r *TeamInferenceQuotaReconciler) patchStatus(ctx context.Context, base, quota *quotav1alpha1.TeamInferenceQuota) error {
	if equality.Semantic.DeepEqual(base.Status, quota.Status) {
		return nil
	}
	patch := client.MergeFrom(base)
	if err := r.Status().Patch(ctx, quota, patch); err != nil {
		return err
	}
	return nil
}

func applyUnknownStatus(quota *quotav1alpha1.TeamInferenceQuota, now time.Time, costDisplay string) {
	quota.Status.Phase = quotav1alpha1.QuotaPhaseUnknown
	quota.Status.TokensUsedToday = 0
	quota.Status.CostUsedThisMonth = costDisplay
	quota.Status.TokenBudgetRemainingPct = 0
	quota.Status.CostBudgetRemainingPct = 0
	last := metav1.NewTime(now)
	quota.Status.LastEvaluationTime = &last
}

func setSpecCondition(quota *quotav1alpha1.TeamInferenceQuota, status metav1.ConditionStatus, reason, message string) {
	c := metav1.Condition{
		Type:               conditionSpecValid,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: quota.Generation,
	}
	apimeta.SetStatusCondition(&quota.Status.Conditions, c)
}

func setPrometheusCondition(quota *quotav1alpha1.TeamInferenceQuota, status metav1.ConditionStatus, reason, message string) {
	c := metav1.Condition{
		Type:               conditionPrometheusReach,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: quota.Generation,
	}
	apimeta.SetStatusCondition(&quota.Status.Conditions, c)
}

func parseMonthlyCostLimitUSD(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("monthlyCostLimitUSD is empty")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse monthlyCostLimitUSD: %w", err)
	}
	if v < 0 {
		return 0, fmt.Errorf("monthlyCostLimitUSD must be non-negative")
	}
	return v, nil
}

func hoursSinceMonthStartUTC(now time.Time) float64 {
	u := now.UTC()
	y, m, _ := u.Date()
	start := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	return u.Sub(start).Hours()
}

func firstOfNextMonthUTC(now time.Time) time.Time {
	u := now.UTC()
	y, m, _ := u.Date()
	firstThis := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	return firstThis.AddDate(0, 1, 0)
}

func formatUSD(v float64) string {
	return fmt.Sprintf("$%.2f", v)
}

func emitPhaseEventIfNeeded(rec record.EventRecorder, quota *quotav1alpha1.TeamInferenceQuota, prev, cur quotav1alpha1.QuotaPhase, costUsed string, nextReset *metav1.Time) {
	if rec == nil {
		return
	}
	if prev == cur {
		return
	}
	if prev == "" && cur == quotav1alpha1.QuotaPhaseActive {
		return
	}
	resetStr := "unknown"
	if nextReset != nil {
		resetStr = nextReset.Format(time.RFC3339)
	}
	msg := fmt.Sprintf("phase %s -> %s; costUsedThisMonth=%s; tokensUsedToday=%d; nextMonthlyReset=%s",
		prev, cur, costUsed, quota.Status.TokensUsedToday, resetStr)

	switch cur {
	case quotav1alpha1.QuotaPhaseExceeded, quotav1alpha1.QuotaPhaseUnknown:
		rec.Event(quota, corev1.EventTypeWarning, string(cur), msg)
	default:
		rec.Event(quota, corev1.EventTypeNormal, string(cur), msg)
	}
}

// emitAuditEventIfNeeded fires a Normal Kubernetes Event with spend details when
// enforcementMode is Audit and the phase transitions to Warning or Exceeded.
// Nothing is blocked; the event serves as an observability record.
func emitAuditEventIfNeeded(
	rec record.EventRecorder,
	quota *quotav1alpha1.TeamInferenceQuota,
	prevPhase, curPhase quotav1alpha1.QuotaPhase,
	monthlyLimit string,
	nextReset *metav1.Time,
) {
	if rec == nil {
		return
	}
	if quota.Spec.EnforcementMode != quotav1alpha1.EnforcementModeAudit {
		return
	}
	if prevPhase == curPhase {
		return
	}
	if curPhase != quotav1alpha1.QuotaPhaseWarning && curPhase != quotav1alpha1.QuotaPhaseExceeded {
		return
	}

	resetStr := "unknown"
	if nextReset != nil {
		resetStr = nextReset.Format(time.RFC3339)
	}

	reason := reasonAuditWarning
	if curPhase == quotav1alpha1.QuotaPhaseExceeded {
		reason = reasonAuditExceeded
	}

	msg := fmt.Sprintf(
		"[Audit] Quota limit reached; pod allowed (not blocked); "+
			"phase=%s; costUsedThisMonth=%s; monthlyCostLimitUSD=%s; "+
			"tokensUsedToday=%d; dailyTokenBudget=%d; nextMonthlyReset=%s",
		curPhase,
		quota.Status.CostUsedThisMonth,
		monthlyLimit,
		quota.Status.TokensUsedToday,
		quota.Spec.DailyTokenBudget,
		resetStr,
	)
	rec.Event(quota, corev1.EventTypeNormal, reason, msg)
}

// SetupWithManager sets up the controller with the Manager.
func (r *TeamInferenceQuotaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&quotav1alpha1.TeamInferenceQuota{}).
		Named("teaminferencequota").
		Complete(r)
}
