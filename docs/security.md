# KubeFisher security and admission

This document covers **TeamInferenceQuota** admission webhook behavior, TLS, namespace scope, and RBAC.

**Verification runbook (start here for support):** [verify-quota.md](verify-quota.md) — five commands, under 5 minutes, to confirm enforcement on a cluster.

For operator reconcile behavior and PromQL, see [teaminferencequota-operator.md](teaminferencequota-operator.md).

---

## failurePolicy decision (v0)

The GPU pod **ValidatingWebhookConfiguration** uses:

```yaml
failurePolicy: Ignore
```

### Rationale

| Policy | When webhook is down | v0 fit |
|--------|----------------------|--------|
| **Ignore** | Pod CREATE is **allowed** | **Chosen** — a restarting or unreachable operator must not block unrelated workloads cluster-wide. |
| **Fail** | Pod CREATE is **denied** | Deferred — strong enforcement against bypass, but causes **cluster-wide outage** if the webhook Service, TLS, or manager is unavailable. |

v0 runs a **single-replica** operator without a documented HA + leader-election deployment for the webhook path. **`Ignore`** is the safe default: enforcement is best-effort when the control plane can reach the webhook; it is suspended when it cannot.

### v1 revisit criteria

Consider **`failurePolicy: Fail`** when all of the following hold:

- Operator Deployment runs **≥2 replicas** with **leader election** and a stable webhook Service backend.
- Runbooks cover webhook TLS renewal and cert-manager health.
- On-call accepts that webhook outages block **labeled** namespaces only (with namespace selector), not the whole cluster — still coordinate with SRE before switching.

Document any change in this file and in Helm values comments.

---

## Namespace opt-in and exclusions

The API server applies `ValidatingWebhookConfiguration.namespaceSelector` **before** calling the webhook. Only matching namespaces are evaluated; the Go webhook does not implement its own namespace filter.

Enforcement applies only where **all** of the following are true:

1. Namespace has label **`kubefisher.io/quota-enforcement=enabled`** (configurable via Helm `operator.webhook.namespaceSelector`).
2. Namespace name is **not** in the exclusion list (see below).

Namespaces without the label are never sent to the webhook (no quota object required).

### Always-excluded namespaces

These namespaces are **never** subject to GPU quota admission, even if someone adds the opt-in label:

| Namespace | Why |
|-----------|-----|
| **`kube-system`** | Platform workloads (DNS, CNI, etc.) must not depend on quota webhook availability. |
| **`kubefisher-system`** | Default Helm install namespace for cost-patcher and the quota operator. |
| **`operator-system`** | Default kubebuilder/kustomize dev namespace (`config/default`). |
| **Helm release namespace** | Always excluded via `{{ .Release.Namespace }}` in the chart template (covers the operator Deployment namespace even when it is not named `kubefisher-system`). |

Helm also merges `operator.webhook.namespaceSelector.excludedNamespaces` (defaults include `kube-system` and `kubefisher-system`). Kustomize dev installs use the same `matchExpressions` in [`operator/config/webhook/namespace_selector_patch.yaml`](../operator/config/webhook/namespace_selector_patch.yaml).

**Operator restarts:** The webhook only denies pods that request **`nvidia.com/gpu`**. Operator manager pods are CPU-only and would not be denied on that basis alone. Namespace exclusions still matter so a mislabeled platform namespace does not route system pod creates through quota admission.

A **`TeamInferenceQuota`** in a labeled, non-excluded namespace with **`spec.enforcementMode: Enforce`** and **`status.phase: Exceeded`** causes **new** GPU pod CREATE requests to be denied. **Updates and deletes** are always allowed (existing pods keep running).

When **`status.phase` is `Unknown`** (Prometheus unreachable), admission **always allows** — the controller cannot measure spend; the webhook must not block blind.

---

## TLS (cert-manager)

When `operator.webhook.certManager.enabled` is true (default), the Helm chart installs:

- **Issuer** (`selfSigned`) in the release namespace
- **Certificate** with DNS names for the webhook Service
- **Secret** mounted at `/tmp/k8s-webhook-server/serving-certs` on the manager (`tls.crt` / `tls.key`)
- **`cert-manager.io/inject-ca-from`** on the `ValidatingWebhookConfiguration` so the API server trusts the serving cert

**Requirements:** [cert-manager](https://cert-manager.io/) installed in the cluster (e.g. `cert-manager` namespace).

**Bring your own TLS:** set `operator.webhook.certManager.enabled=false`, create Secret `operator.webhook.certManager.secretName` (or default `<release>-kubefisher-operator-webhook-tls`) with `tls.crt` and `tls.key`, and ensure the ValidatingWebhookConfiguration `caBundle` is set (manual or your own CA injector).

The manager reads certs via `--webhook-cert-path=/tmp/k8s-webhook-server/serving-certs` (set by the chart when webhook is enabled).

---

## Operator RBAC (summary)

The operator **ServiceAccount** (cluster-scoped binding) can:

| Resource | Verbs | Purpose |
|----------|-------|---------|
| `teaminferencequotas` (+ status, finalizers) | get/list/watch/patch/update/... | Reconcile quota status |
| `pods` | get/list/watch | Cache for admission (webhook uses API reader) |
| `events` | create/patch | Phase and audit events |
| `secrets` | get | Optional future use (scaffold parity with kubebuilder RBAC) |

It **cannot** delete arbitrary workloads, modify unrelated CRDs, or escalate cluster-admin. It does **not** implement a mutating webhook.

---

## Denial message (self-service)

When admission denies a GPU pod (`Enforce` + `Exceeded`), the API server returns a message like:

```text
GPU pod admission denied: namespace "llm" exceeded TeamInferenceQuota "team-quota" (phase Exceeded). Tokens (rolling 24h): 2,000,000 of 1,000,000 daily budget. Cost (month-to-date, UTC): $600.00 of $500.00 monthly limit. Next reset: 2026-06-01 00:00 (2026-06-01T00:00:00Z UTC). To adjust limits, run: kubectl edit teaminferencequota team-quota -n llm
```

Operators should use this message in runbooks instead of opening a platform ticket for “GPU quota exceeded” without context.

---

## Verification

Use **[verify-quota.md](verify-quota.md)** for the canonical 5-command procedure (platform health → label namespace → wait for Exceeded → deny GPU pod → allow CPU pod). Every quota support issue should start with that runbook.

---

## Related documentation

- [verify-quota.md](verify-quota.md) — support runbook and 5-minute enforcement test
- [teaminferencequota-operator.md](teaminferencequota-operator.md) — reconcile loop, phases, Audit mode events
- [contract.md](contract.md) — labels and metrics contract
- [cluster-dev.md](cluster-dev.md) — local k3d stack
