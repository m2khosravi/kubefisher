package kubefisher

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/m2khosravi/kubefisher/internal/workload"
	"github.com/spf13/cobra"
)

func newStatusCommand(g *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status NAME",
		Short: "Show status for a GPU workload by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), g, args[0])
		},
	}
}

func runStatus(ctx context.Context, g *GlobalFlags, name string) error {
	cc := NewClientConfig(g.Kubeconfig, g.Context)
	ns, err := ResolveNamespace(*g, cc)
	if err != nil {
		return err
	}
	cl, err := BuildControllerRuntimeClient(cc)
	if err != nil {
		return err
	}
	dyn, err := NewDynamicClient(cc)
	if err != nil {
		return err
	}
	disc, err := NewDiscoveryClient(cc)
	if err != nil {
		return err
	}

	st, err := workload.FindResource(ctx, name, ns, cl, dyn, disc)
	if err != nil {
		return err
	}
	workload.ResolveDeploymentEndpoint(ctx, cl, st)

	colorize := StdoutIsTerminal()
	printStatus(os.Stdout, st, colorize)
	return nil
}

func printStatus(w io.Writer, st *workload.WorkloadStatus, colorize bool) {
	phase := formatWorkloadPhase(st.Phase, colorize)
	fmt.Fprintf(w, "Name:          %s\n", st.Name)
	fmt.Fprintf(w, "Namespace:     %s\n", st.Namespace)
	fmt.Fprintf(w, "Kind:          %s\n", st.Kind)
	fmt.Fprintf(w, "Platform:      %s\n", st.Platform)
	fmt.Fprintf(w, "WorkloadType:  %s\n", st.WorkloadType)
	fmt.Fprintf(w, "Phase:         %s\n", phase)
	fmt.Fprintf(w, "Replicas:      %s\n", st.Replicas)
	fmt.Fprintf(w, "Endpoint:      %s\n", dashIfEmpty(st.Endpoint))
	fmt.Fprintf(w, "Cost/hr:       %s\n", st.CostPerHour)
	fmt.Fprintf(w, "Cost/token:    %s\n", st.CostPerToken)
	fmt.Fprintf(w, "Last-updated:  %s\n", st.LastUpdated)
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" || s == "—" {
		return "—"
	}
	return s
}

func formatWorkloadPhase(phase string, colorize bool) string {
	if !colorize {
		return phase
	}
	switch strings.ToLower(phase) {
	case "ready", "available", "running", "succeeded":
		return color.New(color.FgGreen).Sprint(phase)
	case "progressing", "pending":
		return color.New(color.FgYellow).Sprint(phase)
	default:
		return color.New(color.FgRed).Sprint(phase)
	}
}
