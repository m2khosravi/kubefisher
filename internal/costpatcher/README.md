# cost-patcher (`internal/costpatcher`)

Private application code for the `cost-patcher` binary:

- **`app.go`** — process wiring (HTTP `/metrics`, pricing refresh, reconcile loop); imports adapter list from [`internal/adapters`](../adapters/registry.go).
- **`reconcile.go`** — periodic GPU pod discovery, Prometheus queries, adapter dispatch, annotation patches.
- **`pricing/`** — ConfigMap YAML loading + Prometheus gauge collector for list prices.
- **`platform/`** — adapter implementations (`Detect`, `ResolveTarget`, `WriteCost`) and shared patch helpers.
- **`contract/`** — stable annotation and platform constants aligned with [`docs/contract.md`](../../docs/contract.md).

## Platform adapters

Registered in [`internal/adapters/registry.go`](../adapters/registry.go): **KServe**, **KubeflowTrainer**, **RayServe**, **BentoML**, **Generic**. See [`internal/adapters/README.md`](../adapters/README.md) and [`docs/cost-patcher.md`](../../docs/cost-patcher.md#platform-adapters-which-resource-is-annotated).

Shared infrastructure: **`internal/kubeclient`**, **`pkg/promclient`**.
