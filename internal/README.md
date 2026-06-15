# `internal/` — private application libraries

Per [Standard Go Project Layout](https://github.com/golang-standards/project-layout/tree/master/internal), packages here are **importable only from within this module** (`github.com/m2khosravi/kubefisher/...`). They are not a public Go API.

## Layout

| Path | Role |
|------|------|
| **`internal/costpatcher/`** | Cost patcher: reconcile loop, pricing gauge export, platform adapter implementations, annotation patching. Consumed by **`cmd/cost-patcher`**. |
| **`internal/adapters/`** | Ordered platform adapter registry + envtest harness. See [`internal/adapters/README.md`](adapters/README.md). |
| **`internal/cli/kubefisher/`** | **`kubefisher` CLI**: Cobra commands, `client-go` / `clientcmd` / dynamic client for CRDs. Consumed by **`cmd/kubefisher`**. Uses **no** controller-runtime (by design). |
| **`internal/kubeclient/`** | Shared controller-runtime cached client bootstrap for binaries that run inside or against the cluster with a manager-style cache. Used by **cost-patcher** today. |

**Not under `internal/`:**

- **`pkg/promclient/`** — Prometheus HTTP instant-query client; shared by **cost-patcher** (root module) and **`operator/`** (nested module imports via `replace` in `operator/go.mod`).
- **`operator/`** — separate **nested Go module** (kubebuilder). See **`operator/README.md`** and **`docs/teaminferencequota-operator.md`**.

## Conventions

- **`cmd/*`** stay thin: parse flags / signal handling, call into `internal/`.
- **Kubernetes API in operators**: controller-runtime `client.Client` in **`operator/`** only.
- **Kubernetes API in CLI**: `k8s.io/client-go` + `clientcmd` in **`internal/cli/kubefisher/`** only (kubectl-compatible kubeconfig).
