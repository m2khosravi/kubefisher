package cost

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
)

// RenderCostTable writes the cost table and footer to w.
func RenderCostTable(w io.Writer, rows []CostRow, colorize bool) {
	tw := tablewriter.NewWriter(w)
	tw.SetHeader([]string{"NAMESPACE", "NAME", "PLATFORM", "TYPE", "REPLICAS", "COST/HR (REPLICA)", "COST/HR (TOTAL)", "COST/TOKEN", "MONTHLY-EST"})
	tw.SetBorder(false)
	tw.SetColumnSeparator(" ")
	tw.SetHeaderLine(false)
	tw.SetAutoWrapText(false)

	var totalHour float64
	for _, r := range rows {
		// Sum fleet totals for footer; fall back to per-replica when total absent.
		if r.CostPerHourTotal != nil {
			totalHour += *r.CostPerHourTotal
		} else if r.CostPerHour != nil {
			totalHour += *r.CostPerHour
		}
		tw.Append([]string{
			r.Namespace,
			r.Name,
			r.Platform,
			r.WorkloadType,
			fmt.Sprintf("%d", r.Replicas),
			formatCostPerHour(r.CostPerHour, colorize),
			formatCostPerHour(r.CostPerHourTotal, colorize),
			formatCostPerToken(r.CostPerToken, colorize),
			formatMoney(r.MonthlyEst()),
		})
	}
	tw.Render()

	totalMonthly := totalHour * 720
	fmt.Fprintf(w, "\nTotal: %s/hr · Est. %s/mo\n",
		formatMoney(totalHour),
		formatMoney(totalMonthly),
	)
}

// RenderCostJSON writes rows as a JSON array.
func RenderCostJSON(w io.Writer, rows []CostRow) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func formatCostPerHour(v *float64, colorize bool) string {
	if v == nil {
		return dimOrPlain("—", colorize)
	}
	s := fmt.Sprintf("$%.2f/hr", *v)
	if !colorize {
		return s
	}
	switch {
	case *v > 5:
		return color.New(color.FgRed, color.Bold).Sprint(s)
	case *v > 2:
		return color.New(color.FgYellow).Sprint(s)
	default:
		return color.New(color.FgGreen).Sprint(s)
	}
}

func formatCostPerToken(v *float64, colorize bool) string {
	if v == nil {
		return dimOrPlain("—", colorize)
	}
	s := fmt.Sprintf("$%.8f", *v)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}

func formatMoney(v float64) string {
	if v >= 1000 {
		return fmt.Sprintf("$%s", formatWithCommas(v, 2))
	}
	return fmt.Sprintf("$%.2f", v)
}

func formatWithCommas(v float64, decimals int) string {
	s := fmt.Sprintf("%.*f", decimals, v)
	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]
	neg := strings.HasPrefix(intPart, "-")
	if neg {
		intPart = intPart[1:]
	}
	var b strings.Builder
	for i, ch := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	out := b.String()
	if neg {
		out = "-" + out
	}
	if len(parts) == 2 {
		return out + "." + parts[1]
	}
	return out
}

func dimOrPlain(s string, colorize bool) string {
	if !colorize {
		return s
	}
	return color.New(color.FgHiBlack).Sprint(s)
}
