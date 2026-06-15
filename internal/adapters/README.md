# Platform adapters (`internal/adapters`)

Ordered registry of cost-patcher [`platform.Adapter`](../costpatcher/platform/adapter.go) implementations. The reconciler in `internal/costpatcher/reconcile.go` uses **first match wins**; `Generic` must stay last.

## Registry ([`registry.go`](registry.go))

| Order | Type | Platform token |
| --- | --- | --- |
| 1 | `platform.KServe` | `kserve` |
| 2 | `platform.KubeflowTrainer` | `kubeflow-trainer` |
| 3 | `platform.RayServe` | `ray-serve` |
| 4 | `platform.BentoML` | `bentoml` |
| 5 | `platform.Generic` | *(catch-all)* |

Implementations live in [`internal/costpatcher/platform/`](../costpatcher/platform/).

## Tests ([`testharness/`](testharness/))

- **`AdapterTestSuite`**: envtest-based tests for `Detect`, `ResolveTarget`, and `WriteCost`.
- **`testdata/`**: minimal CRD stubs (`InferenceService`, `BentoDeployment`, `TrainJob`, `RayService`).
- Run with the root module: `make test` (sets `KUBEBUILDER_ASSETS`).

## Docs

- Contract (labels, annotations, adapter table): [`docs/contract.md`](../../docs/contract.md)
- Runtime behavior: [`docs/cost-patcher.md`](../../docs/cost-patcher.md)
- How to add adapter #6: [`CONTRIBUTING.md`](../../CONTRIBUTING.md#adding-a-platform-adapter)
