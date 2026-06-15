package kubefisher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestQuotaRowFromUnstructured(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "quota.kubefisher.io/v1alpha1",
		"kind":       "TeamInferenceQuota",
		"metadata": map[string]any{
			"name":      "q1",
			"namespace": "team-a",
		},
		"spec": map[string]any{
			"dailyTokenBudget":    int64(1000),
			"monthlyCostLimitUSD": "500.00",
			"enforcementMode":     "Enforce",
			"alertThresholdPct":   float64(80),
		},
		"status": map[string]any{
			"phase":               "Active",
			"tokensUsedToday":     float64(42),
			"costUsedThisMonth":   "$1.23",
			"tokenBudgetRemainingPct": float64(95),
		},
	}}
	r := quotaRowFromUnstructured(u)
	assert.Equal(t, "team-a", r.Namespace)
	assert.Equal(t, "q1", r.Name)
	assert.Equal(t, "Active", r.Phase)
	assert.Equal(t, int64(42), r.TokensUsed)
	assert.Equal(t, int64(1000), r.DailyBudget)
	assert.Equal(t, "$1.23", r.CostUsed)
	assert.Equal(t, "500.00", r.MonthlyLimit)
	assert.Equal(t, "Enforce", r.Mode)
	assert.Equal(t, int32(80), r.AlertThreshold)
}

func TestFormatPhaseNoColorWhenDisabled(t *testing.T) {
	assert.Equal(t, "Exceeded", formatPhase("Exceeded", false))
}
