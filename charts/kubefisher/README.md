# kubefisher

GPU cost visibility and team quota platform for AI inference on Kubernetes.

This chart installs the **KubeFisher cost-patcher**, optional **TeamInferenceQuota
operator** (GPU admission webhook), and cluster-side observability artifacts.

**Template layout:** `templates/cost-patcher/` (cost-patcher workload + rules +
dashboard), `templates/operator/` (quota controller + webhook TLS).

Cost-patcher and observability:

- `cost-patcher` Deployment + Service (RBAC scoped to read pods/owners and
  patch annotations on top-level workload owners: `Deployment`/`StatefulSet`,
  KServe `InferenceService`, Yatai `BentoDeployment`, Kubeflow Trainer `TrainJob`,
  KubeRay `RayService` — see [`docs/cost-patcher.md`](../../docs/cost-patcher.md))
- `gpu-pricing` ConfigMap (the pricing payload the patcher exposes to
  Prometheus as `kubefisher_gpu_price_per_hour_by_node`)
- `ServiceMonitor` for the cost-patcher `/metrics` endpoint
- `PrometheusRule` with the `kubefisher:cost_per_hour` /
  `kubefisher:cost_per_token` / `kubefisher:gpu_efficiency_pct`
  recording rules
- `ConfigMap` carrying the pre-built KubeFisher Grafana dashboard JSON
  (auto-imported by the kube-prometheus-stack Grafana sidecar)

## Prerequisites

- Kubernetes 1.25+
- **cluster-admin** permissions (chart creates `ClusterRole`, `ClusterRoleBinding`,
  `ValidatingWebhookConfiguration`, and installs a CRD)
- `kube-prometheus-stack` (or any Prometheus Operator install) already
  running in the cluster — the chart’s defaults assume the
  release name `prometheus` in the `monitoring` namespace
- **cert-manager** — required when `operator.webhook.certManager.enabled: true`
  (the default). The chart renders `cert-manager.io/v1` `Issuer` and `Certificate`
  objects; `helm install` will fail if cert-manager CRDs are absent. Install
  cert-manager first or disable with
  `--set operator.webhook.certManager.enabled=false` (then supply TLS manually).
- A running DCGM exporter (real GPU clusters: NVIDIA GPU Operator;
  local dev: `dcgm-mock` from `config/cluster/observability/`)

## Install

```bash
# k3d/kind: image is local-only — import after build (see Makefile targets)
make operator-image
make cluster-k3d-import-operator
make cluster-install-kubefisher   # cost-patcher + operator + webhook
```

Or Helm only (cluster must already have the image, or use a registry tag):

```bash
helm install kubefisher ./charts/kubefisher \
  -n kubefisher-system --create-namespace \
  --set operator.enabled=true \
  --set operator.webhook.enabled=true \
  --set operator.image.pullPolicy=IfNotPresent
```

**Verify GPU quota enforcement:** [docs/verify-quota.md](../../docs/verify-quota.md) (five commands, under 5 minutes). Policy details: [docs/security.md](../../docs/security.md).

To upgrade with a custom GPU pricing file:

```bash
helm upgrade --install kubefisher ./charts/kubefisher \
  -n kubefisher-system --create-namespace \
  --set-file gpuPricing.pricingYAML=./my-pricing.yaml   # (or edit values.yaml)
```

## Uninstall

```bash
helm uninstall kubefisher -n kubefisher-system
```

## Values

See [values.yaml](values.yaml) for the full reference (every key is
documented inline). Highlights:

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `ghcr.io/m2khosravi/kubefisher/cost-patcher` | Cost-patcher image |
| `image.tag` | `""` (→ `Chart.AppVersion`) | Image tag; set `dev` for local k3d builds |
| `config.prometheusUrl` | `http://prometheus-kube-prometheus-prometheus.monitoring.svc:9090` | Prometheus base URL |
| `config.reconcileInterval` | `30s` | Annotation reconcile cadence |
| `gpuPricing.create` | `true` | Whether to render the `gpu-pricing` CM |
| `gpuPricing.pricing` | A10G placeholder | YAML pricing payload (see `docs/contract.md`) |
| `observability.serviceMonitor.enabled` | `true` | Render the cost-patcher `ServiceMonitor` |
| `observability.serviceMonitor.additionalLabels.release` | `prometheus` | Must match your kube-prometheus-stack `serviceMonitorSelector` |
| `observability.prometheusRule.enabled` | `true` | Render the recording rules |
| `observability.grafanaDashboard.enabled` | `true` | Render the dashboard ConfigMap |

## Activating quota enforcement

The validating webhook only fires on namespaces that carry the enforcement label.
After installing the chart, label each namespace you want to govern:

```bash
kubectl label namespace <team-ns> kubefisher.io/quota-enforcement=enabled
```

See [`docs/security.md`](../../docs/security.md) for exclusion lists and policy
details, and [`docs/verify-quota.md`](../../docs/verify-quota.md) for the
5-command verification runbook.

## Grafana sidecar label

The dashboard ConfigMap is labelled `grafana.sidecar.dashboards: "1"` by default.
If your kube-prometheus-stack uses a different sidecar label key (check
`grafana.sidecar.dashboards.label` in your kube-prometheus-stack values), override
with `--set observability.grafanaDashboard.sidecarLabel=<your-label-key>`.

## Compatibility with non-default Prometheus releases

If your kube-prometheus-stack release is named differently than `prometheus`,
override the `release` label so Prometheus selects the resources:

```bash
helm install kubefisher ./charts/kubefisher \
  -n kubefisher-system --create-namespace \
  --set observability.serviceMonitor.additionalLabels.release=my-stack \
  --set observability.prometheusRule.additionalLabels.release=my-stack \
  --set config.prometheusUrl=http://my-stack-kube-prometheus-prometheus.monitoring.svc:9090
```

## Air-gapped / no-Prometheus installs

You can disable every integration and run only the cost-patcher controller:

```bash
helm install kubefisher ./charts/kubefisher \
  -n kubefisher-system --create-namespace \
  --set observability.serviceMonitor.enabled=false \
  --set observability.prometheusRule.enabled=false \
  --set observability.grafanaDashboard.enabled=false
```

## Source

- Repo: <https://github.com/m2khosravi/kubefisher>
- Contract / labels / annotations / pricing schema: `docs/contract.md`
