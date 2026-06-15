package kubefisher

import (
	"log/slog"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// Execute runs the fisher root command.
func Execute() error {
	g := &GlobalFlags{}

	root := &cobra.Command{
		Use:   "kubefisher",
		Short: "KubeFisher CLI — quotas and cluster helpers",
		Long: `kubefisher is the command-line interface for KubeFisher.

It uses the same kubeconfig resolution as kubectl (KUBECONFIG, ~/.kube/config, current context).

Subcommands:
  install  Install Prometheus, DCGM, and cost-patcher on any cluster (idempotent).
  version  Show binary version and operator version installed in cluster.
  cost     GPU workload cost table (cost/hr, cost/token) across platforms.
  deploy   Deploy a GPU model (KServe InferenceService or plain vLLM Deployment).
  status   Show Phase, Endpoint, cost/hr for a GPU workload by name.
  logs     Stream logs from pods backing a GPU workload.
  quota    TeamInferenceQuota (tiq) — list, get, and set with table, JSON, or YAML output.

Full CLI reference: docs/cli.md`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			initLogging(g.LogFormat)
			if !StdoutIsTerminal() {
				color.NoColor = true
			}
			return nil
		},
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVar(&g.Kubeconfig, "kubeconfig", "",
		"Path to kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
	root.PersistentFlags().StringVar(&g.Context, "context", "", "Kube context name (default: current context)")
	root.PersistentFlags().StringVarP(&g.Namespace, "namespace", "n", "", "Namespace (default: current context namespace)")
	root.PersistentFlags().BoolVarP(&g.AllNamespaces, "all-namespaces", "A", false, "List TeamInferenceQuota in all namespaces")
	root.PersistentFlags().StringVarP(&g.Output, "output", "o", "table", "Output format: table, json, yaml")
	root.PersistentFlags().StringVar(&g.LogFormat, "log-format", envOrDefault("LOG_FORMAT", "text"),
		"log format: json or text (default: text; env LOG_FORMAT)")

	root.AddCommand(newQuotaCommand(g))
	root.AddCommand(newInstallCommand(g))
	root.AddCommand(newVersionCommand(g))
	root.AddCommand(newCostCommand(g))
	root.AddCommand(newDeployCommand(g))
	root.AddCommand(newStatusCommand(g))
	root.AddCommand(newLogsCommand(g))

	return root.Execute()
}

func initLogging(format string) {
	var h slog.Handler
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	switch format {
	case "json":
		h = slog.NewJSONHandler(os.Stderr, opts)
	default:
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

