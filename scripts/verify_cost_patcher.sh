#!/usr/bin/env bash
# Verify cost-patcher annotations (kubectl + python3).
set -euo pipefail

NS="${1:?namespace}"
DEPLOY="${2:?deployment name}"
EXPECT_HOUR="${EXPECT_HOUR:-0}"
EXPECT_TOKEN="${EXPECT_TOKEN:-0}"
TIMEOUT_SEC="${TIMEOUT_SEC:-120}"

ann() {
  local key="$1"
  kubectl get "deploy/${DEPLOY}" -n "${NS}" -o json \
    | python3 -c "import json,sys; k=sys.argv[1]; d=json.load(sys.stdin); print(d.get('metadata',{}).get('annotations',{}).get(k,''))" "${key}"
}

wait_for() {
  local label="$1" key="$2"
  local end=$((SECONDS + TIMEOUT_SEC))
  while (( SECONDS < end )); do
    if out="$(ann "${key}")"; [[ -n "${out}" ]]; then
      echo "OK ${label}: ${out}"
      return 0
    fi
    sleep 5
  done
  echo "ERROR: timeout waiting for ${label} on ${NS}/${DEPLOY}" >&2
  kubectl get "deploy/${DEPLOY}" -n "${NS}" -o yaml | grep -E 'cost-per|last-updated' || true
  exit 1
}

if [[ "${EXPECT_HOUR}" == "1" ]]; then
  wait_for "kubefisher.io/cost-per-hour" "kubefisher.io/cost-per-hour"
fi

if [[ "${EXPECT_TOKEN}" == "1" ]]; then
  wait_for "kubefisher.io/cost-per-token" "kubefisher.io/cost-per-token"
fi
