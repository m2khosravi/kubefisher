package kubefisher

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/m2khosravi/kubefisher/internal/cost"
	"github.com/spf13/cobra"
)

func newCostCommand(g *GlobalFlags) *cobra.Command {
	var watch bool

	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Show cost/hr and cost/token for GPU workloads",
		Long: `List GPU workloads and their cost annotations (written by cost-patcher).

Includes Deployments, StatefulSets, and platform CRDs (KServe, BentoML, Kubeflow training, Ray) when installed.
Missing annotations are shown as — (not an error).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCost(cmd.Context(), g, watch)
		},
	}

	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "Refresh every 10s (clears screen between updates)")

	return cmd
}

func runCost(ctx context.Context, g *GlobalFlags, watch bool) error {
	cc := NewClientConfig(g.Kubeconfig, g.Context)
	ns, err := ResolveNamespace(*g, cc)
	if err != nil {
		return err
	}
	cl, err := BuildControllerRuntimeClient(cc)
	if err != nil {
		return err
	}
	disc, err := NewDiscoveryClient(cc)
	if err != nil {
		return err
	}
	dyn, err := NewDynamicClient(cc)
	if err != nil {
		return err
	}

	format := OutputFormat(g.Output)
	if format == "" {
		format = OutputTable
	}

	render := func() error {
		rows, err := cost.CollectCostRows(ctx, cl, dyn, disc, ns)
		if err != nil {
			return err
		}
		out := os.Stdout
		switch format {
		case OutputJSON:
			return cost.RenderCostJSON(out, rows)
		case OutputTable:
			colorize := StdoutIsTerminal()
			cost.RenderCostTable(out, rows, colorize)
			return nil
		default:
			return fmt.Errorf("unsupported --output %q for cost (use table or json)", g.Output)
		}
	}

	if !watch {
		s := maybeSpinner()
		if s != nil {
			s.Start()
			defer s.Stop()
		}
		return render()
	}

	if format != OutputTable {
		return fmt.Errorf("--watch is only supported with --output table")
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		if StdoutIsTerminal() {
			fmt.Fprint(os.Stdout, "\033[H\033[2J")
		}
		if err := render(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
