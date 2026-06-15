# KubeFisher platform contract

This document is the **single source of truth** for keys, labels, annotations, metric names, and config shapes the KubeFisher platform relies on. Implementations (controllers, recording rules, dashboards) should treat this file as authoritative; when code diverges, **code should be updated** or this document should be amended in the same change.

**Contract ID**: `kubefisher.contract/v1`  
**Scope**: Kubernetes workloads observed by Prometheus + Grafana; annotations written by the **cost patcher** component.

---

## Pod labels (workload identity)

These labels apply to **Pods** (typically via `spec.template.metadata.labels` on a `Deployment`, `StatefulSet`, `Job`, or equivalent pod-creating resource).

| Key | Required | Semantics | Value format |
| --- | --- | --- | --- |
| `kubefisher.io/model` | **Yes** | Logical model identifier for attribution and cost/token math. | Stable **Kubernetes label-safe** string chosen by the team (examples: `facebook-opt-125m`, `meta-llama.Meta-Llama-3-8B-Instruct`). Prefer the same string you expose to users as “model id”, but keep it label-safe (no `/`). |
| `kubefisher.io/platform` | **Yes** | Serving platform family (used for joins, dashboards, and adapter-specific behavior). | Lowercase token: `vllm`, `kserve`, `bentoml`, `triton`, `ray-serve`, `ray`, `kubeflow-trainer`, `kubeflow-training`, `unknown` (see [Platform tokens](#platform-tokens)). |
| `kubefisher.io/team` | No | Team / cost center. When absent, controllers may fall back to **namespace** (documented behavior, not a substitute for labeling in multi-team namespaces). | DNS-like label value recommended (examples: `platform`, `ml-research`). |
| `kubefisher.io/managed-by` | No | Optional provenance for generated workloads (Helm release, GitOps tool, etc.). | Free string (examples: `helm:kubefisher`, `argocd/app-of-apps`). |

### Rules

- **Prefer setting all four** on production pods even when some keys are optional: it makes dashboards and incident response deterministic.
- **Do not overload** `kubefisher.io/model` with raw image tags unless that is your intentional model id.
- **Keep values stable** across rollouts; changing `kubefisher.io/model` is treated as a **new series** for attribution.
- **Unlabeled sentinel**: when a pod is missing one or more of `kubefisher.io/model`, `kubefisher.io/platform`, or `kubefisher.io/team`, the recording rules (`kubefisher:cost_per_hour`, `kubefisher:cost_per_token`) emit the literal string `"unlabeled"` for the corresponding Prometheus label (e.g. `kubefisher_io_team="unlabeled"`). This guarantees every GPU cost series always carries all four label dimensions (`namespace`, `kubefisher_io_team`, `kubefisher_io_platform`, `kubefisher_io_model`) so Grafana template variable queries and filtered panels never silently drop pods. The value `"unlabeled"` is a signal to platform operators that the corresponding pod-template label is not set and attribution is incomplete.

---

## Resource annotations (written by cost patcher)

The **cost patcher** writes the following annotations on user-owned workload objects it can safely mutate (typically the top-level `Deployment` / `StatefulSet` / `InferenceService` reconcile target—not Pods directly—unless explicitly designed otherwise).

| Key | Written by | Semantics | Value format |
| --- | --- | --- | --- |
| `kubefisher.io/cost-per-token` | cost patcher | Best-effort **fully loaded** inference cost per generated token (includes GPU time + allocated overhead policy). | Decimal string in **USD per token** unless `gpu-pricing` declares another `currency` (see below). Example: `0.00000123`. |
| `kubefisher.io/cost-per-hour` | cost patcher | GPU **compute** cost rate for the workload’s allocated GPUs at the time of calculation (policy-defined). Shorthand: **`cost-per-hour` → `kubefisher.io/cost-per-hour`**. | Decimal string in **USD per GPU-hour** (or configured `currency`). Example: `3.50`. |
| `kubefisher.io/gpu-count` | cost patcher | Number of `nvidia.com/gpu` allocated to the workload (from pod limits or KServe `InferenceService` spec). | Decimal integer string. Example: `4`. |
| `kubefisher.io/last-updated-at` | cost patcher | Timestamp of the last successful reconcile that refreshed cost annotations. Shorthand: **`last-updated-at` → `kubefisher.io/last-updated-at`**. | RFC3339 UTC string. Example: `2026-05-04T14:22:01Z`. |
| `kubefisher.io/total-job-cost-usd` | cost patcher (Kubeflow Trainer) | Total GPU cost for a completed `TrainJob`, computed as `cost-per-hour × duration`. Written once when the job reaches `Complete` or `Failed`. | Decimal string in **USD**. Example: `347.12`. |

### Rules

- Annotations are **informational** for MVP visibility; enforcement mechanisms (quotas) remain Kubernetes-native (e.g., GPU requests) unless explicitly documented elsewhere.
- If token counters are missing/unreliable, `kubefisher.io/cost-per-token` may be **absent** or set to a documented sentinel (implementation choice; must match recording rules).

---

## Prometheus metrics (KubeFisher recording series)

These are **KubeFisher-owned recording metric names** (Prometheus `:` naming style). Upstream raw metrics (DCGM fields, `vllm:*` counters, etc.) remain owned by their respective exporters.

| Metric name | Type (intended) | Description |
| --- | --- | --- |
| `kubefisher:cost_per_token` | gauge | Estimated cost per generated token. Denominator: `rate(vllm:prompt_tokens_total[W]) + rate(vllm:generation_tokens_total[W])` where `W` is `costPatcher.tokenRateWindow` (default `5m`). Series only emitted when the combined token rate is `> 0`. Joined on pod/workload labels with identity-label guarantees (see below). |
| `kubefisher:cost_per_hour` | gauge | Estimated GPU compute cost per hour for the attributed workload/GPU slice. |
| `kubefisher:gpu_efficiency_pct` | gauge | Policy-defined “efficiency” (example: useful GPU work vs wall time), expressed as **0–100**. |

### KubeFisher exporter metrics (emitted by cost patcher)

These are emitted by the cost patcher’s `/metrics` endpoint and are intended to be used by recording rules/dashboards.

| Metric name | Type (intended) | Required labels | Description |
| --- | --- | --- | --- |
| `kubefisher_gpu_price_per_hour_by_node` | gauge | `node`, `currency` | Node-scoped GPU list price derived from the `gpu-pricing` ConfigMap by matching each Node’s labels against pricing rules. This metric exists to support reliable joins when `kube_node_labels` is unavailable. |

### Reference upstream series (not KubeFisher-owned; commonly joined)

These names appear in examples and dashboards, but are **not** defined/owned by KubeFisher:

- **DCGM exporter**: `DCGM_FI_DEV_GPU_UTIL`, `DCGM_FI_DEV_FB_USED`, …
- **vLLM OpenAI server `/metrics`**: `vllm:prompt_tokens_total`, `vllm:generation_tokens_total`, `vllm:request_success_total`, …

### Required / expected labels (when available)

Recording rules and dashboards should preserve:

- `namespace`
- `pod` (or `deployment`, `statefulset`, depending on rule level—**pick one canonical level per metric family** in the recording rule implementation)
- `kubefisher_io_model` — always present; value is `"unlabeled"` when `kubefisher.io/model` pod label is absent
- `kubefisher_io_platform` — always present; value is `"unlabeled"` when `kubefisher.io/platform` pod label is absent
- `kubefisher_io_team` — always present; value is `"unlabeled"` when `kubefisher.io/team` pod label is absent

**Label guarantee**: `kubefisher:cost_per_hour` and `kubefisher:cost_per_token` always emit all three identity labels (`kubefisher_io_model`, `kubefisher_io_platform`, `kubefisher_io_team`) on every series. The `"unlabeled"` sentinel is applied via `label_replace` with a `"^$"` match (Prometheus represents absent labels as empty string). Dashboard queries that filter or group by these labels will always include all GPU pods, including those with incomplete pod-template labeling.

---

## Grafana variables (dashboard contract)

Dashboards shipped by KubeFisher should expose at least:

| Variable | Source (typical) | Notes |
| --- | --- | --- |
| `datasource` | Prometheus datasource | Standard Grafana `prometheus` datasource variable. |
| `namespace` | `label_values(kube_pod_info, namespace)` or `label_values(up, namespace)` | Filter to a cluster namespace. |
| `team` | `label_values(..., kubefisher_io_team)` or mapped label | Implementation may use a `label_replace` to map `kubefisher.io/team` → Prometheus-safe label names in recording rules. |
| `model` | `label_values(..., kubefisher_io_model)` | Same mapping note as `team`. |
| `platform` | `label_values(..., kubefisher_io_platform)` | Same mapping note as `team`. |

**Note**: Prometheus label names cannot contain `/`. If recording rules expose `kubefisher_io_team` etc., dashboards should bind to those **exported** label names; this document’s Kubernetes keys remain `kubefisher.io/...`.

---

## GPU pricing: `gpu-pricing.yaml` (ConfigMap payload schema)

The cluster stores GPU list pricing as YAML inside a `ConfigMap` data key (convention: `pricing.yaml`). This is **pricing input** for `kubefisher:cost_per_hour` / `kubefisher:cost_per_token` logic.

### Required top-level fields

- `version` (integer): schema version for this file format.
- `currency` (string): ISO currency code used by all `*_usd_*` fields below (typically `USD` for MVP).
- `GPUs` (array): list of pricing rules.

### Required fields per `GPUs[]` entry

- `match` (object): how to match nodes or workloads. MVP-friendly option:
  - `node_labels` (map[string]string): **all** key/value pairs must match node labels for the rule to apply.
- `price_per_gpu_hour` (number): nominal list price per GPU-hour in `currency`.

### Optional fields

- `description` (string): human-readable note.
- `effective_from` (string): RFC3339 timestamp for auditing (not required for MVP math).

### Rule precedence

Rules are evaluated **first-match-wins by list order**: the matcher iterates the `GPUs` array top-to-bottom and returns the price from the first rule whose `match.node_labels` are **all** present on the node. There is no automatic specificity ranking — a more-specific rule (more labels) wins only if it appears **before** any less-specific rule that would also match. Place overrides (e.g. spot/pre-emptible) before their general counterparts.

### Example `gpu-pricing.yaml`

```yaml
version: 1
currency: USD
GPUs:
  # --- Spot/pre-emptible A10G (more-specific rule — MUST come first) ---
  # Matches nodes with BOTH accelerator=nvidia-a10g AND kubefisher.io/spot=true.
  # If this were listed after the general on-demand rule, first-match-wins would
  # select the on-demand price for spot nodes and this entry would be unreachable.
  - description: "A10G on spot / pre-emptible node (30 % of on-demand)"
    match:
      node_labels:
        accelerator: "nvidia-a10g"
        kubefisher.io/spot: "true"
    price_per_gpu_hour: 0.38   # 1.25 × 0.30 ≈ 0.38

  # --- On-demand A10G (general rule — MUST come after the spot override) ---
  - description: "Dev k3d fake A10G-ish label"
    match:
      node_labels:
        accelerator: "nvidia-a10g"
    price_per_gpu_hour: 1.25

  - description: "GKE A100 DWS example"
    match:
      node_labels:
        cloud.google.com/gke-accelerator: "nvidia-tesla-a100"
    price_per_gpu_hour: 3.40
```

### Example `ConfigMap` wrapper

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: gpu-pricing
  namespace: kubefisher-system
data:
  pricing.yaml: |
    version: 1
    currency: USD
    GPUs:
      - match:
          node_labels:
            accelerator: "nvidia-a10g"
        price_per_gpu_hour: 1.25
```

---

## Platform tokens

| Token | Workload type | Meaning |
| --- | --- | --- |
| `vllm` | inference | Plain Kubernetes Deployment/StatefulSet running vLLM (or similar). |
| `triton` | inference | NVIDIA Triton on Kubernetes — **Generic adapter** only (see [ADR: Triton](adr/triton-adapter-decision.md)). |
| `kserve` | inference | KServe `InferenceService`. |
| `bentoml` | inference | Yatai `BentoDeployment` (`serving.yatai.ai/v2alpha1`). |
| `ray-serve` | inference | KubeRay `RayService` (Ray Serve on Kubernetes). |
| `kubeflow-trainer` | training | Kubeflow Trainer v2 `TrainJob` (`trainer.kubeflow.org/v1alpha1`). |
| `kubeflow-training` | training | Legacy Kubeflow Training Operator jobs (`PyTorchJob`, `TFJob`, …). |
| `ray` | training | KubeRay `RayJob` / `RayCluster` (not Ray Serve). |
| `unknown` | inference | Fallback when autodetection cannot classify the workload. |

**`ray` vs `ray-serve`:** use `ray-serve` for inference workloads backed by a `RayService` CRD; use `ray` for distributed training via `RayJob` or `RayCluster`.

---

## Platform adapters (cost patcher)

The cost patcher resolves each GPU pod to a **top-level owner** and writes [resource annotations](#resource-annotations-written-by-cost-patcher) on that object. Adapters are registered in [`internal/adapters/registry.go`](../internal/adapters/registry.go); **first match wins** (`Generic` is always last).

| Order | Adapter | `kubefisher.io/platform` | Detect (pod labels) | Annotated resource | RBAC resource |
| --- | --- | --- | --- | --- | --- |
| 1 | KServe | `kserve` | `serving.kserve.io/inferenceservice` or `kubefisher.io/platform=kserve` | `InferenceService` (`serving.kserve.io/v1beta1`) | `inferenceservices` |
| 2 | Kubeflow Trainer | `kubeflow-trainer` | `trainer.kubeflow.org/trainjob-ancestor-step` or `trainer.kubeflow.org/trainjob-name` | `TrainJob` (`trainer.kubeflow.org/v1alpha1`) | `trainjobs` |
| 3 | Ray Serve | `ray-serve` | `ray.io/serve-deployment` or `kubefisher.io/platform=ray-serve` | `RayService` (`ray.io/v1`) | `rayservices` |
| 4 | BentoML | `bentoml` | `yatai.bentoml.com/bento-deployment` or `kubefisher.io/platform=bentoml` | `BentoDeployment` (`serving.yatai.ai/v2alpha1`) | `bentodeployments` |
| 5 | Generic | *(from workload)* | catch-all for remaining GPU pods | `Deployment` / `StatefulSet` | `deployments`, `statefulsets` |

**Triton:** no dedicated adapter — GPU Triton `Deployment`s use **Generic** when pods carry contract labels. See [ADR: Triton adapter decision](adr/triton-adapter-decision.md).

**Resolve hints:**

- **KServe / BentoML / Kubeflow Trainer:** direct `Get` by name from a pod label (see samples below).
- **Ray Serve:** `Get` by listing `RayService` objects and matching `status.activeServiceStatus.rayClusterName` to the pod’s `ray.io/cluster-name` label (KubeRay does not label pods with the RayService name directly).
- **Kubeflow Trainer:** also writes `kubefisher.io/total-job-cost-usd` once when the `TrainJob` reaches `Complete` or `Failed`.
- **KServe:** optional `OwnerReconciler` writes `$0` `cost-per-hour` on scale-to-zero `InferenceService` instances with no active pods.

**CLI-only (no cost-patcher adapter today):** `RayJob`, `RayCluster`, legacy Kubeflow training CRDs — still appear in `kubefisher cost` when the CRD is installed and the workload requests GPUs or has cost annotations.

Runtime details: [`docs/cost-patcher.md`](cost-patcher.md).

---

## Sample pod label snippets (copy/paste)

### KServe `InferenceService` (pod template labels)

```yaml
apiVersion: serving.kserve.io/v1beta1
kind: InferenceService
metadata:
  name: llama3
  namespace: team-a
spec:
  predictor:
    pod:
      metadata:
        labels:
          kubefisher.io/model: "meta-llama.Meta-Llama-3-8B-Instruct"
          kubefisher.io/platform: "kserve"
          kubefisher.io/team: "team-a"
          kubefisher.io/managed-by: "kserve"
    # ... model server container config ...
```

### BentoML `BentoDeployment` (Yatai)

Cost annotations are written on the `BentoDeployment` CRD. Pods should carry `yatai.bentoml.com/bento-deployment` set to the deployment name (Yatai sets this on child pods; you can also add it on templates).

```yaml
apiVersion: serving.yatai.ai/v2alpha1
kind: BentoDeployment
metadata:
  name: sentiment-service
  namespace: team-a
spec:
  # ... bento / runtime spec ...
```

Pod labels (on workloads created by Yatai):

```yaml
metadata:
  labels:
    yatai.bentoml.com/bento-deployment: sentiment-service
    kubefisher.io/model: "sentiment-v2"
    kubefisher.io/platform: "bentoml"
    kubefisher.io/team: "team-a"
```

### Kubeflow Trainer v2 `TrainJob` (pod template labels)

Trainer pods are detected via `trainer.kubeflow.org/trainjob-ancestor-step` and resolved to the parent `TrainJob` using `trainer.kubeflow.org/trainjob-name` (or `jobset.sigs.k8s.io/jobset-name`).

```yaml
apiVersion: trainer.kubeflow.org/v1alpha1
kind: TrainJob
metadata:
  name: llama-finetune
  namespace: team-a
spec:
  # ... runtime / trainer spec ...
```

Pods created by the Trainer runtime should carry:

```yaml
metadata:
  labels:
    trainer.kubeflow.org/trainjob-name: llama-finetune
    trainer.kubeflow.org/trainjob-ancestor-step: trainer
    kubefisher.io/model: "meta-llama.Meta-Llama-3-8B-Instruct"
    kubefisher.io/platform: "kubeflow-trainer"
    kubefisher.io/team: "team-a"
```

On job completion, the cost patcher writes `kubefisher.io/total-job-cost-usd` on the `TrainJob`.

### KubeRay `RayService` (Ray Serve)

KubeRay sets `ray.io/cluster-name` on Ray cluster pods. Add `ray.io/serve-deployment` on your Serve pod templates so the cost patcher selects the Ray Serve adapter (same opt-in pattern as BentoML).

```yaml
apiVersion: ray.io/v1
kind: RayService
metadata:
  name: llm-serve
  namespace: team-a
spec:
  # ... serveConfigV2 + rayClusterConfig ...
```

Pod labels on Ray Serve worker/head pods (template):

```yaml
metadata:
  labels:
    ray.io/serve-deployment: llm-serve
    ray.io/cluster-name: llm-serve-raycluster-xxxxx
    kubefisher.io/model: "meta-llama.Meta-Llama-3-8B-Instruct"
    kubefisher.io/platform: "ray-serve"
    kubefisher.io/team: "team-a"
```

### Plain Kubernetes `Deployment` (vLLM-style)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vllm
  namespace: team-a
spec:
  template:
    metadata:
      labels:
        app: vllm
        kubefisher.io/model: "facebook-opt-125m"
        kubefisher.io/platform: "vllm"
        kubefisher.io/team: "team-a"
    spec:
      containers: []
```

### NVIDIA Triton (Generic adapter — no dedicated CRD)

Standard Triton Kubernetes deployments use the **Generic** cost-patcher adapter. Label pod templates with contract keys; sample manifest: [`config/cluster/serving/triton/triton-resnet50.yaml`](../config/cluster/serving/triton/triton-resnet50.yaml).

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: triton-resnet50
  namespace: team-a
spec:
  template:
    metadata:
      labels:
        kubefisher.io/model: "resnet50"
        kubefisher.io/platform: "triton"
        kubefisher.io/team: "team-a"
    spec:
      containers:
        - name: triton
          resources:
            limits:
              nvidia.com/gpu: "1"
```

See [ADR: Triton adapter decision](adr/triton-adapter-decision.md).

---

## Contract versioning policy

### What “counts” as the contract

- **Breaking** changes: renaming/removing any key in this document, changing value formats, or changing metric names/label contracts.
- **Additive** changes: new optional labels/annotations/metrics, new optional `GPUs[]` fields, new Grafana variables.

### Versioning

- This document uses **`kubefisher.contract/v1`** as the contract bundle id.
- **SemVer alignment**:
  - **KubeFisher app/chart version** should bump **MINOR** for additive contract changes and **MAJOR** for breaking contract changes.
  - Recording rules + dashboards + patcher should ship **in the same release** as contract changes.
- **Pre-1.0 annotation rename (this release)**: `kubefisher.io/cost-per-hour` has been renamed to `kubefisher.io/cost-per-hour-per-replica` and a new `kubefisher.io/cost-per-hour-total` annotation added. The old key is dual-written for migration. This is a pre-1.0 contract change and does not require a MAJOR bump by policy; any tooling reading the old key will continue to work for one release cycle.

### Deprecation process

1. Mark old key/metric **deprecated** in this document for at least one release (document the replacement).
2. Implementations must **dual-write** or **dual-read** during migration when feasible.
3. Remove deprecated keys only on a **MAJOR** bump unless explicitly exempted (internal-only keys).

---

## Change control

Any PR that introduces a new label, annotation, metric, or Grafana variable must:

- update this document first (or in the same PR), and
- update examples under `config/cluster/**` when they are meant to be canonical references.
