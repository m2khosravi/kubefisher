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
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	quotav1alpha1 "github.com/m2khosravi/kubefisher/operator/api/v1alpha1"
)

var (
	// tiqMonthlyLimitUSD exposes spec.monthlyCostLimitUSD as a float so Grafana can
	// use it as the divisor in budget-utilization panels.
	tiqMonthlyLimitUSD = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubefisher_tiq_monthly_limit_usd",
			Help: "Monthly cost budget limit in USD from TeamInferenceQuota spec.monthlyCostLimitUSD.",
		},
		[]string{"namespace", "name"},
	)

	// tiqCostUsedUSD mirrors status.costUsedThisMonth as a float for easy ratio queries.
	tiqCostUsedUSD = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubefisher_tiq_cost_used_usd",
			Help: "Month-to-date USD cost spent, from TeamInferenceQuota status.costUsedThisMonth.",
		},
		[]string{"namespace", "name"},
	)

	// tiqCostBudgetUsedPct is 100 - status.costBudgetRemainingPct, giving the
	// percentage of the monthly budget already consumed (0-100).
	tiqCostBudgetUsedPct = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubefisher_tiq_cost_budget_used_pct",
			Help: "Percentage of the monthly cost budget consumed (100 - costBudgetRemainingPct). Use for bargauge threshold panels.",
		},
		[]string{"namespace", "name"},
	)

	// tiqPhase encodes status.phase as an integer for threshold-based alerting:
	// 0=Unknown, 1=Active, 2=Warning, 3=Exceeded.
	tiqPhase = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kubefisher_tiq_phase",
			Help: "Current TeamInferenceQuota phase encoded as integer: 0=Unknown, 1=Active, 2=Warning, 3=Exceeded.",
		},
		[]string{"namespace", "name"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		tiqMonthlyLimitUSD,
		tiqCostUsedUSD,
		tiqCostBudgetUsedPct,
		tiqPhase,
	)
}

// updateTIQMetrics sets all four TIQ gauge series from the reconciled object.
// Call this after every successful status patch.
func updateTIQMetrics(tiq *quotav1alpha1.TeamInferenceQuota) {
	ns := tiq.Namespace
	name := tiq.Name

	if limit, err := parseMonthlyCostLimitUSD(tiq.Spec.MonthlyCostLimitUSD); err == nil {
		tiqMonthlyLimitUSD.WithLabelValues(ns, name).Set(limit)
	}

	costUsed := parseCostUsedUSD(tiq.Status.CostUsedThisMonth)
	tiqCostUsedUSD.WithLabelValues(ns, name).Set(costUsed)

	usedPct := float64(100 - tiq.Status.CostBudgetRemainingPct)
	if usedPct < 0 {
		usedPct = 0
	}
	tiqCostBudgetUsedPct.WithLabelValues(ns, name).Set(usedPct)

	tiqPhase.WithLabelValues(ns, name).Set(phaseToFloat(tiq.Status.Phase))
}

// deleteTIQMetrics removes all gauge series for a deleted TeamInferenceQuota so
// stale values are not scraped after the object is gone.
func deleteTIQMetrics(namespace, name string) {
	labels := prometheus.Labels{"namespace": namespace, "name": name}
	tiqMonthlyLimitUSD.Delete(labels)
	tiqCostUsedUSD.Delete(labels)
	tiqCostBudgetUsedPct.Delete(labels)
	tiqPhase.Delete(labels)
}

// parseCostUsedUSD converts status.costUsedThisMonth (e.g. "$123.45") to float64.
// Returns 0 on any parse failure so gauges are always set.
func parseCostUsedUSD(s string) float64 {
	s = strings.TrimPrefix(strings.TrimSpace(s), "$")
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// phaseToFloat encodes QuotaPhase as a numeric value for Prometheus:
// 0=Unknown, 1=Active, 2=Warning, 3=Exceeded.
func phaseToFloat(p quotav1alpha1.QuotaPhase) float64 {
	switch p {
	case quotav1alpha1.QuotaPhaseActive:
		return 1
	case quotav1alpha1.QuotaPhaseWarning:
		return 2
	case quotav1alpha1.QuotaPhaseExceeded:
		return 3
	default: // Unknown or ""
		return 0
	}
}
