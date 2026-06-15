# TeamInferenceQuota operator (end-to-end)

This document explains the **KubeFisher TeamInferenceQuota** operator: the CRD API, Prometheus-backed spend evaluation, status updates, events, code layout, deployment artifacts, verification, and tests.

If anything here conflicts with `docs/contract.md`, **`docs/contract.md` wins**.

For the sibling component that **writes cost annotations** on workloads, see **`docs/cost-patcher.md`**.

---

## What it does

On a **~60s** reconcile interval (via `RequeueAfter`, not error backoff), the operator:

1. Loads the **`TeamInferenceQuota`** custom resource in its namespace (`metadata.namespace` is the budgeted namespace; there is no separate `spec.namespace`).
2. Validates **`spec.monthlyCostLimitUSD`** as a parseable non-negative decimal string (e.g. `"500.00"`).
3. Queries Prometheus with **instant queries** (shared **`pkg/promclient`**):
   - **Rolling 24h generation tokens** (not strict calendar “midnight today”):

     `sum(increase(vllm:generation_tokens_total{namespace="<ns>"}[24h])) or sum(increase(vllm:num_generation_tokens_total{namespace="<ns>"}[24h]))` (prefers **`vllm:generation_tokens_total`**, same as repo mocks / recording rules; **`num_generation`** is a fallback for older scrapes)

   - **Month-to-date USD cost (UTC calendar month)**, treating absent `kubefisher:cost_per_hour` as **zero spend** (not an error):

     `sum(kubefisher:cost_per_hour{namespace="<ns>"}) / 3600 * <hoursSinceMonthStartUTC>`

     where `<hoursSinceMonthStartUTC>` is computed in Go from “now” in **UTC**.

4. Derives **`status.phase`** in this **evaluation order**: **`Unknown`** if Prometheus failed; else **`Exceeded`** if over token or cost budget; else **`Warning`** if either utilization ≥ **`alertThresholdPct`**; else **`Active`**.
5. **Merge-patches** only **`status`** (`client.MergeFrom` + `r.Status().Patch` — avoids optimistic-locking noise from `Status().Update`).
6. Emits a **Kubernetes Event** on **phase transitions** (skips the noisy first transition `"" → Active`). **`Exceeded`** and **`Unknown`** use **`Warning`** events; recoveries use **`Normal`** events. Messages include spend and **`status.nextResetTime`** (start of next calendar month UTC, for cost messaging).
7. When **`spec.enforcementMode: Audit`**, emits an additional **`Normal`** event on transitions to **`Warning`** or **`Exceeded`** (controller reconcile; admission still allows pods).

**GPU pod admission (validating webhook):**

A **ValidatingWebhook** on **`Pod` CREATE** (core/v1) enforces budgets for pods that request **`nvidia.com/gpu`** when a **`TeamInferenceQuota`** exists in the same namespace:

| Condition | Admission |
|-----------|-----------|
| Pod does not request GPU | Allow (fast path) |
| No `TeamInferenceQuota` in namespace | Allow (quota is opt-in) |
| List `TeamInferenceQuota` fails | Allow (fail-open) |
| `status.phase` is **`Unknown`** | Allow (cannot measure; Prometheus down) |
| Phase **`Active`** or **`Warning`** | Allow |
| Phase **`Exceeded`** + **`Audit`** | Allow + **`Normal`** event on the quota |
| Phase **`Exceeded`** + **`Enforce`** | **Deny** with spend summary and `kubectl edit` command |

- **Existing running pods** are never touched (`ValidateUpdate` / `ValidateDelete` always allow).
- Webhook **`failurePolicy: Ignore`** so a crashed webhook does not block the cluster.
- Enforcement is **opt-in per namespace**: label `kubefisher.io/quota-enforcement=enabled`.
- **Excluded namespaces** (never enforced, even if labeled): `kube-system`, `kubefisher-system`, `operator-system` (kustomize), plus the Helm release namespace. See **[security.md](security.md)** and **[verify-quota.md](verify-quota.md)**.

**Install paths:** Production — **`charts/kubefisher/`** with `operator.enabled=true` (CRD in `charts/kubefisher/crds/`, webhook + cert-manager TLS in `templates/operator/`). Local dev — kubebuilder **`operator/config/`** + **`make install`** / **`make operator-run`**.

---

## API (`TeamInferenceQuota`)

| Area | Detail |
|------|--------|
| **Group / version** | `quota.kubefisher.io/v1alpha1` |
| **Scope** | **Namespaced** (one object per namespace you care about, or multiple policies if you extend later). |
| **Short name** | `kubectl get tiq` |
| **Spec** | `dailyTokenBudget` (int64), `monthlyCostLimitUSD` (decimal string), optional `alertThresholdPct` (default **80** in reconcile if unset/out of range), optional `enforcementMode` **`Enforce`** / **`Audit`** (default **`Enforce`**: deny new GPU pods when **`Exceeded`**; **`Audit`**: observe only at admission, allow pods + events). |
| **Status** | `phase`, `tokensUsedToday`, `costUsedThisMonth`, `tokenBudgetRemainingPct`, `costBudgetRemainingPct`, `lastEvaluationTime`, `nextResetTime`, `conditions` (`SpecValid`, `PrometheusReachable`). |
| **Print columns** | NAMESPACE, PHASE, TOKENS-USED, BUDGET, COST-USED, LIMIT, MODE, AGE (see type markers in `operator/api/v1alpha1/teaminferencequota_types.go`). |

Sample manifest: **`operator/config/samples/quota_v1alpha1_teaminferencequota.yaml`**.

OpenAPI validation (CRD) includes a **pattern** on `monthlyCostLimitUSD`; runtime reconcile still validates parse errors and surfaces **`Unknown`** + conditions.

---

## Prometheus dependencies

The operator assumes the same observability stack as cost visibility:

- **Tokens**: counter **`vllm:generation_tokens_total`** (or legacy **`vllm:num_generation_tokens_total`**) with a **`namespace`** label on series (see `docs/metrics/vllm_token_counters.md` if your scrape renames metrics).
- **Cost**: recording rule gauge **`kubefisher:cost_per_hour{namespace,...}`** (see **`docs/cost-patcher.md`** and **`charts/kubefisher/templates/prometheusrule.yaml`**).

If recording rules or scrape configs differ in your cluster, adjust PromQL in **`operator/internal/controller/teaminferencequota_controller.go`** or add recording rules that match your labels.

---

## Phase logic (priority)

Implemented in **`operator/internal/controller/phase.go`** (`computePhase`):

1. **`Unknown`** — Prometheus client missing, Prometheus **query error**, or invalid monthly limit string.
2. **`Exceeded`** — tokens ≥ daily budget **or** month-to-date cost ≥ monthly USD limit. **Daily budget 0** with **any** tokens > 0 counts as exceeded.
3. **`Warning`** — either utilization (tokens or cost) ≥ **`alertThresholdPct`** without having exceeded.
4. **`Active`** — otherwise.

Table-driven unit tests: **`operator/internal/controller/phase_test.go`**.

---

## Go code layout (where things live)

The operator is a **nested Go module** so it can use kubebuilder’s layout without merging `go.mod` into the repo root. Boundaries between **`internal/`**, **`pkg/`**, and **`operator/`** are summarized in **`internal/README.md`** (root module vs nested module).

- **`operator/`** — module `github.com/m2khosravi/kubefisher/operator` with `replace github.com/m2khosravi/kubefisher => ../` for **`github.com/m2khosravi/kubefisher/pkg/promclient`** only (Go **`internal/`** packages cannot be imported from another module).
- **`operator/cmd/main.go`** — controller-runtime **Manager**, flags (`--prometheus-url` / **`PROMETHEUS_URL`**, default **`http://localhost:9090`**), constructs **`promclient.Client`** and **`record.EventRecorder`**, registers reconciler and **Pod GPU quota webhook**.
- **`operator/api/v1alpha1/`** — CRD types, `AddToScheme`, deepcopy generated.
- **`operator/internal/controller/`** — **`TeamInferenceQuotaReconciler`**, phase helpers, audit phase events, envtest + httptest Prometheus stub for integration-style tests.
- **`operator/internal/webhook/`** — **`PodGPUQuotaValidator`** (`pod_webhook.go`): GPU detection, quota list, enforce/audit admission, denial message formatting.
- **`operator/config/webhook/`** — `ValidatingWebhookConfiguration`, webhook **Service**, namespace selector patch (kustomize).
- **`pkg/promclient/`** — minimal **instant query** client (`QueryInstant`); shared with **`internal/costpatcher/`**.
- **`operator/config/`** — kubebuilder **CRD bases**, **RBAC**, **kustomize** overlays (`default`, `manager`, `prometheus`, `rbac`, etc.).

Repo-level shortcuts (root **`Makefile`**):

- `make operator-test` — `make -C operator test` (controller-gen + **envtest**).
- `make operator-build` / `make operator-manifests` / `make operator-run`.

Operator-centric README: **`operator/README.md`**.

---

## Deployment and install

**CRD + RBAC + Deployment** are generated under **`operator/config/`** (kubebuilder defaults).

Typical dev flow:

```bash
cd operator
make install          # kubectl apply CRDs (needs current kube context)
make deploy IMG=...   # optional: full kustomize deploy (set image)
```

Local controller against an existing cluster + Prometheus:

```bash
make operator-run
# or: cd operator && make run
```

**Undo in-cluster operator** (if you ran **`make -C operator deploy`**):

```bash
make cluster-clean-operator-deploy
```

**Remove CRDs** (after deleting all `TeamInferenceQuota` objects):

```bash
make cluster-clean-operator-crds
```

For a full local reset of chart + test workloads (keeps k3d + Prometheus): **`make cluster-clean-all-apps`** (see **`docs/cluster-dev.md`**).

Webhook TLS is wired via kubebuilder defaults under **`operator/config/default/`** (`../webhook` resource + **`manager_webhook_patch.yaml`** exposes port **9443**). For production installs, prefer the Helm chart (`charts/kubefisher/`) which provisions **cert-manager** Issuer/Certificate, webhook Service, and `ValidatingWebhookConfiguration` with documented **`failurePolicy: Ignore`** — see **[security.md](security.md)**.

**Enable enforcement on a namespace:**

```bash
kubectl label namespace <your-ns> kubefisher.io/quota-enforcement=enabled
```

Helm: `operator.enabled=true` and `operator.webhook.enabled=true` on `charts/kubefisher/`. Kustomize dev: `kubectl apply -k operator/config/default`.

**Verify:** [verify-quota.md](verify-quota.md) (five commands, support runbook).

**Example denial message** (when **`Enforce`** + **`Exceeded`**):

```text
GPU pod admission denied: namespace "llm" exceeded TeamInferenceQuota "team-quota" (phase Exceeded). Tokens (rolling 24h): 2,000,000 of 1,000,000 daily budget. Cost (month-to-date, UTC): $600.00 of $500.00 monthly limit. Next reset: 2026-06-01 00:00 (2026-06-01T00:00:00Z UTC). To adjust limits, run: kubectl edit teaminferencequota team-quota -n llm
```

---

## Local verification

**Quick enforcement check:** [verify-quota.md](verify-quota.md).

Prerequisites for manual reconcile testing: a cluster with the **CRD installed**, Prometheus reachable from where the manager runs, and the metrics/recording rules above present.

1. Apply a quota object (edit namespace/budgets as needed):

   ```bash
   kubectl apply -f operator/config/samples/quota_v1alpha1_teaminferencequota.yaml
   ```

2. Run the manager (or deploy it) with **`--prometheus-url`** pointing at your Prometheus.

3. Wait at least one reconcile interval (~**60s**) plus scrape/evaluation skew, then:

   ```bash
   kubectl get tiq -n <namespace> -o wide
   kubectl describe teaminferencequota <name> -n <namespace>   # Events + conditions
   ```

You should see **`PHASE`**, **`TOKENS-USED`**, **`COST-USED`**, **`LIMIT`**, **`MODE`**, and timestamps update without manual status edits.

### kubefisher CLI (optional)

The repo ships **`kubefisher`** — a **Cobra** + **client-go** CLI (same kubeconfig as **kubectl**) covering install, cost, deploy, status, logs, and quota. It does **not** use controller-runtime.

```bash
make kubefisher-build

# List quotas with phase colours and budget progress bars
./bin/kubefisher quota list -A
./bin/kubefisher quota list -n team-a -o json

# Get a single quota
./bin/kubefisher quota get teaminferencequota-sample -n default -o yaml

# Create or update a quota (idempotent via Server-Side Apply)
./bin/kubefisher quota set --name team-a -n team-a \
  --daily-tokens 1000000 --monthly-cost 500.00
```

The `quota list` table includes **TOKEN-REM** and **COST-REM** columns showing ASCII progress bars sourced from `status.tokenBudgetRemainingPct` and `status.costBudgetRemainingPct` (written by the operator on every reconcile). Phase colours (green=Active, yellow=Warning, red=Exceeded) apply when stdout is a TTY.

Full CLI reference: **[`docs/cli.md`](cli.md)**. Code lives under **`internal/cli/kubefisher/`**; see **`internal/README.md`** for how the CLI fits next to cost-patcher and the operator.

---

## Common troubleshooting

- **`phase=Unknown`, tokens/cost idle**:
  - confirm **`PROMETHEUS_URL`** / **`--prometheus-url`** from the manager pod can reach Prometheus (DNS, network policy, TLS).
  - check manager logs for query failures; **`PrometheusReachable`** condition should flip **False** with a reason.
- **`tokensUsedToday` always 0**:
  - verify **`vllm:generation_tokens_total`** (or legacy **`vllm:num_generation_tokens_total`**) exists in Prometheus for that namespace and the **`increase(...[24h])`** window has non-zero range (new clusters need time in the 24h window).
- **`costUsedThisMonth` stays `$0.00` but you expect spend**:
  - confirm **`kubefisher:cost_per_hour`** has series for workloads in that namespace (same dependency chain as cost-patcher; see **`docs/cost-patcher.md`**).
- **`SpecValid=False`**:
  - fix **`monthlyCostLimitUSD`** to match the CRD pattern and be parseable as a non-negative decimal.
- **`go mod tidy` inside `operator/` pulled bad Kubernetes versions**:
  - re-pin direct **`require`** versions to match kubebuilder’s scaffold (**k8s.io v0.35**, **controller-runtime v0.23.x** as in **`operator/go.mod`**) before re-running tidy.
- **GPU pods still schedule when over budget**:
  - confirm the namespace has **`kubefisher.io/quota-enforcement=enabled`** and the **`ValidatingWebhookConfiguration`** is installed (`kubectl get validatingwebhookconfiguration`).
  - confirm **`status.phase=Exceeded`** on the `TeamInferenceQuota` and **`spec.enforcementMode=Enforce`** (default).
  - if **`phase=Unknown`**, admission intentionally allows (Prometheus unreachable).
- **Webhook not called**:
  - check manager logs and that the webhook **Service** endpoints are ready; **`failurePolicy: Ignore`** means API server may admit pods if the webhook is unreachable.

---

## Testing (unit + integration)

### Unit tests

- **`operator/internal/controller/phase_test.go`** — `computePhase`, alert threshold defaults (no cluster).

### Controller tests (envtest + fake Prometheus HTTP)

- **`operator/internal/controller/teaminferencequota_controller_test.go`** — creates a real **`TeamInferenceQuota`** object against envtest’s API server, runs **`Reconcile`**, asserts status patches.
- **`operator/internal/controller/prometheus_query_testsupport_test.go`** — minimal **`/api/v1/query`** JSON compatible with **`prometheus/client_golang`**.

### Webhook tests (fake client; no cluster)

- **`operator/internal/webhook/pod_webhook_test.go`** — table-driven tests for GPU detection, all admission decision paths, denial message fields, and audit events (`record.FakeRecorder`).

Run from repo root:

```bash
make operator-test
```

Run only fast unit tests (no envtest download):

```bash
cd operator && go test ./internal/controller/ -run TestComputePhase -count=1
```

---

## Related documentation

- **`docs/verify-quota.md`** — support runbook: 5-command enforcement verification.
- **`docs/security.md`** — `failurePolicy` decision, TLS, namespace exclusions, RBAC.
- **`docs/cost-patcher.md`** — pricing, recording rules, and annotations that feed the cost side of the platform.
- **`docs/contract.md`** — labels, annotations, metrics, and pricing schema (authoritative where overlapping).
- **`docs/cluster-dev.md`** — k3d / observability local cluster.
