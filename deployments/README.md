# Deployments

The canonical install path for **KubeFisher-owned** cluster artifacts is the
Helm chart at **[`charts/kubefisher/`](../charts/kubefisher/)**. It renders:

- ServiceAccount + RBAC for the cost-patcher (cluster-scoped read for
  pods/owners, namespace-scoped read for `gpu-pricing`)
- cost-patcher Deployment + Service
- `gpu-pricing` ConfigMap
- `ServiceMonitor` for cost-patcher `/metrics`
- `PrometheusRule` with the `kubefisher:cost_per_hour` /
  `kubefisher:cost_per_token` / `kubefisher:gpu_efficiency_pct`
  recording rules
- Grafana dashboard ConfigMap (auto-imported by the kube-prometheus-stack
  Grafana sidecar)

The **TeamInferenceQuota operator** (CRD, controller, validating webhook + cert-manager TLS) ships under **`templates/operator/`** when `operator.enabled=true`. Verify enforcement: **`docs/verify-quota.md`**. See **`docs/teaminferencequota-operator.md`** and **`docs/security.md`**.

To remove chart-installed resources from a dev cluster without deleting k3d: **`make cluster-clean-kubefisher-cost`** (and **`make cluster-clean-test-workloads`** for `llm-inference` mocks). See **`docs/cluster-dev.md`**.

Each observability resource is gated by a value (`observability.<thing>.enabled`)
so the chart degrades cleanly on clusters without Prometheus Operator.

Install:

```bash
helm install kubefisher ./charts/kubefisher \
  -n kubefisher-system --create-namespace
```

Or, for local k3d dev (uses the locally-built `kubefisher/cost-patcher:dev`
image; run `make cluster-k3d-import-cost-patcher` first):

```bash
make cluster-install-kubefisher-cost
```

What stays under `deployments/kubernetes/`:

| Path | Contents |
|------|-----------|
| `deployments/kubernetes/test/` | Optional workloads for automated checks (fake GPU) |

The **local dev cluster** stack (k3d, kube-prometheus-stack, DCGM mock,
vLLM demos) lives under **`config/cluster/`**; those are dev-only and are
**not** part of the chart.
