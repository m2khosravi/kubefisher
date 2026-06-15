#!/usr/bin/env bash
# Verify kubefisher:cost_per_hour recording rule label semantics:
#
#   (a) The labeled workload (gpu-labeled-workload) produces exactly ONE series
#       with kubefisher_io_team="team-test" — not "unlabeled", not duplicated.
#
#   (b) The unlabeled workload (gpu-fake-workload) produces a series that carries
#       kubefisher_io_team="unlabeled" instead of lacking the label entirely.
#
# Requires a Prometheus HTTP endpoint reachable at PROM_URL (default
# http://localhost:9090 — ensure a port-forward is active before calling).
#
# Usage:
#   bash scripts/verify_recording_rules.sh
#   PROM_URL=http://localhost:9090 TIMEOUT_SEC=120 bash scripts/verify_recording_rules.sh
set -euo pipefail

PROM_URL="${PROM_URL:-http://localhost:9090}"
TIMEOUT_SEC="${TIMEOUT_SEC:-120}"
NS="llm-inference"

prom_query() {
  local query="$1"
  curl -fsS --get "${PROM_URL}/api/v1/query" --data-urlencode "query=${query}"
}

wait_for_series() {
  local desc="$1" query="$2" validator="$3"
  local end=$((SECONDS + TIMEOUT_SEC))
  while (( SECONDS < end )); do
    if out="$(prom_query "${query}" 2>/dev/null)"; then
      if python3 -c "${validator}" <<< "${out}" 2>/dev/null; then
        echo "OK  ${desc}"
        return 0
      fi
    fi
    sleep 5
  done
  echo "FAIL ${desc}" >&2
  # Print last response for diagnostics
  prom_query "${query}" 2>/dev/null | python3 -c "import json,sys; d=json.load(sys.stdin); [print(' ', r) for r in d.get('data',{}).get('result',[])]" || true
  exit 1
}

echo "== Waiting for Prometheus at ${PROM_URL} =="
for i in {1..60}; do
  if curl -fsS "${PROM_URL}/-/ready" >/dev/null 2>&1; then
    echo "   Prometheus: ready"
    break
  fi
  sleep 2
done

# ── (a) Labeled workload assertions ──────────────────────────────────────────
# (a1) Exactly one series for gpu-labeled-workload with kubefisher_io_team="team-test"
wait_for_series \
  "(a1) labeled pod: exactly one series with kubefisher_io_team=team-test" \
  "kubefisher:cost_per_hour{namespace=\"${NS}\",kubefisher_io_team=\"team-test\"}" \
  'import json,sys
d=json.load(sys.stdin)
assert d["status"] == "success", d
r = d["data"]["result"]
# Exactly one series
assert len(r) == 1, f"expected 1 series, got {len(r)}: {r}"
lset = r[0]["metric"]
assert lset.get("kubefisher_io_team") == "team-test", lset
assert lset.get("kubefisher_io_platform") == "test-platform", lset
assert lset.get("kubefisher_io_model") == "test-model", lset
'

# (a2) No "unlabeled" series for the labeled pod's namespace (it should only appear
#      for gpu-fake-workload, not gpu-labeled-workload)
wait_for_series \
  "(a2) labeled pod: no series with kubefisher_io_team=unlabeled for gpu-labeled-workload" \
  "kubefisher:cost_per_hour{namespace=\"${NS}\",kubefisher_io_team=\"unlabeled\"}" \
  'import json,sys
d=json.load(sys.stdin)
assert d["status"] == "success", d
r = d["data"]["result"]
# Must not contain a series whose pod label matches gpu-labeled-workload
for series in r:
    pod = series["metric"].get("pod", "")
    assert not pod.startswith("gpu-labeled-workload"), \
        f"gpu-labeled-workload pod has kubefisher_io_team=unlabeled: {series}"
'

# ── (b) Unlabeled workload assertions ─────────────────────────────────────────
# (b1) gpu-fake-workload pod carries kubefisher_io_team="unlabeled"
wait_for_series \
  "(b1) unlabeled pod: carries kubefisher_io_team=unlabeled" \
  "kubefisher:cost_per_hour{namespace=\"${NS}\",kubefisher_io_team=\"unlabeled\"}" \
  'import json,sys
d=json.load(sys.stdin)
assert d["status"] == "success", d
r = d["data"]["result"]
assert len(r) >= 1, f"expected at least 1 unlabeled series, got {len(r)}"
found = any(s["metric"].get("pod","").startswith("gpu-fake-workload") for s in r)
assert found, f"no gpu-fake-workload pod among unlabeled series: {r}"
'

# (b2) gpu-fake-workload pod does NOT appear in any series lacking kubefisher_io_team
#      (i.e. the label is always present)
wait_for_series \
  "(b2) unlabeled pod: kubefisher_io_team label is always present (no series missing it)" \
  "kubefisher:cost_per_hour{namespace=\"${NS}\"}" \
  'import json,sys
d=json.load(sys.stdin)
assert d["status"] == "success", d
for s in d["data"]["result"]:
    m = s["metric"]
    if not m.get("pod","").startswith("gpu-fake-workload"):
        continue
    assert "kubefisher_io_team" in m, f"gpu-fake-workload series missing kubefisher_io_team: {m}"
    assert "kubefisher_io_platform" in m, f"gpu-fake-workload series missing kubefisher_io_platform: {m}"
    assert "kubefisher_io_model" in m, f"gpu-fake-workload series missing kubefisher_io_model: {m}"
'

echo ""
echo "== All recording rule label assertions passed =="

# ── (c) Total cost conservation ───────────────────────────────────────────────
# (c1) sum(kubefisher:cost_per_hour) across the namespace must equal the raw
#      expected total derived from first-principle metrics (GPU limits × node
#      price). Catches overlap (pod counted in both arms → sum too high) and gap
#      (pod dropped from both arms → sum too low), regardless of which labels
#      are involved.
#
#      Tolerance: 1 % relative.  The recording rule is evaluated on a 30 s
#      interval; the two Prometheus queries run milliseconds apart on stable test
#      pods, so any difference reflects a rule logic error, not eval timing.
echo "== Checking cost conservation (recorded vs raw GPU×price) =="
python3 - <<PYEOF
import json, sys, urllib.request, urllib.parse

prom = "${PROM_URL}"
ns   = "${NS}"

def query(q):
    url = f"{prom}/api/v1/query?" + urllib.parse.urlencode({"query": q})
    with urllib.request.urlopen(url, timeout=10) as r:
        d = json.loads(r.read())
    assert d["status"] == "success", d
    return d["data"]["result"]

recorded = query(f'kubefisher:cost_per_hour{{namespace="{ns}"}}')
assert len(recorded) > 0, "no kubefisher:cost_per_hour series in namespace"
recorded_total = sum(float(s["value"][1]) for s in recorded)

raw_q = (
    # The price join must happen BEFORE sum by (namespace, pod) aggregates away
    # the node label. Mirroring the recording rule structure exactly:
    #   sum by (namespace, pod) (
    #     sum by (namespace, pod, node) ( gpu_limits * pod_info )
    #     * on(node) group_left(currency) price_by_node
    #   )
    f'sum(sum by (namespace, pod) ('
    f'sum by (namespace, pod, node) ('
    f'kube_pod_container_resource_limits{{resource="nvidia_com_gpu",namespace="{ns}"}}'
    f' * on(namespace, pod) group_left(node)'
    f' max by (namespace, pod, node) (kube_pod_info{{node!="",namespace!="",pod!=""}})'
    f') * on(node) group_left(currency) kubefisher_gpu_price_per_hour_by_node))'
)
raw = query(raw_q)
assert len(raw) == 1, f"expected 1 scalar from raw expected query, got: {raw}"
raw_total = float(raw[0]["value"][1])

print(f"  recorded sum : {recorded_total:.6f}")
print(f"  raw expected : {raw_total:.6f}")
print(f"  series count : {len(recorded)}")

if raw_total == 0:
    # Pricing not yet scraped — skip the tolerance check but warn
    print("  WARN: raw_total is 0 (kubefisher_gpu_price_per_hour_by_node not yet available); skipping tolerance check")
    sys.exit(0)

rel_diff = abs(recorded_total - raw_total) / raw_total
print(f"  rel diff     : {rel_diff*100:.4f} %")
assert rel_diff <= 0.01, (
    f"FAIL: recorded total {recorded_total:.6f} differs from raw expected {raw_total:.6f} "
    f"by {rel_diff*100:.4f} % (> 1 %); likely overlap or gap between primary and fallback arms"
)
print("OK  (c1) total cost conservation: within 1 %")
PYEOF
