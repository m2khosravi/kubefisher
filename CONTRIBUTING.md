# Contributing to KubeFisher

Thank you for your interest in contributing. This document covers how to build, test, and extend the project locally.

---

## Running locally

### Prerequisites

- Go ≥ 1.22
- Docker (running)
- k3d ≥ 5.6
- kubectl, helm, make

### Bring up a local cluster

```bash
make cluster-up          # k3d + Prometheus + fake DCGM (≈5 min on first run)
make kubefisher-build     # CLI binary → bin/kubefisher
bash hack/demo.sh --skip-cluster   # smoke-test the full pipeline
```

### Run all tests

```bash
go test ./...            # root module (CLI, cost-patcher, workload packages)
make operator-test       # operator (controller + webhook; downloads envtest on first run)
```

Run only fast unit tests (no envtest download required):

```bash
cd operator && go test ./internal/controller/ -run TestComputePhase -count=1
```

### Build the cost-patcher image

```bash
make cost-patcher-image                  # builds kubefisher/cost-patcher:dev
make cluster-k3d-import-cost-patcher    # imports into k3d
make cluster-install-kubefisher-cost   # installs Helm chart
```

---

## Project layout

The repo follows [Standard Go Project Layout](https://github.com/golang-standards/project-layout):

| Path | Role |
|------|------|
| `cmd/kubefisher/` | CLI entrypoint (thin: flags only) |
| `cmd/cost-patcher/` | Cost-patcher entrypoint |
| `internal/cli/kubefisher/` | All CLI commands (Cobra + client-go) |
| `internal/cost/` | Cost row collection and rendering |
| `internal/deploy/` | Deploy strategies (KServe, generic vLLM) |
| `internal/workload/` | Workload discovery, pod finding |
| `internal/install/` | Install command logic + embedded assets |
| `internal/costpatcher/` | Cost-patcher reconcile loop and platform adapters |
| `internal/costpatcher/contract/` | Shared label/annotation constants |
| `operator/` | TeamInferenceQuota kubebuilder operator (nested Go module) |
| `charts/kubefisher/` | Helm chart (canonical install path) |
| `hack/` | Dev scripts and demo fixtures |

Full details: [`internal/README.md`](internal/README.md) and [`docs/cli.md`](docs/cli.md).

---

## Adding a platform adapter

To add support for a new platform (e.g. Triton, TorchServe), follow the same pattern as [`internal/costpatcher/platform/bentoml.go`](internal/costpatcher/platform/bentoml.go) or [`internal/costpatcher/platform/ray_serve.go`](internal/costpatcher/platform/ray_serve.go).

### 1. Add a platform constant

Add to [`internal/costpatcher/contract/platforms.go`](internal/costpatcher/contract/platforms.go):

```go
PlatformTriton = "triton"
```

### 2. Implement `platform.Adapter`

Create `internal/costpatcher/platform/triton.go`:

```go
type Triton struct{}

func (Triton) Name() string { return contract.PlatformTriton }
func (Triton) Detect(pod *corev1.Pod) bool { ... }
func (Triton) ResolveTarget(ctx context.Context, c client.Client, pod *corev1.Pod) (client.Object, error) { ... }
func (Triton) WriteCost(ctx context.Context, c client.Client, target client.Object, res CostResult) error {
    return PatchTargetAnnotations(ctx, c, target, res)
}
```

Optional: implement [`OwnerReconciler`](internal/costpatcher/platform/adapter.go) if the platform needs reconciliation without active pods (see KServe scale-to-zero).

### 3. Register the adapter

Insert `platform.Triton{}` in [`internal/adapters/registry.go`](internal/adapters/registry.go) **before** `platform.Generic{}` (first match wins).

### 4. RBAC and CLI discovery (if the platform has a CRD)

- Add `get/list/watch/patch/update` for the CRD in [`charts/kubefisher/templates/cost-patcher/rbac.yaml`](charts/kubefisher/templates/cost-patcher/rbac.yaml).
- Extend discovery in [`internal/workload/discover.go`](internal/workload/discover.go) and [`internal/cost/collect.go`](internal/cost/collect.go) (`discoverCRDs` / `discoverResources`).
- Add kind/label cases in [`internal/cost/detect.go`](internal/cost/detect.go) and pod selectors in [`internal/workload/pods.go`](internal/workload/pods.go) / [`internal/workload/find.go`](internal/workload/find.go) as needed.

### 5. Tests

- Unit tests: `internal/costpatcher/platform/triton_test.go` (`Detect`, resolve helpers).
- Integration: add `TestTritonAdapterSuite` in [`internal/adapters/testharness/adapters_test.go`](internal/adapters/testharness/adapters_test.go) using `AdapterTestSuite` + a minimal CRD under `testharness/testdata/`.
- Run `make test`.

### 6. Documentation

Update [`docs/contract.md`](docs/contract.md) (platform tokens + adapter table) and [`docs/cost-patcher.md`](docs/cost-patcher.md) in the same PR.

---

## Operator development

The operator is a nested Go module at `operator/` (kubebuilder v4 + controller-runtime v0.23).

```bash
# Install CRD into your current cluster context
cd operator && make install

# Run controller locally (watches cluster; hot-reload on code changes)
make operator-run      # from repo root — calls 'make -C operator run'

# Regenerate CRD manifests + RBAC after changing types
make operator-manifests
```

After modifying `operator/api/v1alpha1/teaminferencequota_types.go`, always run `make operator-manifests` and commit the regenerated files under `operator/config/crd/`.

Controller tests use envtest (real API server, fake Prometheus HTTP):

```bash
make operator-test
```

---

## Code style

- **`gofmt`** — all Go files must pass `gofmt -l .` with no output.
- **No controller-runtime in the CLI** — `internal/cli/kubefisher/` uses `k8s.io/client-go` + `clientcmd` only (never `sigs.k8s.io/controller-runtime`). This is enforced by architecture convention; see [`internal/README.md`](internal/README.md).
- **No magic strings** — platform and annotation names must come from `internal/costpatcher/contract/platforms.go`. Do not hardcode `"kserve"`, `"kubefisher.io/cost-per-hour"`, etc. inline.
- **Keep `cmd/*` thin** — business logic belongs in `internal/`; entrypoints parse flags and call in.
- **Imports** — group standard library, external, and internal imports with blank lines between groups.

---

## Opening a pull request

1. **Branch naming:** `feat/<short-description>`, `fix/<issue-number>-description`, `docs/<topic>`.
2. **Link an issue:** reference it in the PR description (`Closes #N`).
3. **Tests:** new behaviour should have a unit test. For CLI commands, a table-driven test of the output formatting is sufficient.
4. **Docs:** if you add a new command flag or change observable behaviour, update [`docs/cli.md`](docs/cli.md) and the relevant `Short`/`Long` Cobra fields.
5. **Commit messages:** use [Conventional Commits](https://www.conventionalcommits.org/) — `feat:`, `fix:`, `docs:`, `refactor:`, `test:`.
6. **CI:** all checks must pass before merge. The `go test ./...` and `make operator-test` jobs are required.

---

## Release process

See [`docs/releasing.md`](docs/releasing.md) for the full release checklist
(version bump → tag → CI image push → GitHub Release → first-time GHCR
visibility step).
