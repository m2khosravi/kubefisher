package install

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// HelmOpts configures a single helm upgrade --install invocation.
type HelmOpts struct {
	// Release is the Helm release name.
	Release string
	// Chart is a chart reference: registry/chart, OCI URL, or local path.
	Chart string
	// Namespace is the target namespace (created if absent).
	Namespace string
	// SetValues is a list of --set k=v overrides.
	SetValues []string
	// Repo is an optional --repo URL (for non-OCI chart repos).
	Repo string
	// Version pins the chart version (--version). Empty means latest.
	Version string
	// DryRun appends --dry-run to the command (no changes, plan only).
	DryRun bool
}

// HelmInstallIfAbsent runs `helm upgrade --install` which is idempotent by
// design: re-running against an already-installed release at the same version
// is a no-op. The atomic flag ensures a failed install is rolled back.
func HelmInstallIfAbsent(ctx context.Context, opts HelmOpts) error {
	args := []string{
		"upgrade", "--install",
		"--create-namespace",
		"--namespace", opts.Namespace,
		"--atomic",
		"--timeout", "5m",
		"--wait",
	}
	if opts.Repo != "" {
		args = append(args, "--repo", opts.Repo)
	}
	if opts.Version != "" {
		args = append(args, "--version", opts.Version)
	}
	for _, sv := range opts.SetValues {
		args = append(args, "--set", sv)
	}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	args = append(args, opts.Release, opts.Chart)

	slog.Info("helm", "cmd", "upgrade --install", "release", opts.Release,
		"chart", opts.Chart, "namespace", opts.Namespace, "dry_run", opts.DryRun)

	cmd := exec.CommandContext(ctx, "helm", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		combined := strings.TrimSpace(stderr.String())
		if combined == "" {
			combined = strings.TrimSpace(stdout.String())
		}
		return fmt.Errorf("helm upgrade --install %s: %w\n%s", opts.Release, err, combined)
	}

	if out := strings.TrimSpace(stdout.String()); out != "" {
		slog.Info("helm output", "release", opts.Release, "output", out)
	}
	return nil
}

// helmInPath returns an error if the `helm` binary is not found in PATH.
// Called once at the start of Run() to give a clear error early.
func helmInPath() error {
	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm not found in PATH: install helm (https://helm.sh/docs/intro/install/) and retry")
	}
	return nil
}
