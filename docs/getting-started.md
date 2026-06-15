# Getting started with KubeFisher

This guide takes you from zero to a live cost table and quota in under 10 minutes using a local k3d cluster — no GPU hardware, no cloud account, no Docker image build.

---

## Prerequisites

Install these before you begin:

| Tool | Version tested | Install |
|------|---------------|---------|
| [k3d](https://k3d.io) | ≥ 5.6 | `brew install k3d` / [k3d.io/#installation](https://k3d.io/#installation) |
| [kubectl](https://kubernetes.io/docs/tasks/tools/) | ≥ 1.28 | `brew install kubectl` |
| [helm](https://helm.sh/docs/intro/install/) | ≥ 3.14 | `brew install helm` |
| [Go](https://go.dev/dl/) | ≥ 1.22 | `brew install go` |
| make | any | `xcode-select --install` (macOS) / `apt install build-essential` |
| Docker Desktop | running | Required by k3d |

**Optional** (needed only for real cost-patcher metrics):

- Docker engine access to build local images (`make cost-patcher-image`)

---

## Step 1 — Clone and build the CLI

```bash
git clone https://github.com/m2khosravi/kubefisher
cd kubefisher
make kubefisher-build
```

Verify:

```
$ ./bin/kubefisher version
kubefisher: dev
operator:    (not installed in namespace "kubefisher-system")
```

The operator line is expected — we haven't installed it yet.

---

## Step 2 — Create the local cluster

```bash
make cluster-up
```

This takes 4–6 minutes on a first run (Helm chart pulls). What it does:

1. Creates a k3d cluster (`kubefisher-dev`) with 1 server + 2 agents.
2. Patches fake GPU capacity on both agent nodes (`nvidia.com/gpu=2`).
3. Installs `kube-prometheus-stack` (Prometheus + Grafana).
4. Installs the DCGM mock exporter.

When complete:

```
✓ Prometheus: http://localhost:9090
✓ Grafana:    http://localhost:3000  (admin / admin)
```

---

## Step 3 — Run the demo

```bash
bash hack/demo.sh
```

The script runs 8 steps automatically. Expected output:

```
[STEP 1/8] Checking prerequisites
  ✓ k3d version v5.7.4
  ✓ kubectl version v1.30.2
  ✓ helm version v3.15.1
  ✓ go go1.22.4
  ✓ make GNU Make 3.81

[STEP 2/8] Skipping cluster creation (already done)
  → Using existing cluster context: k3d-kubefisher-dev

[STEP 3/8] Building kubefisher CLI
  ✓ Binary: kubefisher: v0.1.0-3-gabcdef

[STEP 4/8] Applying demo workload (facebook/opt-125m, CPU-only)
  → Waiting for rollout…
  ✓ Deployment opt-125m is ready

[STEP 5/8] Running: kubefisher cost -A

  NAMESPACE  NAME      PLATFORM  TYPE       REPLICAS  COST/HR (REPLICA)  COST/HR (TOTAL)  COST/TOKEN   MONTHLY-EST
  demo       opt-125m  vllm      inference  1         $0.45/hr           $0.45/hr         $0.0000123   $324.00

  Total: $0.45/hr · Est. $324/mo

  ✓ The opt-125m row shows cost/hr and cost/token from annotations.

[STEP 6/8] Creating TeamInferenceQuota for the demo namespace
  → Skipped (operator not installed)

[STEP 7/8] Labelling namespace for quota enforcement
  → Skipped (operator not installed)

[STEP 8/8] Running: kubefisher quota list -A
  No quotas found. Run: kubefisher quota set --help

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Demo complete in 47s.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

The cost values (`$0.45/hr`, `$0.0000123/token`) come from annotations on the demo Deployment — exactly what the real cost-patcher writes. See [Step 5 — Real cost-patcher](#step-5--real-cost-patcher-optional) to generate live values.

---

## Step 4 — Explore the CLI

All commands use the same kubeconfig as `kubectl`.

### Watch costs live

```bash
./bin/kubefisher cost -A --watch
```

Refreshes every 10 seconds. Press Ctrl-C to exit.

### Deploy a model

```bash
./bin/kubefisher deploy --model facebook/opt-125m --gpu a10g -n demo --dry-run
```

`--dry-run` prints the YAML that would be created. Remove it to actually deploy. The CLI detects KServe if installed and falls back to a plain vLLM Deployment.

### Check workload status

```bash
./bin/kubefisher status opt-125m -n demo
```

Output:

```
Name:          opt-125m
Namespace:     demo
Phase:         Running
WorkloadType:  inference
Platform:      vllm
Replicas:      1/1
Cost/hr:       $0.45
Cost/token:    $0.0000123
Last-updated:  2026-01-01T00:00:00Z
```

### Tail logs

```bash
./bin/kubefisher logs opt-125m -n demo
```

Press Ctrl-C to stop streaming. The log stream closes cleanly.

### Set and inspect quotas

```bash
# Install the operator first (see below) then:
./bin/kubefisher quota set \
  --name demo \
  -n demo \
  --daily-tokens 100000 \
  --monthly-cost 50.00

./bin/kubefisher quota list -A
```

The list table shows coloured phase (Active / Warning / Exceeded) and ASCII progress bars for token and cost remaining.

---

## Step 5 — Real cost-patcher (optional)

The demo uses pre-annotated values. To generate live `cost/hr` and `cost/token` from real Prometheus metrics:

```bash
# Build and import the cost-patcher image into k3d (~2 min)
make cluster-k3d-import-cost-patcher

# Install the kubefisher chart (cost-patcher + recording rules)
make cluster-install-kubefisher-cost

# Apply the token counter mock (a tiny Python server; uses fake GPU)
make cluster-install-vllm-cpu-costtest

# Wait 130 s for the Prometheus rate window then verify
sleep 130
make cluster-verify-cost-annotation-token

# Now cost/token shows a real value
./bin/kubefisher cost -A
```

This is the same flow as `make cluster-e2e-cost-patcher-token`. See [`docs/cost-patcher.md`](cost-patcher.md) for how cost is computed.

---

## Step 6 — Install the quota operator (optional)

```bash
# Build and import both images
make cluster-k3d-import-cost-patcher cluster-k3d-import-operator

# Install chart with operator + webhook enabled
make cluster-install-kubefisher

# Run the demo again — steps 6-8 will now fully execute
bash hack/demo.sh --skip-cluster
```

Then verify quota enforcement with the runbook in [`docs/verify-quota.md`](verify-quota.md).

---

---

## Installing on a real cluster

This section covers installing KubeFisher on an existing Kubernetes cluster that
already has `kube-prometheus-stack` running. The k3d Makefile targets above are
**not** used here.

### Prerequisites checklist

Before running `helm install`, confirm every item:

| Requirement | How to verify |
|-------------|--------------|
| Kubernetes ≥ 1.25 | `kubectl version` |
| **cluster-admin** permissions | `kubectl auth can-i create clusterroles --all-namespaces` → `yes` |
| kube-prometheus-stack installed | `kubectl get prometheuses -A` → at least one resource |
| **cert-manager** installed | `kubectl get crds certificates.cert-manager.io` → present |
| Helm ≥ 3.14 | `helm version` |

> **cert-manager is required by default.** The chart renders `cert-manager.io/v1`
> `Issuer` and `Certificate` objects for webhook TLS. `helm install` will fail
> immediately if cert-manager CRDs are absent. Install cert-manager first:
> `helm install cert-manager jetstack/cert-manager -n cert-manager --create-namespace --set crds.enabled=true`

> **cluster-admin is required.** The chart creates two `ClusterRole` +
> `ClusterRoleBinding` objects, a `ValidatingWebhookConfiguration`, and installs
> the `teaminferencequotas.quota.kubefisher.io` CRD from the chart's `crds/`
> directory. Helm applies CRDs automatically before any templates render.

---

### Step A — Prepare gpu-pricing for your nodes

The default `gpu-pricing` ConfigMap matches `accelerator=nvidia-a10g` labels
(the fake k3d label). Real clusters use different node labels. Edit the pricing
before installing:

```bash
# Copy the example and edit it for your node labels
cp configs/gpu-pricing.example.yaml my-pricing.yaml
# Edit: set match.node_labels to labels that exist on your GPU nodes, and
#        price_per_gpu_hour to your cloud provider's list price.
# Example: kubectl get node <gpu-node> --show-labels
```

See [`configs/gpu-pricing.example.yaml`](../configs/gpu-pricing.example.yaml)
for the spot/on-demand precedence pattern and [`docs/contract.md`](contract.md)
for the full schema.

If the pricing labels do not match any node, the recording rules produce no
output and all cost annotations stay `—`. This looks identical to the
cost-patcher not running.

---

### Step B — Identify your Prometheus release name and namespace

The chart defaults to:
- `config.prometheusUrl`: `http://prometheus-kube-prometheus-prometheus.monitoring.svc:9090`
- `observability.serviceMonitor.additionalLabels.release: prometheus`
- `observability.prometheusRule.additionalLabels.release: prometheus`

These assume kube-prometheus-stack was installed with release name `prometheus`
in namespace `monitoring`. If yours differs:

```bash
# Find your Prometheus release name
helm list -A | grep prometheus

# Find the Prometheus service URL
kubectl get svc -A | grep prometheus
```

---

### Step C — Install the chart

Cost-patcher only (no quota enforcement):

```bash
helm install kubefisher ./charts/kubefisher \
  -n kubefisher-system --create-namespace \
  --set-file gpuPricing.pricing=my-pricing.yaml \
  --set config.prometheusUrl=http://<your-prometheus-svc>.<namespace>.svc:9090 \
  --set observability.serviceMonitor.additionalLabels.release=<your-stack-release> \
  --set observability.prometheusRule.additionalLabels.release=<your-stack-release> \
  --wait
```

With the quota operator and webhook (requires cert-manager):

```bash
helm install kubefisher ./charts/kubefisher \
  -n kubefisher-system --create-namespace \
  --set operator.enabled=true \
  --set operator.webhook.enabled=true \
  --set-file gpuPricing.pricing=my-pricing.yaml \
  --set config.prometheusUrl=http://<your-prometheus-svc>.<namespace>.svc:9090 \
  --set observability.serviceMonitor.additionalLabels.release=<your-stack-release> \
  --set observability.prometheusRule.additionalLabels.release=<your-stack-release> \
  --wait
```

`--wait` blocks until both Deployments are ready and cert-manager has issued
the webhook TLS certificate. Without it the operator may crash-loop briefly
while the certificate Secret is provisioned.

---

### Step D — Verify

```bash
# Both pods should be Running
kubectl get pods -n kubefisher-system

# Cost-patcher is reading Prometheus (check logs for connection errors)
kubectl logs -n kubefisher-system deploy/kubefisher -f --tail=20

# Recording rules are loaded (check Prometheus UI → Status → Rules)
# or:
kubectl get prometheusrules -n kubefisher-system

# After ~30-60s, GPU workloads should gain cost annotations
kubectl get deployments -A -o json | \
  python3 -c "import json,sys; [print(d['metadata']['namespace'], d['metadata']['name'], d['metadata'].get('annotations',{}).get('kubefisher.io/cost-per-hour-per-replica','—')) for d in json.load(sys.stdin)['items']]"
```

---

### Step E — Activate quota enforcement (operator only)

The webhook only intercepts Pod CREATE in namespaces that carry the enforcement
label. Label each namespace you want to govern:

```bash
kubectl label namespace <team-namespace> kubefisher.io/quota-enforcement=enabled
```

Then create a quota:

```bash
kubefisher quota set --name <team> -n <team-namespace> \
  --daily-tokens 1000000 --monthly-cost 500.00
kubefisher quota list -A
```

Run the 5-command verification runbook: [`docs/verify-quota.md`](verify-quota.md).

---

### Grafana dashboard

The dashboard ConfigMap is labelled `grafana.sidecar.dashboards: "1"` by default.
If your Grafana sidecar uses a different label key (set via
`grafana.sidecar.dashboards.label` in your kube-prometheus-stack values), pass:

```bash
--set observability.grafanaDashboard.sidecarLabel=<your-label-key>
```

---

## Next steps

| Document | What it covers |
|----------|---------------|
| [`docs/cli.md`](cli.md) | All commands, flags, and workflows |
| [`docs/cost-patcher.md`](cost-patcher.md) | How cost/hr and cost/token are computed |
| [`docs/teaminferencequota-operator.md`](teaminferencequota-operator.md) | TeamInferenceQuota CRD and reconciler |
| [`docs/verify-quota.md`](verify-quota.md) | 5-command quota enforcement verification |
| [`docs/faq.md`](faq.md) | Common questions and fixes |
| [`CONTRIBUTING.md`](../CONTRIBUTING.md) | How to contribute |
| [`docs/releasing.md`](releasing.md) | How to cut a release and publish images to ghcr.io |
