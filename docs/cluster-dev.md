# Local cluster dev (`k3d`) guide

Platform-wide label/annotation/metric contracts live in `docs/contract.md`.

This repo supports two local-dev scenarios:

- **Fake GPU capacity + mock DCGM metrics (default)**: works on most laptops; validates that your Prometheus/Grafana stack and queries are wired correctly.
- **Real GPU Operator + real DCGM exporter (later)**: requires a **Linux + NVIDIA GPU** environment with the NVIDIA container runtime/driver prerequisites.

## Prerequisites

### Required (both scenarios)

- **Docker** running (k3d runs Kubernetes-in-Docker)
- **kubectl**
- **helm**
- **k3d**
- **make**

### Required (real NVIDIA scenario only)

- **Linux host (or Linux nodes)** with an NVIDIA GPU
- NVIDIA driver + runtime prerequisites suitable for the [NVIDIA GPU Operator](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/)

On macOS Docker Desktop (and many non-NVIDIA environments), the GPU Operator’s driver/runtime components commonly fail (e.g., sysfs mount restrictions, missing `nvidia` runtime handler), which prevents DCGM from producing real metrics.

## What `make cluster-up` does (default: fake GPU + mock metrics)

`make cluster-up` creates a local k3d cluster and installs an observability stack that can be used on non-GPU machines.

### 1) Create k3d cluster

Uses `config/cluster/observability/k3d-config.yaml` (1 server + 2 agents) and exposes:

- Grafana at `http://localhost:3000`
- Prometheus at `http://localhost:9090`

### 2) Label GPU-ish nodes

`make cluster-up` labels the agent nodes:

- **agent-0**: `accelerator=nvidia-a10g`
- **agent-1**: `accelerator=nvidia-a10g` and `kubefisher.io/spot=true`

### 3) Patch fake GPU capacity (both agents)

`make cluster-up` patches the node status for `k3d-$(CLUSTER_NAME)-agent-0` and `k3d-$(CLUSTER_NAME)-agent-1` so they advertise:

- `status.capacity["nvidia.com/gpu"] = "2"`
- `status.allocatable["nvidia.com/gpu"] = "2"`

This is done using `kubectl proxy` + a JSON Patch against the Kubernetes API.

### 4) Install Prometheus + Grafana

Installs `prometheus-community/kube-prometheus-stack` with values from:

- `config/cluster/observability/helm/kube-prometheus-stack-values.yaml`

### 5) Install `dcgm-mock` (default)

Applies:

- `config/cluster/observability/manifests/dcgm-mock.yaml`

This exports DCGM-shaped metrics (including `DCGM_FI_DEV_GPU_UTIL`) without requiring any NVIDIA runtime.

### 6) Verify

`make cluster-up` finishes by running `make cluster-verify`, which:

- waits for Grafana and Prometheus to be reachable
- waits for the `dcgm-mock` scrape target to appear in Prometheus
- asserts `DCGM_FI_DEV_GPU_UTIL` is queryable via the Prometheus HTTP API

## Real NVIDIA GPU Operator + real DCGM exporter (optional)

If you have a compatible **Linux + NVIDIA** environment, you can install the real exporter.

### Install

Run:

```bash
make cluster-install-real-dcgm
```

This runs:

- `make cluster-install-gpu-operator`
- `make cluster-install-dcgm-exporter-monitor` (fallback `ServiceMonitor` for kube-prometheus-stack)

### Notes

- The repo includes a values file at `config/cluster/observability/helm/gpu-operator-values.yaml` for GPU Operator installs.
- If your environment doesn’t support the NVIDIA runtime/driver stack, the GPU Operator operands may deploy partially (operator/nfd) but DCGM exporter pods will not become healthy and Prometheus will not scrape real DCGM metrics.

## Useful commands

- **Recreate from scratch**: `make cluster-reset`
- **Tear down**: `make cluster-down`
- **Re-run verify**: `make cluster-verify`

## Optional: run a real vLLM CPU demo

If you want a “real server” demo (not the nginx mock), you can deploy `vllm/vllm-openai` on CPU with a small model:

- Apply: `make cluster-install-vllm-cpu-demo`
- Manifest: `config/cluster/serving/vllm/vllm-cpu-demo.yaml`

This is intentionally **not** part of `make cluster-up` because it can be slow/flaky (model download + startup time) compared to the deterministic mock.

## vLLM metrics (platform-agnostic serving metrics)

This repo includes **both** a real vLLM Deployment (GPU-oriented) and a deterministic metrics mock so you can validate scraping on non-GPU clusters.

### Deterministic vLLM metrics mock (recommended on k3d)

- **Mock workload**: `config/cluster/serving/vllm/vllm-mock-metrics.yaml` (nginx serving a static `/metrics`)
- **Scrape config**: `config/cluster/serving/vllm/vllm-mock-servicemonitor.yaml` (ServiceMonitor in `monitoring`)
- **Install**: `make cluster-install-vllm-mock`
- **Verify token series**: `make cluster-verify-vllm-mock`

This validates that Prometheus is scraping serving metrics and that the token counter series names match what KubeFisher expects.

### Real vLLM (GPU cluster)

- **Deployment + Service (plain K8s)**: `config/cluster/serving/vllm/vllm-deployment.yaml`
- **ServiceMonitor**: `config/cluster/serving/vllm/vllm-servicemonitor.yaml`

The real Deployment includes contract labels on the pod template:
- `kubefisher.io/platform=vllm`
- `kubefisher.io/model=<model-id>`
- `kubefisher.io/team=<team>`

## Cost patcher (optional)

To install the recording rules + example pricing + cost patcher workload (after observability is up):

```bash
make cost-patcher-image
make cluster-install-kubefisher-cost
```

Manifests are under **`deployments/kubernetes/`** (`deployments/README.md`). See the root **`README.md`** for the full layout.

### Cost patcher verification (e2e)

Prerequisites: `make cluster-up` (Prometheus reachable at `http://localhost:9090`), then either run the bundled e2e targets or the steps manually.

**Why two workloads**

- **`gpu-fake-workload`** (`deployments/kubernetes/test/gpu-fake-workload.yaml`): requests `nvidia.com/gpu` but does not run vLLM. Prometheus can still evaluate `kubefisher:cost_per_hour`, so the patcher should set **`kubefisher.io/cost-per-hour`** on the owning `Deployment` within roughly one reconcile interval (**30s**) plus scrape/evaluation delay. Use **`make cluster-e2e-cost-patcher-hour`** for a one-shot check (builds the image, `k3d image import`, applies rules + patcher + fake workload, asserts the annotation).
- **`vllm-costtoken-mock`** (`config/cluster/serving/vllm/vllm-costtoken-mock*.yaml`): deterministic token counter generator that requests a **fake GPU** so the cost patcher selects the pod. It exposes a monotonically increasing `vllm:generation_tokens_total`, so `rate(...[2m]) > 0`. This satisfies the `kubefisher:cost_per_token` recording rule in a k3d/fake-GPU environment. Use **`make cluster-e2e-cost-patcher-token`** (slow: includes rollout and a 130s wait for the PromQL rate window).

**Manual outline**

```bash
make cost-patcher-image
make cluster-k3d-import-cost-patcher
make cluster-install-kubefisher-cost
kubectl rollout status deploy/kubefisher -n kubefisher-system --timeout=180s

# cost / hour
make cluster-install-test-gpu-fake
EXPECT_HOUR=1 EXPECT_TOKEN=0 TIMEOUT_SEC=120 bash scripts/verify_cost_patcher.sh llm-inference gpu-fake-workload

# cost / token (after vLLM is ready + traffic + ~2m scrape window)
make cluster-install-vllm-cpu-costtest
sleep 130
EXPECT_HOUR=1 EXPECT_TOKEN=1 TIMEOUT_SEC=300 bash scripts/verify_cost_patcher.sh llm-inference vllm-costtoken-mock
```

**Run patcher outside the cluster (optional)**  
Build the binary, point it at port-forwarded Prometheus, and use the same kubeconfig:

```bash
kubectl -n monitoring port-forward svc/prometheus-kube-prometheus-prometheus 9090:9090 &
export PROMETHEUS_URL=http://127.0.0.1:9090
go run ./cmd/cost-patcher --pricing-namespace=kubefisher-system
```

## TeamInferenceQuota operator + `kubefisher` CLI (optional)

After **`make cluster-up`**, Prometheus is usually reachable at **`http://localhost:9090`**, which matches the operator manager default.

1. Install the CRD and run the controller (use a second terminal for the manager):

   ```bash
   cd operator && make install
   cd .. && make operator-run
   ```

2. Apply a sample quota and watch status:

   ```bash
   kubectl apply -f operator/config/samples/quota_v1alpha1_teaminferencequota.yaml
   kubectl get tiq -n default -w
   ```

3. Use the CLI to inspect quotas, costs, and workloads (same kubeconfig as **kubectl**):

   ```bash
   make kubefisher-build

   ./bin/kubefisher quota list -A        # phase + budget progress bars across all namespaces
   ./bin/kubefisher cost -A              # cost/hr and cost/token for all GPU workloads
   ./bin/kubefisher status <name>        # phase, endpoint, cost for a named workload
   ./bin/kubefisher logs  <name>         # stream logs (Ctrl-C safe)
   ```

   You can also create a quota and enable enforcement:

   ```bash
   ./bin/kubefisher quota set --name default -n default \
     --daily-tokens 500000 --monthly-cost 200.00
   kubectl label namespace default kubefisher.io/quota-enforcement=enabled
   ```

Full CLI reference: **[`docs/cli.md`](cli.md)**. Operator behavior, PromQL, and tests: **`docs/teaminferencequota-operator.md`**. **GPU admission verification:** **`docs/verify-quota.md`** (run first for quota/webhook support). How **`internal/`**, **`pkg/`**, and **`operator/`** relate: **`internal/README.md`**.

## Cleaning up local dev (without deleting k3d)

The root **`Makefile`** targets below remove workloads installed by the Make flows. They do **not** tear down kube-prometheus-stack or the k3d cluster (use **`make cluster-down`** for that).

- **`make cluster-clean-kubefisher-cost`** — Uninstalls Helm release **`kubefisher`** in **`kubefisher-system`** (cost-patcher, recording rules, pricing, dashboard from the chart).
- **`make cluster-clean-test-workloads`** — Deletes `llm-inference` test Deployments/Services/ConfigMaps (`gpu-fake-workload`, `vllm-mock`, `vllm-costtoken-mock`, `vllm-cpu-demo`) and **`monitoring`** `ServiceMonitor`s for the vLLM mocks.
- **`make cluster-clean-all-apps`** — Runs both of the above (common reset: remove chart + mocks, keep Prometheus/Grafana).
- **`make cluster-clean-operator-deploy`** — Undoes **`make -C operator deploy`** (manager + RBAC from kubebuilder `config/default`).
- **`make cluster-clean-operator-crds`** — Removes **TeamInferenceQuota** CRDs. **Delete every `TeamInferenceQuota` / `tiq` in all namespaces first**, or CRD deletion can fail.

**Cost-patcher Deployment name:** defaults to **`kubefisher`** (Helm release **`CHART_RELEASE`** in the root Makefile). **`make cluster-wait-cost-patcher`** waits on **`deploy/$(CHART_RELEASE)`**.
