package kubefisher

import (
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// QuotaRow is a stable projection of TeamInferenceQuota for table/JSON output.
type QuotaRow struct {
	Namespace         string `json:"namespace"`
	Name              string `json:"name"`
	Phase             string `json:"phase"`
	TokensUsed        int64  `json:"tokensUsedToday"`
	DailyBudget       int64  `json:"dailyTokenBudget"`
	CostUsed          string `json:"costUsedThisMonth"`
	MonthlyLimit      string `json:"monthlyCostLimitUSD"`
	Mode              string `json:"enforcementMode"`
	Age               string `json:"age"`
	CreationTime      string `json:"creationTimestamp,omitempty"`
	AlertThreshold    int32  `json:"alertThresholdPct,omitempty"`
	TokenRemainingPct int32  `json:"tokenBudgetRemainingPct,omitempty"`
	CostRemainingPct  int32  `json:"costBudgetRemainingPct,omitempty"`
}

func quotaRowFromUnstructured(u *unstructured.Unstructured) QuotaRow {
	row := QuotaRow{
		Namespace: u.GetNamespace(),
		Name:      u.GetName(),
	}
	if ct := u.GetCreationTimestamp(); !ct.IsZero() {
		row.CreationTime = ct.UTC().Format(time.RFC3339)
		row.Age = durationHuman(time.Since(ct.Time))
	}
	if ph, ok, _ := unstructured.NestedString(u.Object, "status", "phase"); ok {
		row.Phase = ph
	}
	if v, ok, _ := unstructured.NestedFloat64(u.Object, "status", "tokensUsedToday"); ok {
		row.TokensUsed = int64(v)
	}
	if v, ok, _ := unstructured.NestedString(u.Object, "status", "costUsedThisMonth"); ok {
		row.CostUsed = v
	}
	if v, ok, _ := unstructured.NestedInt64(u.Object, "spec", "dailyTokenBudget"); ok {
		row.DailyBudget = v
	}
	if v, ok, _ := unstructured.NestedString(u.Object, "spec", "monthlyCostLimitUSD"); ok {
		row.MonthlyLimit = v
	}
	if v, ok, _ := unstructured.NestedString(u.Object, "spec", "enforcementMode"); ok {
		row.Mode = v
	}
	if v, ok, _ := unstructured.NestedFloat64(u.Object, "spec", "alertThresholdPct"); ok {
		row.AlertThreshold = int32(v)
	}
	if v, ok, _ := unstructured.NestedFloat64(u.Object, "status", "tokenBudgetRemainingPct"); ok {
		row.TokenRemainingPct = int32(v)
	}
	if v, ok, _ := unstructured.NestedFloat64(u.Object, "status", "costBudgetRemainingPct"); ok {
		row.CostRemainingPct = int32(v)
	}
	return row
}

// renderProgressBar returns a 10-char wide ASCII bar representing pct (0-100).
// Example: renderProgressBar(67) → "[█████░░░] 67%"
func renderProgressBar(pct int) string {
	const width = 8
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := (pct * width) / 100
	return fmt.Sprintf("[%s%s] %3d%%",
		strings.Repeat("█", filled),
		strings.Repeat("░", width-filled),
		pct)
}

func durationHuman(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// formatPhase applies semantic colors when colorize && stdout is a TTY.
func formatPhase(phase string, colorize bool) string {
	if !colorize || phase == "" {
		return phase
	}
	switch phase {
	case "Exceeded":
		return color.New(color.FgRed, color.Bold).Sprint(phase)
	case "Warning":
		return color.New(color.FgYellow).Sprint(phase)
	case "Active":
		return color.New(color.FgGreen).Sprint(phase)
	case "Unknown":
		return color.New(color.FgHiBlack).Sprint(phase)
	default:
		return phase
	}
}
