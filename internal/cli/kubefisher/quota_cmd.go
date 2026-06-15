package kubefisher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func newQuotaCommand(g *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "quota",
		Aliases: []string{"tiq"},
		Short:   "Inspect and manage TeamInferenceQuota resources (tiq)",
	}
	cmd.AddCommand(newQuotaListCommand(g))
	cmd.AddCommand(newQuotaGetCommand(g))
	cmd.AddCommand(newQuotaSetCommand(g))
	return cmd
}

func newQuotaListCommand(g *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List TeamInferenceQuota objects",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuotaList(cmd.Context(), g)
		},
	}
}

func newQuotaGetCommand(g *GlobalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "get NAME",
		Short: "Get one TeamInferenceQuota by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuotaGet(cmd.Context(), g, args[0])
		},
	}
}

func runQuotaList(ctx context.Context, g *GlobalFlags) error {
	cc := NewClientConfig(g.Kubeconfig, g.Context)
	ns, err := ResolveNamespace(*g, cc)
	if err != nil {
		return err
	}
	dyn, err := NewDynamicClient(cc)
	if err != nil {
		return err
	}

	s := maybeSpinner()
	if s != nil {
		s.Start()
		defer s.Stop()
	}

	gvr := TeamInferenceQuotaGVR
	list, err := dyn.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list teaminferencequotas: %w", err)
	}

	rows := make([]QuotaRow, 0, len(list.Items))
	for i := range list.Items {
		rows = append(rows, quotaRowFromUnstructured(&list.Items[i]))
	}

	format := OutputFormat(g.Output)
	if format == "" {
		format = OutputTable
	}

	out := os.Stdout
	switch format {
	case OutputTable:
		colorize := StdoutIsTerminal()
		RenderQuotaTable(out, rows, colorize)
	case OutputJSON:
		return RenderQuotaListJSON(out, rows)
	case OutputYAML:
		return RenderQuotaListYAML(out, rows)
	default:
		return fmt.Errorf("unsupported --output %q (use table, json, yaml)", g.Output)
	}
	return nil
}

func runQuotaGet(ctx context.Context, g *GlobalFlags, name string) error {
	if g.AllNamespaces {
		return fmt.Errorf("--all-namespaces is not supported for get; use --namespace")
	}
	cc := NewClientConfig(g.Kubeconfig, g.Context)
	ns, err := ResolveNamespace(*g, cc)
	if err != nil {
		return err
	}
	dyn, err := NewDynamicClient(cc)
	if err != nil {
		return err
	}

	s := maybeSpinner()
	if s != nil {
		s.Start()
		defer s.Stop()
	}

	obj, err := dyn.Resource(TeamInferenceQuotaGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get teaminferencequota %s/%s: %w", ns, name, err)
	}

	format := OutputFormat(g.Output)
	if format == "" {
		format = OutputTable
	}

	out := os.Stdout
	switch format {
	case OutputTable:
		row := quotaRowFromUnstructured(obj)
		colorize := StdoutIsTerminal()
		RenderQuotaTable(out, []QuotaRow{row}, colorize)
	case OutputJSON:
		return writeUnstructuredJSON(out, obj)
	case OutputYAML:
		return writeUnstructuredYAML(out, obj)
	default:
		return fmt.Errorf("unsupported --output %q (use table, json, yaml)", g.Output)
	}
	return nil
}

func newQuotaSetCommand(g *GlobalFlags) *cobra.Command {
	var (
		name           string
		dailyTokens    int64
		monthlyCost    string
		mode           string
		alertThreshold int32
	)

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Create or update a TeamInferenceQuota (idempotent via Server-Side Apply)",
		Example: `  # Create a quota for team-a with 1 million daily tokens and $500/mo limit
  kubefisher quota set --name team-a -n team-a --daily-tokens 1000000 --monthly-cost 500.00

  # Switch to Audit mode
  kubefisher quota set --name team-a -n team-a --daily-tokens 1000000 --monthly-cost 500.00 --mode Audit`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runQuotaSet(cmd.Context(), g, name, dailyTokens, monthlyCost, mode, alertThreshold)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Name of the TeamInferenceQuota object (required)")
	cmd.Flags().Int64Var(&dailyTokens, "daily-tokens", 0, "Daily token budget (rolling 24 h window)")
	cmd.Flags().StringVar(&monthlyCost, "monthly-cost", "", "Monthly USD cost limit, e.g. \"500.00\" (required)")
	cmd.Flags().StringVar(&mode, "mode", "Enforce", "Enforcement mode: Enforce or Audit")
	cmd.Flags().Int32Var(&alertThreshold, "alert-threshold", 80, "Percentage utilisation at which phase becomes Warning")

	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("daily-tokens")
	_ = cmd.MarkFlagRequired("monthly-cost")

	return cmd
}

func runQuotaSet(ctx context.Context, g *GlobalFlags, name string, dailyTokens int64, monthlyCost, mode string, alertThreshold int32) error {
	if mode != "Enforce" && mode != "Audit" {
		return fmt.Errorf("--mode must be Enforce or Audit, got %q", mode)
	}

	cc := NewClientConfig(g.Kubeconfig, g.Context)
	ns, err := ResolveNamespace(*g, cc)
	if err != nil {
		return err
	}
	if ns == "" {
		return fmt.Errorf("--namespace is required for quota set")
	}

	dyn, err := NewDynamicClient(cc)
	if err != nil {
		return err
	}

	obj := map[string]any{
		"apiVersion": "quota.kubefisher.io/v1alpha1",
		"kind":       "TeamInferenceQuota",
		"metadata": map[string]any{
			"name":      name,
			"namespace": ns,
		},
		"spec": map[string]any{
			"dailyTokenBudget":    dailyTokens,
			"monthlyCostLimitUSD": monthlyCost,
			"enforcementMode":     mode,
			"alertThresholdPct":   alertThreshold,
		},
	}

	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("marshal TeamInferenceQuota: %w", err)
	}

	force := true
	_, err = dyn.Resource(TeamInferenceQuotaGVR).Namespace(ns).Patch(
		ctx, name, types.ApplyPatchType, data,
		metav1.PatchOptions{FieldManager: "kubefisher-cli", Force: &force},
	)
	if err != nil {
		return fmt.Errorf("apply TeamInferenceQuota %s/%s: %w", ns, name, err)
	}

	fmt.Printf("TeamInferenceQuota %s/%s applied.\n", ns, name)
	fmt.Printf("  daily-tokens : %d\n", dailyTokens)
	fmt.Printf("  monthly-cost : $%s\n", monthlyCost)
	fmt.Printf("  mode         : %s\n", mode)
	fmt.Printf("  alert-at     : %d%%\n", alertThreshold)
	fmt.Println("\nRun: fisher quota list to verify.")
	return nil
}

func maybeSpinner() *spinner.Spinner {
	if !StdoutIsTerminal() {
		return nil
	}
	s := spinner.New(spinner.CharSets[14], 120*time.Millisecond)
	s.Writer = os.Stderr
	return s
}

func writeUnstructuredJSON(w io.Writer, u *unstructured.Unstructured) error {
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   TeamInferenceQuotaGVR.Group,
		Version: TeamInferenceQuotaGVR.Version,
		Kind:    "TeamInferenceQuota",
	})
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(u.Object); err != nil {
		return err
	}
	return nil
}

func writeUnstructuredYAML(w io.Writer, u *unstructured.Unstructured) error {
	enc := yaml.NewEncoder(w)
	defer enc.Close()
	return enc.Encode(u.Object)
}
