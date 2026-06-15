# TeamInferenceQuota operator

Kubebuilder v4 project (nested Go module under `github.com/m2khosravi/kubefisher/operator`). It reconciles `TeamInferenceQuota` resources: every **60s** it queries Prometheus for rolling **24h** token usage and month-to-date **USD** cost, then patches **status** (merge patch) and emits **Events** on phase changes. A **validating webhook** blocks **new** GPU pod creation when quota is **Exceeded** and `enforcementMode` is **Enforce** (namespaces labeled `kubefisher.io/quota-enforcement=enabled`).

**Full blueprint (API, PromQL, phase rules, layout, troubleshooting, tests):** [`docs/teaminferencequota-operator.md`](../docs/teaminferencequota-operator.md) (same style as [`docs/cost-patcher.md`](../docs/cost-patcher.md)).

**Production install (operator + webhook TLS + CRD):** Helm chart [`charts/kubefisher/`](../charts/kubefisher/) with `operator.enabled=true`.

**Verify enforcement:** [`docs/verify-quota.md`](../docs/verify-quota.md) (support runbook). Policy/TLS/exclusions: [`docs/security.md`](../docs/security.md).

**Monorepo boundaries** (root module vs nested `operator/` module, `pkg/promclient`): [`internal/README.md`](../internal/README.md).

## Prerequisites

- Go 1.26+
- Prometheus reachable from the manager (same recording rules as cost-patcher: `vllm:generation_tokens_total` or legacy `vllm:num_generation_tokens_total`, `kubefisher:cost_per_hour`).

## Common commands

```bash
make manifests generate   # CRD + deepcopy + RBAC + webhook manifests
make test                 # unit tests + envtest (downloads kube apiserver binaries)
make run                  # local manager + webhook; uses --prometheus-url or PROMETHEUS_URL (default http://localhost:9090)
make install              # apply CRD to current cluster context
make deploy IMG=...       # CRD + RBAC + manager + webhook (kustomize default overlay)
make docker-build IMG=kubefisher/operator:dev   # image; build context is repo root (see Dockerfile)
```

From the repository root you can use `make operator-test`, `make operator-build`, `make operator-manifests`, or `make operator-run`.

## Sample

Apply [config/samples/quota_v1alpha1_teaminferencequota.yaml](config/samples/quota_v1alpha1_teaminferencequota.yaml), then:

```bash
kubectl get tiq -n default
```

**Notes:**

- Token status is a **rolling 24h** total from Prometheus, not a strict calendar “today”.
- **`Enforce`** (default): deny new GPU pods when `status.phase` is **Exceeded** (webhook; namespace must have `kubefisher.io/quota-enforcement=enabled`).
- **`Audit`**: allow GPU pods; controller and webhook emit **Normal** events with spend details when over budget.
- **`phase=Unknown`**: admission always allows (fail-open when Prometheus cannot be read).

## Cleanup (from repository root)

If you used **`make deploy`** here, remove the in-cluster manager with:

```bash
make cluster-clean-operator-deploy
```

To remove CRDs after deleting all `TeamInferenceQuota` objects:

```bash
make cluster-clean-operator-crds
```

