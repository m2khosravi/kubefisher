package controller

import (
	"math"

	quotav1alpha1 "github.com/m2khosravi/kubefisher/operator/api/v1alpha1"
)

// phaseInputs are normalized values for phase and remaining-percent computation.
type phaseInputs struct {
	PrometheusError bool
	TokensUsed      float64
	CostUsedUSD     float64
	DailyTokenLimit int64
	MonthlyLimitUSD float64
	AlertThreshold  int32
}

func effectiveAlertThreshold(pct *int32) int32 {
	if pct == nil || *pct < 1 || *pct > 100 {
		return 80
	}
	return *pct
}

// computePhase returns phase using priority Exceeded > Warning > Active, or Unknown on Prometheus errors.
func computePhase(in phaseInputs) quotav1alpha1.QuotaPhase {
	if in.PrometheusError {
		return quotav1alpha1.QuotaPhaseUnknown
	}

	tokenExceeded := false
	switch {
	case in.DailyTokenLimit < 0:
		// invalid; treat as no exceed from tokens alone
	case in.DailyTokenLimit == 0:
		tokenExceeded = in.TokensUsed > 0
	default:
		tokenExceeded = in.TokensUsed >= float64(in.DailyTokenLimit)
	}

	costExceeded := in.MonthlyLimitUSD > 0 && in.CostUsedUSD >= in.MonthlyLimitUSD

	if tokenExceeded || costExceeded {
		return quotav1alpha1.QuotaPhaseExceeded
	}

	var tokUtil, costUtil float64
	if in.DailyTokenLimit > 0 {
		tokUtil = 100.0 * in.TokensUsed / float64(in.DailyTokenLimit)
	}
	if in.MonthlyLimitUSD > 0 {
		costUtil = 100.0 * in.CostUsedUSD / in.MonthlyLimitUSD
	}

	th := in.AlertThreshold
	if th < 1 || th > 100 {
		th = 80
	}
	if tokUtil >= float64(th) || costUtil >= float64(th) {
		return quotav1alpha1.QuotaPhaseWarning
	}
	return quotav1alpha1.QuotaPhaseActive
}

func tokenBudgetRemainingPct(tokensUsed float64, dailyLimit int64) int32 {
	if dailyLimit <= 0 {
		return 0
	}
	usedPct := 100.0 * tokensUsed / float64(dailyLimit)
	if usedPct < 0 {
		usedPct = 0
	}
	if usedPct > 100 {
		usedPct = 100
	}
	return clampInt32Round(100.0 - usedPct)
}

func costBudgetRemainingPct(costUsed, monthlyLimit float64) int32 {
	if monthlyLimit <= 0 {
		return 0
	}
	usedPct := 100.0 * costUsed / monthlyLimit
	if usedPct < 0 {
		usedPct = 0
	}
	if usedPct > 100 {
		usedPct = 100
	}
	return clampInt32Round(100.0 - usedPct)
}

func clampInt32Round(v float64) int32 {
	r := math.Round(v)
	if r < 0 {
		return 0
	}
	if r > 100 {
		return 100
	}
	return int32(r)
}
