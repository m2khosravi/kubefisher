# Frequently asked questions

---

## Why is cost/token showing — ?

`kubefisher cost` reads the annotation `kubefisher.io/cost-per-token` from each workload. The annotation is written by the **cost-patcher**, which must be running in the cluster and able to reach Prometheus.

**Common reasons for `—`:**

1. **Cost-patcher is not installed.** The demo workload in `hack/demo-workload.yaml` pre-sets annotations so the demo works without cost-patcher. In production, run:

   ```bash
   make cluster-k3d-import-cost-patcher   # build + import image
   make cluster-install-kubefisher-cost  # install Helm chart
   ```

2. **`vllm:prompt_tokens_total` or `vllm:generation_tokens_total` counter is absent or not rising.** The `kubefisher:cost_per_token` recording rule requires `(rate(vllm:prompt_tokens_total[5m]) + rate(vllm:generation_tokens_total[5m])) > 0`. The rate window needs **at least 5 minutes** of rising counter data after the first scrape — expect `cost-per-token` to be absent for the first ~5 minutes after a workload starts serving requests.

   Verify in Prometheus:
   ```
   http://localhost:9090/graph?g0.expr=vllm%3Ageneration_tokens_total
   ```

3. **cost/hr is present but cost/token is `—`.** The cost-patcher writes `cost-per-hour` from DCGM metrics (available immediately) and `cost-per-token` only when the vLLM token counter is rising. This is expected on workloads that aren't using vLLM or haven't served a request yet.

To run the full end-to-end cost/token verification:

```bash
make cluster-e2e-cost-patcher-token
```

See [`docs/cost-patcher.md`](cost-patcher.md) for the full PromQL chain.

---

## How do I add a GPU type or change pricing?

GPU list prices live in a `ConfigMap` named `gpu-pricing` (key `pricing.yaml`) in the `kubefisher-system` namespace. The cost-patcher reads it on startup and exposes the prices as Prometheus gauges.

**Edit the ConfigMap directly:**

```bash
kubectl edit configmap gpu-pricing -n kubefisher-system
```

**Or patch a specific entry:**

```bash
kubectl patch configmap gpu-pricing -n kubefisher-system \
  --type=json \
  -p='[{"op":"replace","path":"/data/pricing.yaml","value":"gpus:\n  - match:\n      accelerator: nvidia-h100\n    pricePerHour: 3.20\n    currency: USD\n  - match:\n      accelerator: nvidia-a10g\n    pricePerHour: 1.50\n    currency: USD\n"}]'
```

The pricing YAML schema and all match fields are documented in [`docs/contract.md`](contract.md#gpu-pricing-schema). After updating, the cost-patcher picks up the new values within its next reconcile cycle (default 30s). You do not need to restart the cost-patcher pod.

**Adding a new GPU type** (e.g., `nvidia-a100`): add a new entry under `gpus:` with a `match` block that corresponds to one or more node labels. The `accelerator` key matches the node label `accelerator=<value>` applied by `make cluster-gpu` or your own node labelling.

---

## How does quota enforcement work?

`TeamInferenceQuota` is a namespaced CRD managed by the quota operator. It tracks token consumption (rolling 24 h) and cost spend (calendar month) from Prometheus, then transitions through four phases:

| Phase | Trigger | Enforcement |
|-------|---------|-------------|
| `Active` | Usage below alert threshold | Pods schedule normally |
| `Warning` | Usage ≥ `alertThresholdPct` (default 80%) | Pods schedule; team gets an event |
| `Exceeded` | Token or cost budget exceeded | New GPU pods **denied** (Enforce mode) or logged (Audit mode) |
| `Unknown` | Prometheus unreachable | Admission always **allows** (safe-open) |

**To enable enforcement for a namespace:**

```bash
# 1. Create the quota
kubefisher quota set \
  --name my-team \
  -n my-team \
  --daily-tokens 1000000 \
  --monthly-cost 500.00

# 2. Label the namespace (opts it into the webhook)
kubectl label namespace my-team kubefisher.io/quota-enforcement=enabled

# 3. Verify
kubefisher quota list -n my-team
```

**Enforcement vs Audit:**

- `--mode Enforce` (default): the validating webhook denies `CREATE` of new Pods that request `nvidia.com/gpu` when the namespace quota is `Exceeded`.
- `--mode Audit`: the webhook allows the Pod but records a Kubernetes Event. Use this to test budget limits without blocking teams.

**System namespaces are never enforced:** `kube-system`, `kubefisher-system`, and the Helm release namespace are always excluded regardless of labels.

The full verification runbook (5 commands): [`docs/verify-quota.md`](verify-quota.md).

---

## Why does `kubefisher cost` show no rows?

`kubefisher cost` includes a Deployment or StatefulSet only when it satisfies **at least one** of:

- Has a `kubefisher.io/cost-per-hour` or `kubefisher.io/cost-per-token` annotation.
- Has at least one container that requests `nvidia.com/gpu`.

For platform CRDs, the CRD must be installed in the cluster — they are discovered automatically when present:

- KServe `InferenceService`
- BentoML `BentoDeployment` (`serving.yatai.ai` or `serving.bento.ai`)
- Kubeflow Trainer `TrainJob` and legacy training jobs (`PyTorchJob`, `TFJob`, …)
- KubeRay `RayService`, `RayJob`, `RayCluster`

The cost-patcher writes annotations on GPU pods’ **owners** for KServe, BentoML, Kubeflow Trainer, and Ray Serve (when pods carry `ray.io/serve-deployment`). See [`docs/cost-patcher.md`](cost-patcher.md#platform-adapters-which-resource-is-annotated).

**Checklist when no rows appear:**

1. Confirm the namespace: `kubefisher cost -A` queries all namespaces; `-n <ns>` scopes to one.
2. Add contract labels and annotations to your workload:

   ```bash
   kubectl annotate deployment my-model \
     kubefisher.io/cost-per-hour="1.50" \
     kubefisher.io/cost-per-token="0.0000045"
   kubectl label deployment my-model \
     kubefisher.io/platform=vllm \
     kubefisher.io/model=my-model \
     kubefisher.io/team=my-team
   ```

3. Or install and run the cost-patcher — it writes these annotations automatically from DCGM + Prometheus.

---

## Why did `demo.sh` fail at `make cluster-up`?

**Docker is not running:**

```
FATA[0000] Failed to create cluster 'kubefisher-dev':...
```

Start Docker Desktop and re-run.

**Port conflict (3000 or 9090 already in use):**

```
Error: failed to create cluster: failed to ensure loadbalancer: ...
```

Check what is using the port:

```bash
lsof -i :3000
lsof -i :9090
```

Stop the conflicting process, or edit `config/cluster/observability/k3d-config.yaml` to use different host ports.

**k3d cluster already exists:**

```
FATA[0000] Failed to create cluster: cluster 'kubefisher-dev' already exists
```

Either run `make cluster-down` first, or pass `--skip-cluster` if the cluster is already running:

```bash
bash hack/demo.sh --skip-cluster
```

**k3d version too old:** The config uses k3d v5+ format. Upgrade: `brew upgrade k3d`.
