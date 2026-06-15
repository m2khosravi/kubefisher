package cost

// CostRow is one GPU workload row for `fisher cost` table/JSON output.
type CostRow struct {
	Namespace           string   `json:"namespace"`
	Name                string   `json:"name"`
	Platform            string   `json:"platform"`
	WorkloadType        string   `json:"workloadType"`
	WorkloadKind        string   `json:"workloadKind"`
	GPUCount            int      `json:"gpuCount"`
	Replicas            int32    `json:"replicas"`
	CostPerHour         *float64 `json:"costPerHour,omitempty"`
	CostPerHourTotal    *float64 `json:"costPerHourTotal,omitempty"`
	CostPerToken        *float64 `json:"costPerToken,omitempty"`
	LastUpdated         string   `json:"lastUpdated,omitempty"`
}

// MonthlyEst returns 720 × fleet-total cost/hr (falls back to per-replica when total is absent).
func (r CostRow) MonthlyEst() float64 {
	if r.CostPerHourTotal != nil {
		return *r.CostPerHourTotal * 720
	}
	if r.CostPerHour != nil {
		return *r.CostPerHour * 720
	}
	return 0
}
