# deployments/kubernetes

This directory only holds **dev/test fixtures** that are intentionally not
part of the shipped Helm chart:

- `test/` — fake GPU-requesting workload used by the `cluster-e2e-cost-patcher-*`
  Make targets to verify cost-patcher annotations end-to-end on a local k3d cluster.

The canonical install path for KubeFisher cluster artifacts (cost-patcher,
RBAC, gpu-pricing, ServiceMonitor, PrometheusRule, Grafana dashboard) is the
Helm chart at [`../../charts/kubefisher/`](../../charts/kubefisher/).

Install on a cluster with kube-prometheus-stack already running:

```bash
helm install kubefisher ../../charts/kubefisher \
  -n kubefisher-system --create-namespace
```
