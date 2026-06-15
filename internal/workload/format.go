package workload

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
)

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func formatCostPerHour(v string) string {
	if v == "" {
		return "—"
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
		return fmt.Sprintf("$%.2f/hr", f)
	}
	return v
}

func formatCostPerToken(v string) string {
	if v == "" {
		return "—"
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
		s := fmt.Sprintf("$%.8f", f)
		return strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return v
}

func populateCostFields(st *WorkloadStatus, annotations map[string]string) {
	if annotations == nil {
		st.CostPerHour = "—"
		st.CostPerToken = "—"
		st.LastUpdated = "—"
		return
	}
	st.CostPerHour = formatCostPerHour(annotations[contract.AnnCostPerHour])
	st.CostPerToken = formatCostPerToken(annotations[contract.AnnCostPerToken])
	st.LastUpdated = dashIfEmpty(annotations[contract.AnnLastUpdated])
}
