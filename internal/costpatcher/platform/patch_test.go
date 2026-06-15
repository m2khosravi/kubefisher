package platform

import (
	"testing"
	"time"

	"github.com/m2khosravi/kubefisher/internal/costpatcher/contract"
	"github.com/stretchr/testify/require"
)

func TestFormatCost(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{in: 0, want: "0"},
		{in: 3.5, want: "3.5"},
		{in: 3.0, want: "3"},
		{in: 0.00000123, want: "0.00000123"},
		{in: 1.234567890123, want: "1.234567890123"},
	}
	for _, tt := range tests {
		if got := formatCost(tt.in); got != tt.want {
			require.Equal(t, tt.want, got)
		}
	}
}

func TestShouldPatchCostAnnotations(t *testing.T) {
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	hour := 1.25
	token := 0.000002

	tests := []struct {
		name     string
		existing map[string]string
		res      CostResult
		want     bool
	}{
		{
			name:     "no_cost_values_no_patch",
			existing: map[string]string{},
			res:      CostResult{LastUpdatedAt: now},
			want:     false,
		},
		{
			name:     "missing_cost_per_hour_patch",
			existing: map[string]string{},
			res:      CostResult{CostPerHour: &hour, LastUpdatedAt: now},
			want:     true,
		},
		{
			name:     "same_cost_per_hour_no_patch",
			existing: map[string]string{contract.AnnCostPerHourPerReplica: "1.25", contract.AnnCostPerHour: "1.25"},
			res:      CostResult{CostPerHour: &hour, LastUpdatedAt: now},
			want:     false,
		},
		{
			name:     "changed_cost_per_hour_patch",
			existing: map[string]string{contract.AnnCostPerHourPerReplica: "1.2", contract.AnnCostPerHour: "1.2"},
			res:      CostResult{CostPerHour: &hour, LastUpdatedAt: now},
			want:     true,
		},
		{
			name:     "token_missing_patch",
			existing: map[string]string{contract.AnnCostPerHour: "1.25"},
			res:      CostResult{CostPerToken: &token, LastUpdatedAt: now},
			want:     true,
		},
		{
			name:     "token_same_no_patch",
			existing: map[string]string{contract.AnnCostPerToken: "0.000002"},
			res:      CostResult{CostPerToken: &token, LastUpdatedAt: now},
			want:     false,
		},
		{
			name:     "gpu_count_missing_patch",
			existing: map[string]string{},
			res:      CostResult{GPUCount: ptrInt64(2), LastUpdatedAt: now},
			want:     true,
		},
		{
			name:     "gpu_count_same_no_patch",
			existing: map[string]string{contract.AnnGPUCount: "2"},
			res:      CostResult{GPUCount: ptrInt64(2), LastUpdatedAt: now},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldPatchCostAnnotations(tt.existing, tt.res))
		})
	}
}

func ptrInt64(v int64) *int64 { return &v }
