# Cost patcher (end-to-end)

This document explains the **KubeFisher cost patcher** end-to-end: pricing input, Prometheus recording rules, adapter resolution, Kubernetes annotation writes, deployment manifests, and local verification.

If anything here conflicts with `docs/contract.md`, **`docs/contract.md` wins`.

For the **TeamInferenceQuota** operator (namespace budgets, Prometheus-backed status, reconcile loop), see **`docs/teaminferencequota-operator.md`**.

---

## What it does

Every ~30s, the cost patcher:

1. Lists Kubernetes **Pods requesting `nvidia.com/gpu`**.
2. Resolves each Pod to a **top-level user-owned resource** (adapter pattern).
3. Queries Prometheus for:
   - `kubefisher:cost_per_hour{namespace,pod}` (**required**)
   - `kubefisher:cost_per_token{namespace,pod}` (**optional/best-effort**)
4. Writes the values as annotations on the resolved target object:
   - `kubefisher.io/cost-per-hour`
   - `kubefisher.io/cost-per-token` (only when present; omitted for loading pods)
   - `kubefisher.io/gpu-count` (when GPU count is known)
   - `kubefisher.io/last-updated-at`
   - `kubefisher.io/total-job-cost-usd` (Kubeflow Trainer only, on job completion)

Annotation keys and semantics are defined in **`docs/contract.md`**.

---

## Inputs and outputs

### Pricing input (ConfigMap)

Pricing is supplied via a ConfigMap (rendered by the Helm chart from values):

- **Helm template**: `charts/kubefisher/templates/configmap-gpu-pricing.yaml` (values: `pricing.*` in `charts/kubefisher/values.yaml`)
- **Data key**: `pricing.yaml`
- **Schema**: `docs/contract.md` (“GPU pricing: gpu-pricing.yaml”)

The patcher reads `pricing.yaml` periodically and exports a Prometheus gauge series:

- **Metric**: `kubefisher_gpu_price_per_hour{currency, label_<nodeLabelKey>=...}`

Label names are formatted to match kube-state-metrics style `kube_node_labels` (see `internal/costpatcher/pricing/collector.go`).

#### Node-scoped pricing metric (recommended)

In some clusters (including many local kube-state-metrics installs), `kube_node_labels` is **not** exposed by default.
To keep `cost_per_hour` reliable without depending on `kube_node_labels`, the cost patcher also computes a per-node price map and exposes:

- **Metric**: `kubefisher_gpu_price_per_hour_by_node{currency, node}`

This metric is derived from:

- `gpu-pricing` ConfigMap rules (`match.node_labels`)
- Kubernetes `Node` labels (`kubectl get nodes --show-labels`)

The shipped recording rule `kubefisher:cost_per_hour` joins on **`node`** using this metric.

### Prometheus inputs (required series)

The current recording rules depend on:

- kube-state-metrics:
  - `kube_pod_container_resource_limits{resource="nvidia_com_gpu"}`
  - `kube_pod_info{namespace,pod,node}`
- Kubernetes API (for node label lookup inside patcher):
  - `Node.metadata.labels`
- vLLM metrics (only for cost/token):
  - `vllm:prompt_tokens_total`
  - `vllm:generation_tokens_total`
- DCGM metrics (efficiency signal):
  - `DCGM_FI_DEV_GPU_UTIL`

### Prometheus outputs (recording rules)

KubeFisher-owned recording series are defined in the Helm chart:

- **`charts/kubefisher/templates/prometheusrule.yaml`**

Today’s rules produce:

- `kubefisher:cost_per_hour{namespace,pod}`
- `kubefisher:cost_per_token{namespace,pod}` (**only when** `(rate(vllm:prompt_tokens_total[5m]) + rate(vllm:generation_tokens_total[5m])) > 0`)
- `kubefisher:gpu_efficiency_pct` (cluster-wide avg for now)

---

## Platform adapters (which resource is annotated)

The patcher does **not** annotate Pods directly. Each GPU pod is matched by the **first** adapter in the registry (see [`internal/adapters/registry.go`](../internal/adapters/registry.go), imported from `internal/costpatcher/app.go`). Implementations live under `internal/costpatcher/platform/`.

| Order | Adapter | Detect signal (pod) | Annotated object | Notes |
| --- | --- | --- | --- | --- |
| 1 | **KServe** | `serving.kserve.io/inferenceservice` | `InferenceService` | `OwnerReconciler`: scale-to-zero → `cost-per-hour: 0` when no ready replicas and no active pods |
| 2 | **KubeflowTrainer** | `trainer.kubeflow.org/trainjob-ancestor-step` or `trainjob-name` | `TrainJob` | Writes `total-job-cost-usd` when status is `Complete` or `Failed` |
| 3 | **RayServe** | `ray.io/serve-deployment` | `RayService` | Resolves via `ray.io/cluster-name` ↔ `status.activeServiceStatus.rayClusterName` |
| 4 | **BentoML** | `yatai.bentoml.com/bento-deployment` | `BentoDeployment` | Label value = deployment name |
| 5 | **Generic** | catch-all | `Deployment` / `StatefulSet` | Pod → ReplicaSet → Deployment, or Pod → StatefulSet. Covers plain vLLM, **Triton**, and other GPU Deployments — see [ADR: Triton](adr/triton-adapter-decision.md). |

Full label and GVK details: [`docs/contract.md`](contract.md#platform-adapters-cost-patcher).

To add another platform, register a new adapter **before** `Generic` in `internal/adapters/registry.go` and update RBAC, `internal/cost/detect.go`, and discovery in `internal/cost/collect.go` / `internal/workload/discover.go` as needed. See [`CONTRIBUTING.md`](../CONTRIBUTING.md#adding-a-platform-adapter).

### RBAC (Helm chart)

The cost-patcher ClusterRole can patch:

- `apps/deployments`, `apps/statefulsets`
- `serving.kserve.io/inferenceservices`
- `serving.yatai.ai/bentodeployments`
- `trainer.kubeflow.org/trainjobs`
- `ray.io/rayservices`

See [`charts/kubefisher/templates/cost-patcher/rbac.yaml`](../charts/kubefisher/templates/cost-patcher/rbac.yaml).

---

## Go code layout (where things live)

This repo follows the community [Standard Go Project Layout](https://github.com/golang-standards/project-layout):

- `cmd/cost-patcher/`: flags + `main` only
- `internal/costpatcher/`:
  - `app.go`: wiring (pricing refresh loop, reconcile loop, HTTP server)
  - `reconcile.go`: list GPU Pods → query Prometheus → patch annotations
  - `pricing/`: `pricing.yaml` loader + pricing gauge collector
  - `platform/`: adapter implementations + patch helper
  - `contract/`: annotation and platform constants
- `internal/adapters/`: ordered adapter registry + envtest harness (`testharness/`)
- `internal/kubeclient/`: controller-runtime client bootstrap; **`pkg/promclient`**: Prometheus instant queries (also used by **`operator/`**).
- **`internal/README.md`**: map of **`internal/`** vs **`pkg/`** vs nested **`operator/`** module.

For the **TeamInferenceQuota** operator layout, see **`docs/teaminferencequota-operator.md`**. For the **`kubefisher`** CLI, see **`cmd/kubefisher/README.md`**.

## Deployment manifests

**Canonical install:** the Helm chart at **`charts/kubefisher/`** (see **`deployments/README.md`** and the root **`README.md`**). It renders cost-patcher Deployment, RBAC, `gpu-pricing`, `ServiceMonitor`, `PrometheusRule`, and optional Grafana dashboard.

**Dev-only fixtures** (not in the chart):

- **`deployments/kubernetes/test/`** — e.g. `gpu-fake-workload.yaml` for **`make cluster-install-test-gpu-fake`** / e2e cost-hour checks.
- **`config/cluster/`** — k3d observability values, DCGM mock, vLLM demo manifests and `ServiceMonitor`s used by **`make cluster-*`** targets.

Older paths such as `deployments/kubernetes/cost-patcher/` or `deployments/kubernetes/observability/` are **removed**; do not reference them in scripts.

---

## Local verification (k3d)

Prerequisite: bring up the local cluster and observability stack:

```bash
make cluster-up
```

### Verify cost/hour annotation

This uses a tiny `pause` Deployment that requests a fake GPU:

```bash
make cluster-e2e-cost-patcher-hour
```

What it does:

- builds the image (`make cost-patcher-image`)
- imports into k3d (`make cluster-k3d-import-cost-patcher`)
- applies KubeFisher manifests (`make cluster-install-kubefisher-cost`)
- deploys `gpu-fake-workload` (`make cluster-install-test-gpu-fake`)
- verifies `kubefisher.io/cost-per-hour` annotation appears on `Deployment/gpu-fake-workload`

### Verify cost/token annotation

`kubefisher:cost_per_token` requires a **rising** `vllm:generation_tokens_total`.
The static nginx vLLM mock has fixed counters, so `rate(...)` is 0 and **cost/token will not appear**.

Use the deterministic vLLM token mock workload (requests a fake GPU so the patcher selects it):

```bash
make cluster-e2e-cost-patcher-token
```

This deploys:

- `config/cluster/serving/vllm/vllm-costtoken-mock.yaml`
- `config/cluster/serving/vllm/vllm-costtoken-mock-servicemonitor.yaml`

Then waits for the 5-minute PromQL rate window before checking the annotations.

---

## Common troubleshooting

- **No `cost-per-hour` annotation**:
  - ensure the Pod actually requests `nvidia.com/gpu`
  - ensure `kubefisher_gpu_price_per_hour_by_node` is present (patcher `/metrics` / Prometheus)
  - check Prometheus has kube-state-metrics series `kube_pod_container_resource_limits{resource="nvidia_com_gpu"}`
  - check `kubefisher:cost_per_hour` exists for the `(namespace,pod)` label set
  - if `kubefisher:cost_per_hour` is empty, check whether kube-state-metrics exposes `kube_pod_info` and GPU limits series

- **No `cost-per-token` annotation**:
  - verify `vllm:generation_tokens_total` is scraped and labels include `namespace` and `pod`
  - generate traffic so the counters increase (then wait for the `rate(...[5m])` window — ~5 minutes after first scrape)

- **Patcher running in-cluster but not starting** (k3d):
  - import the local image into the cluster: `make cluster-k3d-import-cost-patcher`

---

## Testing (unit + e2e)

### Unit tests

We keep unit tests focused on logic we own (no Prometheus/cluster dependencies):

- `internal/costpatcher/platform/*_test.go`: per-adapter `Detect` and helpers (`kserve`, `bentoml`, `kubeflow_trainer`, `ray_serve`, `generic`, `patch`, `gpu`)
- `internal/adapters/testharness/`: `AdapterTestSuite` (envtest) for each registered adapter
- `internal/costpatcher/platform/patch_test.go`:
  - cost formatting + “should patch” decision
- `internal/costpatcher/pricing/loader_test.go`:
  - table-driven pricing YAML validation
- `internal/costpatcher/pricing/nodeprice.go`:
  - per-node rule matching (used for `kubefisher_gpu_price_per_hour_by_node`)

Run:

```bash
go test ./...
```

### E2E checks (local k3d)

Use:

- `make cluster-e2e-cost-patcher-hour`
- `make cluster-e2e-cost-patcher-token`

## Cost model and limitations

### Allocated-capacity pricing (list price)

`kubefisher:cost_per_hour` and the annotations it drives reflect **allocated list price**: the number of GPUs in the pod's resource limits multiplied by the matching node price from the `gpu-pricing` ConfigMap (see [contract.md — GPU pricing schema](contract.md#gpu-pricing-gpu-pricingyaml-configmap-payload-schema)).

This is intentional and consistent with standard cloud chargeback practice: teams are charged for **reserved capacity** regardless of how busy those GPUs are. A team that provisions four A10G replicas but runs them at 5% utilisation pays for four A10G-hours — the same as a team running at 95%. This model:

- incentivises right-sizing and scale-down when workloads are idle.
- aligns with what the cloud provider or on-prem capital budget actually costs, independent of runtime activity.

**GPU utilisation is therefore a separate, complementary signal** — not a denominator in the cost formula. The "GPU efficiency (cluster-wide)" Grafana panel (`kubefisher:gpu_efficiency_pct`) exists precisely to let operators cross-reference spend against efficiency: high spend + low efficiency is the actionable alert.

### Known limitations

**MIG and GPU time-slicing are not yet accounted for.**  
NVIDIA Multi-Instance GPU (MIG) and GPU time-slicing allow multiple pods to share one physical GPU device. The current cost attribution model counts each pod's `nvidia.com/gpu` limit as a whole physical GPU and applies the full node-level list price per GPU. In clusters using MIG profiles (e.g. `1g.5gb`) or the time-slicing device plugin, this will **over-charge** individual pods proportionally to the number of tenants sharing the physical device.

Correct attribution in those environments requires either:
- a pricing rule with a fraction of the per-GPU list price for the relevant MIG profile, or
- a future adapter that reads the MIG slice count and divides the node price accordingly.

This limitation is tracked in the backlog. Until resolved, treat `cost_per_hour` values on MIG/time-sliced nodes as proportional estimates, not absolute charges.

