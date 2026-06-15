#!/usr/bin/env bash
# KubeFisher demo script
# Runs end-to-end on a clean k3d cluster in under 10 minutes.
# No GPU hardware, no Docker image build, no manual steps required.
#
# Usage:
#   bash hack/demo.sh            # full demo (creates cluster)
#   bash hack/demo.sh --skip-cluster  # skip cluster-up (cluster already running)
#   bash hack/demo.sh --clean    # tear down demo namespace at the end
#
# Prerequisites: k3d, kubectl, helm, go, make
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DEMO_NS="demo"
DEMO_WORKLOAD="opt-125m"
SKIP_CLUSTER=false
CLEAN_UP=false

for arg in "$@"; do
  case "$arg" in
    --skip-cluster) SKIP_CLUSTER=true ;;
    --clean)        CLEAN_UP=true ;;
  esac
done

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

step()  { echo -e "\n${CYAN}${BOLD}[STEP $1/$TOTAL_STEPS] $2${RESET}"; }
ok()    { echo -e "  ${GREEN}✓${RESET} $1"; }
info()  { echo -e "  ${YELLOW}→${RESET} $1"; }
fatal() { echo -e "\n${RED}${BOLD}ERROR: $1${RESET}" >&2; exit 1; }

require_cmd() {
  if ! command -v "$1" &>/dev/null; then
    fatal "'$1' not found in PATH. Install it and re-run.\n  Hint: $2"
  fi
  ok "$1 $(${1} --version 2>&1 | head -1)"
}

TOTAL_STEPS=8
START_TS=$(date +%s)

# ---------------------------------------------------------------------------
# Step 1 — Prerequisites
# ---------------------------------------------------------------------------
step 1 "Checking prerequisites"
require_cmd k3d    "https://k3d.io/#installation"
require_cmd kubectl "https://kubernetes.io/docs/tasks/tools/"
require_cmd helm   "https://helm.sh/docs/intro/install/"
require_cmd go     "https://go.dev/dl/"
require_cmd make   "install build-essential / Xcode CLT"

# ---------------------------------------------------------------------------
# Step 2 — Create k3d cluster + Prometheus
# ---------------------------------------------------------------------------
if [[ "$SKIP_CLUSTER" == "true" ]]; then
  step 2 "Skipping cluster creation (--skip-cluster)"
  info "Using existing cluster context: $(kubectl config current-context)"
else
  step 2 "Creating k3d cluster + installing Prometheus (≈5 min)"
  info "Running: make cluster-up"
  cd "${REPO_ROOT}"
  make cluster-up
  ok "Cluster ready. Prometheus: http://localhost:9090  Grafana: http://localhost:3000"
fi

cd "${REPO_ROOT}"

# ---------------------------------------------------------------------------
# Step 3 — Build CLI
# ---------------------------------------------------------------------------
step 3 "Building kubefisher CLI"
make kubefisher-build
ok "Binary: $(./bin/kubefisher version 2>/dev/null | head -1 || echo bin/kubefisher)"

# ---------------------------------------------------------------------------
# Step 4 — Apply demo workload
# ---------------------------------------------------------------------------
step 4 "Applying demo workload (facebook/opt-125m, CPU-only)"
kubectl apply -f hack/demo-workload.yaml
info "Waiting for rollout…"
kubectl rollout status deployment/"${DEMO_WORKLOAD}" \
  -n "${DEMO_NS}" --timeout=60s
ok "Deployment ${DEMO_WORKLOAD} is ready"

# ---------------------------------------------------------------------------
# Step 5 — Show cost table
# ---------------------------------------------------------------------------
step 5 "Running: kubefisher cost -A"
echo ""
./bin/kubefisher cost -A
echo ""
ok "The opt-125m row shows cost/hr and cost/token from annotations."
info "(In production, cost-patcher writes these; see docs/faq.md for details.)"

# ---------------------------------------------------------------------------
# Step 6 — Set a quota
# ---------------------------------------------------------------------------
step 6 "Creating TeamInferenceQuota for the demo namespace"
# Requires the TeamInferenceQuota CRD to be installed.
# Gracefully skip if the CRD is absent (cost demo still succeeded above).
if kubectl get crd teaminferencequotas.quota.kubefisher.io &>/dev/null 2>&1; then
  ./bin/kubefisher quota set \
    --name demo \
    -n "${DEMO_NS}" \
    --daily-tokens 100000 \
    --monthly-cost 50.00 \
    --mode Audit
  ok "TeamInferenceQuota 'demo/demo' created in Audit mode"
else
  info "TeamInferenceQuota CRD not installed — skipping quota set."
  info "Install the operator (make cluster-install-kubefisher) and re-run to try quotas."
fi

# ---------------------------------------------------------------------------
# Step 7 — Label namespace for enforcement
# ---------------------------------------------------------------------------
step 7 "Labelling namespace for quota enforcement"
if kubectl get crd teaminferencequotas.quota.kubefisher.io &>/dev/null 2>&1; then
  kubectl label namespace "${DEMO_NS}" \
    kubefisher.io/quota-enforcement=enabled \
    --overwrite
  ok "Namespace '${DEMO_NS}' labelled for enforcement"
else
  info "Skipped (operator not installed)"
fi

# ---------------------------------------------------------------------------
# Step 8 — Show quota list
# ---------------------------------------------------------------------------
step 8 "Running: kubefisher quota list -A"
if kubectl get crd teaminferencequotas.quota.kubefisher.io &>/dev/null 2>&1; then
  echo ""
  ./bin/kubefisher quota list -A
  echo ""
  ok "Quota list shows phase, budget bars, and mode."
else
  echo ""
  ./bin/kubefisher quota list -A 2>/dev/null || \
    info "No quotas found (CRD not installed). See docs/teaminferencequota-operator.md."
  echo ""
fi

# ---------------------------------------------------------------------------
# Clean up (optional)
# ---------------------------------------------------------------------------
if [[ "$CLEAN_UP" == "true" ]]; then
  echo -e "\n${YELLOW}Cleaning up demo namespace…${RESET}"
  kubectl delete namespace "${DEMO_NS}" --ignore-not-found
  ok "Namespace '${DEMO_NS}' removed"
fi

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
END_TS=$(date +%s)
ELAPSED=$(( END_TS - START_TS ))

echo ""
echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
echo -e "${GREEN}${BOLD}  Demo complete in ${ELAPSED}s.${RESET}"
echo -e "${GREEN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}"
echo ""
echo "  Next steps:"
echo "    ./bin/kubefisher cost -A --watch      # live cost refresh"
echo "    ./bin/kubefisher deploy --help        # deploy a real model"
echo "    docs/getting-started.md             # full step-by-step guide"
echo "    docs/faq.md                         # common questions"
echo ""
