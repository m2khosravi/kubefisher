# Grafana dashboard (KubeFisher)

KubeFisher ships a pre-built Grafana dashboard that imports automatically
via the kube-prometheus-stack Grafana sidecar.

## Where it lives

- Dashboard JSON: `charts/kubefisher/dashboards/kubefisher-dashboard.json`
- Rendered ConfigMap (applied to the cluster via Helm):
  `charts/kubefisher/templates/configmap-grafana-dashboard.yaml`

The ConfigMap is labeled `grafana.sidecar.dashboards: "1"` (configurable via
`observability.grafanaDashboard.sidecarLabel{,Value}` in the chart values) so
the Grafana sidecar can pick it up. The chart loads the JSON from the chart
directory using `.Files.Get`, so the chart is self-contained when packaged.

To disable the dashboard ConfigMap (e.g. you manage dashboards out-of-band):

```bash
helm upgrade --install kubefisher ./charts/kubefisher \
  -n kubefisher-system --create-namespace \
  --set observability.grafanaDashboard.enabled=false
```

## What it shows (panels)

- **Cost per token (by model)**: `kubefisher:cost_per_token` grouped by `kubefisher_io_model`
- **GPU utilisation % (by node)**: `DCGM_FI_DEV_GPU_UTIL` grouped by Kubernetes node
- **Daily GPU spend (24h run-rate)**: uses `kubefisher:cost_per_hour` to compute a 24h run-rate
- **Cost per hour (by namespace)**: sums `kubefisher:cost_per_hour` by namespace
- **GPU list price per hour (by node)**: reads `kubefisher_gpu_price_per_hour_by_node` (debugging / sanity check)

## "No data" is expected sometimes

Grafana will show **No data** when the underlying metrics are absent.

Common examples:

- **Cost/token empty**: `kubefisher:cost_per_token` is best-effort and is only emitted when `rate(vllm:generation_tokens_total[2m]) > 0` (you need a rising token counter, and you need to wait for the 2-minute rate window).
- **GPU utilisation empty**: if DCGM is not installed/scraped yet, or if you're in a non-NVIDIA environment.
- **GPU price empty**: if the cost patcher `/metrics` endpoint isn't scraped or the `gpu-pricing` ConfigMap isn't applied.
