// Package install contains the pure business logic for `fisher install`.
// It has no dependency on cobra — the cobra wiring lives in
// internal/cli/kubefisher/install_cmd.go and calls Run().
package install

import (
	"context"
	"fmt"
	"log/slog"

	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultPrometheusRelease  = "prometheus"
	defaultPrometheusChart    = "kube-prometheus-stack"
	defaultPrometheusRepo     = "https://prometheus-community.github.io/helm-charts"
	defaultPrometheusNS       = "monitoring"
	defaultGPUOperatorRelease = "gpu-operator"
	defaultGPUOperatorChart   = "gpu-operator"
	defaultGPUOperatorRepo    = "https://helm.ngc.nvidia.com/nvidia"
	defaultGPUOperatorNS      = "gpu-operator"
)

// Options configures a single install run.
type Options struct {
	// Namespace is the target namespace for the kubefisher chart (cost-patcher + operator).
	Namespace string
	// DryRun prints what would happen without applying any changes.
	DryRun bool
	// SkipPrometheus skips Step 1 even when Prometheus is absent.
	SkipPrometheus bool
	// ChartPath is the local path or OCI reference for the kubefisher chart.
	// Defaults to "./charts/kubefisher".
	ChartPath string
}

// Run is the install entry point called by the cobra command.
// Steps:
//
//	0  Detect what is already installed → InstalledState
//	1  kube-prometheus-stack   (skipped if Prometheus present or --skip-prometheus)
//	2  gpu-operator            (skipped if no GPU nodes or DCGM already present)
//	3  kubefisher chart       (skipped if cost-patcher already present)
//	   + apply embedded ConfigMap and PrometheusRule via SSA
//	4  Print serving platform summary
func Run(ctx context.Context, opts Options, cl client.Client, disc discovery.DiscoveryInterface) error {
	if err := helmInPath(); err != nil {
		return err
	}

	if opts.ChartPath == "" {
		opts.ChartPath = "./charts/kubefisher"
	}

	// Step 0 — detect.
	slog.Info("Detecting installed components...")
	state := detectInstalled(ctx, cl, disc)

	// Step 1 — Prometheus.
	if err := stepPrometheus(ctx, opts, state); err != nil {
		return err
	}

	// Step 2 — GPU Operator / DCGM.
	if err := stepGPUOperator(ctx, opts, state); err != nil {
		return err
	}

	// Step 3 — kubefisher (cost-patcher + operator) + embedded assets.
	if err := stepKubeFisher(ctx, opts, state, cl); err != nil {
		return err
	}

	// Step 4 — serving platform summary.
	printSummary(state)
	return nil
}

func stepPrometheus(ctx context.Context, opts Options, state InstalledState) error {
	switch {
	case state.Prometheus:
		slog.Info("[Step 1/3] Prometheus already installed — skipping")
		return nil
	case opts.SkipPrometheus:
		slog.Info("[Step 1/3] Prometheus not found but --skip-prometheus set — skipping")
		return nil
	}

	slog.Info("[Step 1/3] Installing kube-prometheus-stack...", "dry_run", opts.DryRun)
	return HelmInstallIfAbsent(ctx, HelmOpts{
		Release:   defaultPrometheusRelease,
		Chart:     defaultPrometheusChart,
		Repo:      defaultPrometheusRepo,
		Namespace: defaultPrometheusNS,
		DryRun:    opts.DryRun,
	})
}

func stepGPUOperator(ctx context.Context, opts Options, state InstalledState) error {
	switch {
	case state.GPUNodeCount == 0:
		slog.Info("[Step 2/3] No GPU nodes detected — skipping gpu-operator")
		return nil
	case state.DCGM:
		slog.Info("[Step 2/3] DCGM already installed — skipping gpu-operator")
		return nil
	}

	slog.Info("[Step 2/3] GPU nodes detected, installing gpu-operator...",
		"gpu_nodes", state.GPUNodeCount, "dry_run", opts.DryRun)
	return HelmInstallIfAbsent(ctx, HelmOpts{
		Release:   defaultGPUOperatorRelease,
		Chart:     defaultGPUOperatorChart,
		Repo:      defaultGPUOperatorRepo,
		Namespace: defaultGPUOperatorNS,
		DryRun:    opts.DryRun,
	})
}

func stepKubeFisher(ctx context.Context, opts Options, state InstalledState, cl client.Client) error {
	if state.CostPatcher {
		slog.Info("[Step 3/3] Cost-patcher already installed — skipping Helm chart")
	} else {
		slog.Info("[Step 3/3] Installing kubefisher chart...",
			"chart", opts.ChartPath, "namespace", opts.Namespace, "dry_run", opts.DryRun)
		if err := HelmInstallIfAbsent(ctx, HelmOpts{
			Release:   "kubefisher",
			Chart:     opts.ChartPath,
			Namespace: opts.Namespace,
			DryRun:    opts.DryRun,
		}); err != nil {
			return err
		}
	}

	if opts.DryRun {
		slog.Info("[Step 3/3] DRY RUN — skipping embedded asset apply (ConfigMap, PrometheusRule)")
		return nil
	}

	slog.Info("[Step 3/3] Applying embedded assets (gpu-pricing ConfigMap, PrometheusRule)...")
	if err := ApplyAssets(ctx, cl, opts.Namespace); err != nil {
		return fmt.Errorf("apply embedded assets: %w", err)
	}
	return nil
}

func printSummary(state InstalledState) {
	slog.Info("Install complete")
	fmt.Println()
	fmt.Println("Detected serving platforms:")

	found := false
	if state.KServe {
		fmt.Println("  KServe   ✓")
		found = true
	}
	if state.BentoML {
		fmt.Println("  BentoML  ✓")
		found = true
	}
	if state.RayTrain {
		fmt.Println("  Ray      ✓")
		found = true
	}
	if !found {
		fmt.Println("  (none detected — kubefisher works without a serving platform)")
	}
	fmt.Println()
}
