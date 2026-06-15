# Verify GPU quota enforcement

**Support runbook:** run this procedure first for any “GPU pod won’t schedule”, “quota not blocking”, or “webhook not working” issue. Policy background (failurePolicy, TLS, namespace exclusions) lives in [security.md](security.md).

**Time:** about 5 minutes on a cluster that already has KubeFisher installed with `operator.enabled=true` and `operator.webhook.enabled=true`.

**Prerequisites:** `kubectl`, `helm` (for install step only), [cert-manager](https://cert-manager.io/) if using chart TLS defaults.

Set these once (adjust to your install):

```bash
export KUBEFISHER_NS=kubefisher-system   # Helm release namespace
export TEST_NS=gpu-test
export HELM_RELEASE=kubefisher
```

---

## Command 1 — Platform health (operator + webhook + exclusions)

Confirms the quota operator is running and the validating webhook is registered with **`failurePolicy: Ignore`** and system-namespace **`NotIn`** exclusions.

```bash
kubectl rollout status deployment -n "$KUBEFISHER_NS" \
  -l app.kubernetes.io/component=quota-operator --timeout=120s && \
VWC=$(kubectl get validatingwebhookconfiguration -o yaml) && \
echo "$VWC" | grep -q 'name: vpod.kb.io' && \
echo "$VWC" | grep -q 'failurePolicy: Ignore' && \
echo "$VWC" | grep -q 'kubefisher.io/quota-enforcement' && \
echo "$VWC" | grep -q 'kube-system' && \
echo "$VWC" | grep -q 'kubefisher-system' && \
echo "OK: operator and webhook configuration look correct"
```

**Pass:** Deployment is ready; last line prints `OK: operator and webhook configuration look correct`.

**Fail:** Operator not ready → `kubectl logs -n "$KUBEFISHER_NS" deploy -l app.kubernetes.io/component=quota-operator`. No webhook → reinstall with `operator.webhook.enabled=true`. Missing `kube-system` / `kubefisher-system` in output → upgrade Helm chart or kustomize [namespace_selector_patch.yaml](../operator/config/webhook/namespace_selector_patch.yaml).

---

## Command 2 — Opt-in namespace + exceeded quota

Creates a test namespace (labeled for enforcement) and a `TeamInferenceQuota` that becomes **Exceeded** immediately (`dailyTokenBudget: 0`).

```bash
kubectl create namespace "$TEST_NS" --dry-run=client -o yaml | kubectl apply -f - && \
kubectl label namespace "$TEST_NS" kubefisher.io/quota-enforcement=enabled --overwrite && \
kubectl apply -f - <<EOF
apiVersion: quota.kubefisher.io/v1alpha1
kind: TeamInferenceQuota
metadata:
  name: test-quota
  namespace: $TEST_NS
spec:
  dailyTokenBudget: 0
  monthlyCostLimitUSD: "0.01"
  enforcementMode: Enforce
EOF
```

**Pass:** `teaminferencequota/test-quota created` (or configured).

**Fail:** CRD missing → `helm upgrade` / `kubectl apply` CRD from `charts/kubefisher/crds/`.

---

## Command 3 — Wait for phase Exceeded

Reconcile runs about every 60s.

```bash
kubectl wait teaminferencequota/test-quota -n "$TEST_NS" \
  --for=jsonpath='{.status.phase}'=Exceeded --timeout=90s && \
kubectl get tiq test-quota -n "$TEST_NS" -o wide
```

**Pass:** `condition met` and `PHASE` column shows `Exceeded`.

**Fail:** Stays `Unknown` → Prometheus unreachable from operator ([teaminferencequota-operator.md](teaminferencequota-operator.md) troubleshooting). Stays `Active` → wait longer or check `kubectl describe tiq test-quota -n "$TEST_NS"`.

---

## Command 4 — GPU pod must be denied

```bash
kubectl run gpu-quota-verify -n "$TEST_NS" --image=busybox --restart=Never \
  --overrides='{"spec":{"containers":[{"name":"c","image":"busybox","resources":{"limits":{"nvidia.com/gpu":"1"}}}]}}' \
  2>&1 | tee /tmp/gpu-quota-verify.txt; \
grep -iE 'denied|quota exceeded|GPU pod admission' /tmp/gpu-quota-verify.txt
```

**Pass:** Error output contains `GPU pod admission denied` (or similar) with token/cost details and `kubectl edit teaminferencequota`.

**Fail:** Pod created → namespace missing label, phase not `Exceeded`, `enforcementMode: Audit`, webhook not registered, or `failurePolicy: Ignore` with webhook unreachable (admission allowed). Re-run **Command 1**.

---

## Command 5 — CPU pod must be allowed

Confirms the webhook only targets GPU workloads.

```bash
kubectl run cpu-quota-verify -n "$TEST_NS" --image=busybox --restart=Never --command -- sleep 120 && \
kubectl wait pod/cpu-quota-verify -n "$TEST_NS" --for=condition=Ready --timeout=60s
```

**Pass:** Pod reaches `Ready`.

**Fail:** CPU pod blocked → webhook misconfigured (should only deny `nvidia.com/gpu`); capture `kubectl get validatingwebhookconfiguration -o yaml`.

---

## Cleanup (optional)

```bash
kubectl delete namespace "$TEST_NS" --ignore-not-found
```

---

## Fresh install (before Command 1)

If KubeFisher is not installed yet, from the **repository root**:

**k3d / kind (local image):** the chart defaults to `kubefisher/operator:dev`, which is not on Docker Hub. Build and import before Helm:

```bash
make operator-image
make cluster-k3d-import-operator    # or: k3d image import kubefisher/operator:dev -c <cluster>
```

**Helm:**

```bash
make cluster-install-kubefisher    # imports both images + installs chart (k3d)
# or manually:
helm upgrade --install "$HELM_RELEASE" ./charts/kubefisher \
  --namespace "$KUBEFISHER_NS" --create-namespace \
  --set operator.enabled=true \
  --set operator.webhook.enabled=true \
  --set operator.image.pullPolicy=IfNotPresent \
  --wait --timeout=180s
```

Then run **Commands 1–5**.

---

## Quick reference

| Symptom | Likely cause | Check |
|---------|----------------|-------|
| GPU pod schedules when over budget | Namespace not labeled, phase not Exceeded, Audit mode | Commands 2–4 |
| Nothing blocked, phase Unknown | Prometheus down | `kubectl describe tiq`, operator logs |
| Operator won’t restart | Should not happen for CPU pods; if platform NS labeled wrongly | [security.md](security.md) exclusions |
| All pods slow to create | Webhook latency; only labeled namespaces call webhook | Command 1 `namespaceSelector` |

---

## Related docs

- [security.md](security.md) — failurePolicy, TLS, RBAC, namespace exclusions
- [teaminferencequota-operator.md](teaminferencequota-operator.md) — reconcile loop, PromQL, phases
- [cluster-dev.md](cluster-dev.md) — local k3d install
