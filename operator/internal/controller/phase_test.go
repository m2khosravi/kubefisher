package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"

	quotav1alpha1 "github.com/m2khosravi/kubefisher/operator/api/v1alpha1"
)

func TestComputePhase(t *testing.T) {
	alert := int32(80)
	tests := []struct {
		name string
		in   phaseInputs
		want quotav1alpha1.QuotaPhase
	}{
		{
			name: "prometheus_error",
			in:   phaseInputs{PrometheusError: true},
			want: quotav1alpha1.QuotaPhaseUnknown,
		},
		{
			name: "token_exceeded",
			in: phaseInputs{
				TokensUsed:      100,
				CostUsedUSD:     0,
				DailyTokenLimit: 100,
				MonthlyLimitUSD: 500,
				AlertThreshold:  alert,
			},
			want: quotav1alpha1.QuotaPhaseExceeded,
		},
		{
			name: "cost_exceeded",
			in: phaseInputs{
				TokensUsed:      0,
				CostUsedUSD:     500,
				DailyTokenLimit: 1000,
				MonthlyLimitUSD: 500,
				AlertThreshold:  alert,
			},
			want: quotav1alpha1.QuotaPhaseExceeded,
		},
		{
			name: "warning_tokens",
			in: phaseInputs{
				TokensUsed:      81,
				CostUsedUSD:     0,
				DailyTokenLimit: 100,
				MonthlyLimitUSD: 500,
				AlertThreshold:  alert,
			},
			want: quotav1alpha1.QuotaPhaseWarning,
		},
		{
			name: "warning_cost",
			in: phaseInputs{
				TokensUsed:      0,
				CostUsedUSD:     400,
				DailyTokenLimit: 10000,
				MonthlyLimitUSD: 500,
				AlertThreshold:  alert,
			},
			want: quotav1alpha1.QuotaPhaseWarning,
		},
		{
			name: "active",
			in: phaseInputs{
				TokensUsed:      10,
				CostUsedUSD:     10,
				DailyTokenLimit: 1000,
				MonthlyLimitUSD: 500,
				AlertThreshold:  alert,
			},
			want: quotav1alpha1.QuotaPhaseActive,
		},
		{
			name: "budget_zero_with_usage_exceeded",
			in: phaseInputs{
				TokensUsed:      1,
				CostUsedUSD:     0,
				DailyTokenLimit: 0,
				MonthlyLimitUSD: 100,
				AlertThreshold:  alert,
			},
			want: quotav1alpha1.QuotaPhaseExceeded,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, computePhase(tc.in))
		})
	}
}

func TestEffectiveAlertThreshold(t *testing.T) {
	v := int32(50)
	assert.Equal(t, int32(50), effectiveAlertThreshold(&v))
	assert.Equal(t, int32(80), effectiveAlertThreshold(nil))
	z := int32(0)
	assert.Equal(t, int32(80), effectiveAlertThreshold(&z))
}
