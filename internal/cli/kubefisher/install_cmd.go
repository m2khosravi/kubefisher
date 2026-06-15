package kubefisher

import (
	"github.com/m2khosravi/kubefisher/internal/install"
	"github.com/spf13/cobra"
)

func newInstallCommand(g *GlobalFlags) *cobra.Command {
	var (
		namespace      string
		dryRun         bool
		skipPrometheus bool
		chartPath      string
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install Prometheus, DCGM, and cost-patcher on any cluster",
		Long: `Install the KubeFisher observability stack on any Kubernetes cluster.

Steps performed:
  1. kube-prometheus-stack  — skipped if Prometheus is already present
  2. gpu-operator / DCGM    — only when GPU nodes are detected and DCGM is absent
  3. kubefisher chart      — cost-patcher + operator (skipped if already installed)
     embedded ConfigMap and PrometheusRule applied via server-side apply

The install is idempotent: running twice does not duplicate or break anything.
Works on clusters with KServe, BentoML, Ray, or none of them installed.

Requires helm in PATH.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if namespace == "" {
				namespace = "kubefisher-system"
			}

			cc := NewClientConfig(g.Kubeconfig, g.Context)

			cl, err := BuildControllerRuntimeClient(cc)
			if err != nil {
				return err
			}

			disc, err := NewDiscoveryClient(cc)
			if err != nil {
				return err
			}

			opts := install.Options{
				Namespace:      namespace,
				DryRun:         dryRun,
				SkipPrometheus: skipPrometheus,
				ChartPath:      chartPath,
			}
			return install.Run(cmd.Context(), opts, cl, disc)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "",
		"Namespace for the kubefisher chart (default: kubefisher-system)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Print what would be installed without applying any changes")
	cmd.Flags().BoolVar(&skipPrometheus, "skip-prometheus", false,
		"Skip kube-prometheus-stack installation even if Prometheus is absent")
	cmd.Flags().StringVar(&chartPath, "chart-path", "",
		"Path or OCI reference for the kubefisher chart (default: ./charts/kubefisher)")

	return cmd
}
