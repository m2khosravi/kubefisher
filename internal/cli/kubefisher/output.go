package kubefisher

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/olekukonko/tablewriter"
	"gopkg.in/yaml.v3"
)

// OutputFormat is --output value.
type OutputFormat string

const (
	OutputTable OutputFormat = "table"
	OutputJSON  OutputFormat = "json"
	OutputYAML  OutputFormat = "yaml"
)

// RenderQuotaTable writes kubectl-style columns for TeamInferenceQuota.
func RenderQuotaTable(w io.Writer, rows []QuotaRow, colorize bool) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "No quotas found. Run: kubefisher quota set --help")
		return
	}

	tw := tablewriter.NewWriter(w)
	tw.SetHeader([]string{"NAMESPACE", "NAME", "PHASE", "TOKENS-USED", "BUDGET", "TOKEN-REM", "COST-USED", "LIMIT", "COST-REM", "MODE", "AGE"})
	tw.SetBorder(false)
	tw.SetColumnSeparator(" ")
	tw.SetHeaderLine(false)
	tw.SetAutoWrapText(false)

	for _, r := range rows {
		tw.Append([]string{
			r.Namespace,
			r.Name,
			formatPhase(r.Phase, colorize),
			fmt.Sprintf("%d", r.TokensUsed),
			fmt.Sprintf("%d", r.DailyBudget),
			renderProgressBar(int(r.TokenRemainingPct)),
			r.CostUsed,
			r.MonthlyLimit,
			renderProgressBar(int(r.CostRemainingPct)),
			r.Mode,
			r.Age,
		})
	}
	tw.Render()
}

// RenderQuotaListJSON emits {"apiVersion":"...", "kind":"List", "items":[QuotaRow...]} style for readability.
func RenderQuotaListJSON(w io.Writer, rows []QuotaRow) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"kind":       "TeamInferenceQuotaList",
		"apiVersion": "quota.kubefisher.io/v1alpha1",
		"items":      rows,
	})
}

// RenderQuotaListYAML emits the same logical list as YAML.
func RenderQuotaListYAML(w io.Writer, rows []QuotaRow) error {
	enc := yaml.NewEncoder(w)
	defer enc.Close()
	return enc.Encode(map[string]any{
		"kind":       "TeamInferenceQuotaList",
		"apiVersion": "quota.kubefisher.io/v1alpha1",
		"items":      rows,
	})
}

// StdoutIsTerminal returns whether stdout is a character device.
func StdoutIsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
