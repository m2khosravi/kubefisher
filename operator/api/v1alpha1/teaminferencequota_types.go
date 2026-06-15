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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EnforcementMode defines whether quota limits are enforced at admission or observed only.
// +kubebuilder:validation:Enum=Enforce;Audit
type EnforcementMode string

const (
	EnforcementModeEnforce EnforcementMode = "Enforce"
	EnforcementModeAudit   EnforcementMode = "Audit"
)

// QuotaPhase reflects budget utilization relative to configured limits.
// +kubebuilder:validation:Enum=Active;Warning;Exceeded;Unknown
type QuotaPhase string

const (
	QuotaPhaseActive   QuotaPhase = "Active"
	QuotaPhaseWarning  QuotaPhase = "Warning"
	QuotaPhaseExceeded QuotaPhase = "Exceeded"
	QuotaPhaseUnknown  QuotaPhase = "Unknown"
)

// TeamInferenceQuotaSpec defines token and cost budgets for the quota object's namespace.
type TeamInferenceQuotaSpec struct {
	// DailyTokenBudget is the maximum number of generation tokens (rolling 24h window from Prometheus) allowed per day.
	// +kubebuilder:validation:Minimum=0
	DailyTokenBudget int64 `json:"dailyTokenBudget"`

	// MonthlyCostLimitUSD is a decimal USD string (e.g. "500.00") for the calendar month (UTC).
	// +kubebuilder:validation:Pattern=`^([0-9]+(\.[0-9]+)?)$`
	MonthlyCostLimitUSD string `json:"monthlyCostLimitUSD"`

	// AlertThresholdPct is the utilization percentage at or above which phase becomes Warning (until exceeded).
	// +optional
	// +kubebuilder:default:=80
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	AlertThresholdPct *int32 `json:"alertThresholdPct,omitempty"`

	// EnforcementMode selects Enforce (deny new GPU pods at admission when Exceeded) vs Audit (allow + events only).
	// +optional
	// +kubebuilder:default:=Enforce
	EnforcementMode EnforcementMode `json:"enforcementMode,omitempty"`
}

// TeamInferenceQuotaStatus defines observed spend and phase.
type TeamInferenceQuotaStatus struct {
	// Phase is the high-level quota state.
	// +optional
	Phase QuotaPhase `json:"phase,omitempty"`

	// TokensUsedToday is the rolling 24h token total from Prometheus (not calendar midnight).
	// Always serialized (including zero) so UIs and kubectl print columns can show the value.
	TokensUsedToday int64 `json:"tokensUsedToday"`

	// CostUsedThisMonth is formatted USD spent this calendar month (UTC), derived from cost_per_hour.
	// +optional
	CostUsedThisMonth string `json:"costUsedThisMonth,omitempty"`

	// TokenBudgetRemainingPct is 100 minus consumed percent of the daily token budget (0 if budget is 0).
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	TokenBudgetRemainingPct int32 `json:"tokenBudgetRemainingPct,omitempty"`

	// CostBudgetRemainingPct is 100 minus consumed percent of the monthly USD budget.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	CostBudgetRemainingPct int32 `json:"costBudgetRemainingPct,omitempty"`

	// LastEvaluationTime is the last reconcile evaluation time (including failed Prometheus queries).
	// +optional
	LastEvaluationTime *metav1.Time `json:"lastEvaluationTime,omitempty"`

	// NextResetTime is the start of the next calendar month (UTC), used for cost budget messaging.
	// +optional
	NextResetTime *metav1.Time `json:"nextResetTime,omitempty"`

	// Conditions represent the state of the quota resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=tiq
// +kubebuilder:printcolumn:name="NAMESPACE",type="string",JSONPath=".metadata.namespace",description="Budgeted namespace"
// +kubebuilder:printcolumn:name="PHASE",type="string",JSONPath=".status.phase",description="Quota phase"
// +kubebuilder:printcolumn:name="TOKENS-USED",type="integer",JSONPath=".status.tokensUsedToday",description="Rolling 24h tokens used"
// +kubebuilder:printcolumn:name="BUDGET",type="integer",JSONPath=".spec.dailyTokenBudget",description="Daily token budget"
// +kubebuilder:printcolumn:name="COST-USED",type="string",JSONPath=".status.costUsedThisMonth",description="Month-to-date cost (USD)"
// +kubebuilder:printcolumn:name="LIMIT",type="string",JSONPath=".spec.monthlyCostLimitUSD",description="Monthly USD limit"
// +kubebuilder:printcolumn:name="MODE",type="string",JSONPath=".spec.enforcementMode",description="Enforcement mode"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// TeamInferenceQuota is the Schema for the teaminferencequotas API.
type TeamInferenceQuota struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec TeamInferenceQuotaSpec `json:"spec"`

	// +optional
	Status TeamInferenceQuotaStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TeamInferenceQuotaList contains a list of TeamInferenceQuota.
type TeamInferenceQuotaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TeamInferenceQuota `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TeamInferenceQuota{}, &TeamInferenceQuotaList{})
}
