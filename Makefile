SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

# Binaries
K3D ?= k3d
KUBECTL ?= kubectl
HELM ?= helm
CURL ?= curl
PYTHON ?= python3

# Config
CLUSTER_NAME ?= kubefisher-dev
K3D_CONFIG ?= config/cluster/observability/k3d-config.yaml
PROM_VALUES ?= config/cluster/observability/helm/kube-prometheus-stack-values.yaml
DCGM_MOCK_MANIFEST ?= config/cluster/observability/manifests/dcgm-mock.yaml
GRAFANA_AUTH_MANIFEST ?= config/cluster/observability/manifests/grafana-auth.yaml
GPU_OPERATOR_VALUES ?= config/cluster/observability/helm/gpu-operator-values.yaml
DCGM_EXPORTER_SERVICEMONITOR ?= config/cluster/observability/manifests/dcgm-exporter-servicemonitor.yaml
VLLM_MOCK_MANIFEST ?= config/cluster/serving/vllm/vllm-mock-metrics.yaml
VLLM_MOCK_SERVICEMONITOR ?= config/cluster/serving/vllm/vllm-mock-servicemonitor.yaml
VLLM_CPU_DEMO_MANIFEST ?= config/cluster/serving/vllm/vllm-cpu-demo.yaml
VLLM_COSTTOKEN_MOCK_MANIFEST ?= config/cluster/serving/vllm/vllm-costtoken-mock.yaml
VLLM_COSTTOKEN_MOCK_SERVICEMONITOR ?= config/cluster/serving/vllm/vllm-costtoken-mock-servicemonitor.yaml
TEST_GPU_FAKE_MANIFEST ?= deployments/kubernetes/test/gpu-fake-workload.yaml
TEST_GPU_LABELED_MANIFEST ?= deployments/kubernetes/test/gpu-labeled-workload.yaml

# KubeFisher Helm chart (canonical install path)
CHART_DIR ?= charts/kubefisher
CHART_RELEASE ?= kubefisher
CHART_NAMESPACE ?= kubefisher-system
# Local-dev image overrides (build + `k3d image import` before in-cluster Deployments start)
LOCAL_IMAGE_REPO ?= kubefisher/cost-patcher
LOCAL_OPERATOR_IMAGE_REPO ?= kubefisher/operator
LOCAL_IMAGE_TAG ?= dev

# Published registry image names (used by `make release-images`).
# Override RELEASE_TAG to push a specific version; defaults to the current git tag.
GHCR_IMAGE_REPO ?= ghcr.io/m2khosravi/kubefisher/cost-patcher
GHCR_OPERATOR_IMAGE_REPO ?= ghcr.io/m2khosravi/kubefisher/operator
RELEASE_TAG ?= $(shell git describe --tags --exact-match 2>/dev/null || echo "dev")

MON_NS ?= monitoring
PROM_RELEASE ?= prometheus
PROM_CHART ?= prometheus-community/kube-prometheus-stack
GPU_OPERATOR_NS ?= gpu-operator
GPU_OPERATOR_RELEASE ?= gpu-operator

## Location to install dev tool binaries (envtest, etc.)
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
ENVTEST ?= $(LOCALBIN)/setup-envtest

## Tool Versions (derived from go.mod when possible)
ENVTEST_VERSION ?= $(shell v='$(call gomodver,sigs.k8s.io/controller-runtime)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_VERSION manually" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?([0-9]+)\.([0-9]+).*/release-\1.\2/')
ENVTEST_K8S_VERSION ?= $(shell v='$(call gomodver,k8s.io/api)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_K8S_VERSION manually" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?[0-9]+\.([0-9]+).*/1.\1/')

.PHONY: help
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ { printf "  %-28s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo ""
	@echo "Recommended flows:"
	@echo ""
	@echo "  1) Bring up local observability (k3d + Prometheus + Grafana + DCGM mock)"
	@echo "     make cluster-up"
	@echo ""
	@echo "  2) Verify vLLM scrape (token counters exist; does NOT test cost/token math)"
	@echo "     make cluster-install-vllm-mock"
	@echo "     make cluster-verify-vllm-mock"
	@echo ""
	@echo "  3) Run cost patcher in-cluster + verify cost/hour annotation (fast)"
	@echo "     make cluster-e2e-cost-patcher-hour"
	@echo ""
	@echo "     make cluster-e2e-cost-patcher-token  (slow: vLLM rollout + 5m rate window)"
	@echo "     make cluster-e2e-cost-patcher-token"
	@echo ""
	@echo "  5) TeamInferenceQuota operator + kubefisher CLI (local dev; needs cluster-up Prometheus)"
	@echo "     cd operator && make install && cd .. && make operator-run"
	@echo "     kubectl apply -f operator/config/samples/quota_v1alpha1_teaminferencequota.yaml"
	@echo "     make kubefisher-build && ./bin/kubefisher quota list -A"
	@echo "     (see docs/cluster-dev.md and docs/teaminferencequota-operator.md)"
	@echo ""
	@echo "  Notes:"
	@echo "    - In k3d, import locally-built images before Helm workloads start:"
	@echo "        make cluster-k3d-import-kubefisher    # cost-patcher + operator"
	@echo "        make cluster-k3d-import-cost-patcher   # cost-patcher only"
	@echo "        make cluster-k3d-import-operator       # operator only"
	@echo "    - The nginx vLLM mock exposes static counters; cost/token requires increasing counters (we use vllm-costtoken-mock)."
	@echo ""
	@echo "Cleanup (cluster still running):"
	@echo "  make cluster-clean-all-apps          # Helm uninstall kubefisher + all dev/test workloads below"
	@echo "  make cluster-clean-kubefisher-cost  # remove cost-patcher chart only (keeps Prometheus/Grafana)"
	@echo "  make cluster-clean-test-workloads    # fake GPU + vLLM mocks + ServiceMonitors (llm-inference + monitoring)"
	@echo "  make cluster-clean-operator-crds     # remove TeamInferenceQuota CRD (delete all TIQ CRs first)"
	@echo "  make cluster-clean-operator-deploy   # remove operator manager from operator/ (undo: make -C operator deploy)"
	@echo "  make docker-clean-build-cache        # prune docker build cache (optional)"

##@ Cluster (k3d + Prometheus + Grafana)

.PHONY: cluster-up
cluster-up: ## Create k3d cluster + install observability stack
	$(K3D) cluster create --config $(K3D_CONFIG)
	$(MAKE) cluster-gpu
	$(MAKE) cluster-install
	$(MAKE) cluster-verify

.PHONY: cluster-down
cluster-down: ## Delete the k3d cluster
	$(K3D) cluster delete $(CLUSTER_NAME)

.PHONY: cluster-reset
cluster-reset: cluster-down cluster-up ## Recreate the cluster from scratch

.PHONY: cluster-install
cluster-install: cluster-helm-repos cluster-ensure-monitoring-ns cluster-apply-secrets cluster-install-prometheus cluster-install-dcgm-mock ## Install Prometheus, Grafana, and mock DCGM exporter (default dev path)

.PHONY: cluster-ensure-monitoring-ns
cluster-ensure-monitoring-ns: ## Ensure monitoring namespace exists (for secrets before Helm)
	@$(KUBECTL) get ns $(MON_NS) >/dev/null 2>&1 || $(KUBECTL) create ns $(MON_NS)

.PHONY: cluster-apply-secrets
cluster-apply-secrets: ## Apply Grafana Secret/ConfigMap (auth + env)
	$(KUBECTL) apply -f $(GRAFANA_AUTH_MANIFEST)

.PHONY: cluster-install-vllm-mock
cluster-install-vllm-mock: ## Install vLLM mock metrics exporter + ServiceMonitor
	$(KUBECTL) apply -f $(VLLM_MOCK_MANIFEST)
	$(KUBECTL) apply -f $(VLLM_MOCK_SERVICEMONITOR)
	$(KUBECTL) -n llm-inference rollout status deploy/vllm-mock --timeout=120s

.PHONY: cluster-install-vllm-cpu-demo
cluster-install-vllm-cpu-demo: ## Optional: install real vLLM OpenAI server on CPU (facebook/opt-125m)
	$(KUBECTL) apply -f $(VLLM_CPU_DEMO_MANIFEST)
	$(KUBECTL) -n llm-inference rollout status deploy/vllm-cpu-demo --timeout=600s

.PHONY: cluster-verify-vllm-mock
cluster-verify-vllm-mock: ## Verify vLLM token counter series exist in Prometheus (mock)
	@echo "== Waiting for Prometheus to respond =="; \
	for i in {1..60}; do \
		if $(CURL) -fsS "http://localhost:9090/-/ready" >/dev/null 2>&1; then \
			echo "Prometheus: OK (http://localhost:9090)"; \
			break; \
		fi; \
		sleep 2; \
	done
	@echo "== Querying vLLM token counters in Prometheus =="; \
	for i in {1..30}; do \
		ok1=0; ok2=0; \
		if $(CURL) -fsS "http://localhost:9090/api/v1/query?query=vllm:prompt_tokens_total" | \
			$(PYTHON) -c 'import json,sys; d=json.load(sys.stdin); r=d.get("data",{}).get("result",[]); assert d.get("status")=="success", d; assert len(r)>0' >/dev/null 2>&1; then ok1=1; fi; \
		if $(CURL) -fsS "http://localhost:9090/api/v1/query?query=vllm:generation_tokens_total" | \
			$(PYTHON) -c 'import json,sys; d=json.load(sys.stdin); r=d.get("data",{}).get("result",[]); assert d.get("status")=="success", d; assert len(r)>0' >/dev/null 2>&1; then ok2=1; fi; \
		if [[ "$$ok1" -eq 1 && "$$ok2" -eq 1 ]]; then \
			echo "vLLM token counters: OK (vllm:prompt_tokens_total, vllm:generation_tokens_total)"; \
			exit 0; \
		fi; \
		sleep 2; \
	done; \
	echo "ERROR: vLLM token counters not found yet (scrape may not have happened)" >&2; \
	exit 1

.PHONY: cluster-helm-repos
cluster-helm-repos: ## Add/update Helm repos used by this stack
	@$(HELM) repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
	@$(HELM) repo add nvidia https://nvidia.github.io/gpu-operator >/dev/null 2>&1 || true
	@$(HELM) repo update

.PHONY: cluster-gpu
cluster-gpu: cluster-label-gpu cluster-patch-gpu ## Apply fake GPU labels + capacity

.PHONY: cluster-label-gpu
cluster-label-gpu: ## Add fake GPU labels to agent nodes
	$(KUBECTL) label node k3d-$(CLUSTER_NAME)-agent-0 \
		accelerator=nvidia-a10g \
		cloud.google.com/gke-accelerator=nvidia-tesla-a10 \
		feature.node.kubernetes.io/pci-10de.present=true \
		feature.node.kubernetes.io/system-os_release.ID=ubuntu \
		feature.node.kubernetes.io/system-os_release.VERSION_ID=22.04 --overwrite
	$(KUBECTL) label node k3d-$(CLUSTER_NAME)-agent-1 \
		accelerator=nvidia-a10g \
		cloud.google.com/gke-accelerator=nvidia-tesla-a10 \
		kubefisher.io/spot=true \
		feature.node.kubernetes.io/pci-10de.present=true \
		feature.node.kubernetes.io/system-os_release.ID=ubuntu \
		feature.node.kubernetes.io/system-os_release.VERSION_ID=22.04 --overwrite

.PHONY: cluster-patch-gpu
cluster-patch-gpu: ## Patch node capacity to advertise fake GPUs
	@$(KUBECTL) proxy --port=8001 >/tmp/kubectl-proxy-$(CLUSTER_NAME).log 2>&1 & \
	pid="$$!"; \
	trap 'kill "$$pid" >/dev/null 2>&1 || true' EXIT; \
	sleep 2; \
	for n in 0 1; do \
		curl -fsS --header "Content-Type: application/json-patch+json" \
			--request PATCH \
			--data '[{"op":"add","path":"/status/capacity/nvidia.com~1gpu","value":"2"},{"op":"add","path":"/status/allocatable/nvidia.com~1gpu","value":"2"}]' \
			http://localhost:8001/api/v1/nodes/k3d-$(CLUSTER_NAME)-agent-$$n/status >/dev/null; \
	done

.PHONY: cluster-install-gpu-operator
cluster-install-gpu-operator: ## Install NVIDIA GPU Operator (includes DCGM exporter)
	$(HELM) upgrade --install $(GPU_OPERATOR_RELEASE) nvidia/gpu-operator \
		--namespace $(GPU_OPERATOR_NS) --create-namespace \
		-f $(GPU_OPERATOR_VALUES) \
		--wait

.PHONY: cluster-install-dcgm-exporter-monitor
cluster-install-dcgm-exporter-monitor: ## Install ServiceMonitor to scrape GPU Operator DCGM exporter
	$(KUBECTL) apply -f $(DCGM_EXPORTER_SERVICEMONITOR)

.PHONY: cluster-install-prometheus
cluster-install-prometheus: ## Install kube-prometheus-stack (Prometheus + Grafana)
	$(HELM) upgrade --install $(PROM_RELEASE) $(PROM_CHART) \
		--namespace $(MON_NS) --create-namespace \
		-f $(PROM_VALUES) \
		--wait

.PHONY: cluster-install-dcgm-mock
cluster-install-dcgm-mock: ## Install dcgm-mock exporter + ServiceMonitor (default local dev path)
	$(KUBECTL) apply -f $(DCGM_MOCK_MANIFEST)
	$(KUBECTL) -n $(MON_NS) rollout status deploy/dcgm-mock --timeout=120s

.PHONY: cluster-install-real-dcgm
cluster-install-real-dcgm: cluster-gpu cluster-install-gpu-operator cluster-install-dcgm-exporter-monitor ## Install GPU Operator + DCGM exporter + ServiceMonitor (requires Linux + NVIDIA runtime)

.PHONY: cluster-verify
cluster-verify: ## Verify Grafana is reachable + DCGM metric is queryable in Prometheus
	@echo "== Waiting for Grafana to respond =="; \
	for i in {1..60}; do \
		if $(CURL) -fsS "http://localhost:3000/login" >/dev/null 2>&1; then \
			echo "Grafana: OK (http://localhost:3000)"; \
			break; \
		fi; \
		sleep 2; \
	done
	@echo "== Waiting for Prometheus to respond =="; \
	for i in {1..60}; do \
		if $(CURL) -fsS "http://localhost:9090/-/ready" >/dev/null 2>&1; then \
			echo "Prometheus: OK (http://localhost:9090)"; \
			break; \
		fi; \
		sleep 2; \
	done
	@echo "== Waiting for DCGM target to be scraped =="; \
	for i in {1..60}; do \
		if $(CURL) -fsS "http://localhost:9090/api/v1/query?query=up%7Bjob%3D%22dcgm-mock%22%7D" | \
			$(PYTHON) -c 'import json,sys; d=json.load(sys.stdin); r=d.get("data",{}).get("result",[]); assert d.get("status")=="success", d; assert len(r)>0 and float(r[0][\"value\"][1])>=0' >/dev/null 2>&1; then \
			echo "DCGM scrape target: OK (dcgm-mock)"; \
			break; \
		fi; \
		sleep 2; \
	done
	@echo "== Querying DCGM metric in Prometheus =="; \
	for i in {1..30}; do \
		if $(CURL) -fsS "http://localhost:9090/api/v1/query?query=DCGM_FI_DEV_GPU_UTIL" | \
			$(PYTHON) -c 'import json,sys; data=json.load(sys.stdin); res=data.get("data",{}).get("result",[]); assert data.get("status")=="success", data; assert len(res)>0' >/dev/null 2>&1; then \
			echo "DCGM metric: OK (DCGM_FI_DEV_GPU_UTIL)"; \
			exit 0; \
		fi; \
		sleep 2; \
	done; \
	echo "ERROR: No results for DCGM_FI_DEV_GPU_UTIL (scrape may not have happened yet)" >&2; \
	exit 1

.PHONY: cluster-clean-kubefisher-cost
cluster-clean-kubefisher-cost: ## Uninstall KubeFisher Helm release (cost-patcher, rules, pricing, dashboard). Keeps observability stack.
	-$(HELM) uninstall $(CHART_RELEASE) -n $(CHART_NAMESPACE) --ignore-not-found

# Deployment name matches Helm release $(CHART_RELEASE) (see charts/kubefisher/templates/_helpers.tpl fullname).
.PHONY: cluster-clean-test-workloads
cluster-clean-test-workloads: ## Delete dev/test workloads (GPU fake, vLLM mocks, CPU demo) and their ServiceMonitors
	-$(KUBECTL) -n llm-inference delete deploy/gpu-fake-workload deploy/vllm-mock deploy/vllm-costtoken-mock deploy/vllm-cpu-demo --ignore-not-found
	-$(KUBECTL) -n llm-inference delete svc/vllm-mock svc/vllm-costtoken-mock svc/vllm-cpu-demo --ignore-not-found
	-$(KUBECTL) -n llm-inference delete cm/vllm-mock-metrics cm/vllm-costtoken-mock --ignore-not-found
	-$(KUBECTL) -n monitoring delete servicemonitor/vllm-mock servicemonitor/vllm-costtoken-mock --ignore-not-found

.PHONY: cluster-clean-all-apps
cluster-clean-all-apps: cluster-clean-kubefisher-cost cluster-clean-test-workloads ## Uninstall chart + remove all local-dev test/mock workloads (keeps k3d + kube-prometheus-stack)

.PHONY: cluster-clean-operator-crds
cluster-clean-operator-crds: ## Remove TeamInferenceQuota CRDs (delete all TIQ CRs in every namespace first)
	$(MAKE) -C operator uninstall ignore-not-found=true

.PHONY: cluster-clean-operator-deploy
cluster-clean-operator-deploy: ## Remove operator manager/RBAC from `operator` kustomize (`make deploy` / config/default)
	$(MAKE) -C operator undeploy ignore-not-found=true

.PHONY: docker-clean-build-cache
docker-clean-build-cache: ## Prune local Docker build cache (frees disk; slows next build)
	docker builder prune -f

.PHONY: cluster-install-kubefisher-cost
cluster-install-kubefisher-cost: ## Install/upgrade chart (cost-patcher only; requires `make cluster-k3d-import-cost-patcher`)
	$(HELM) upgrade --install $(CHART_RELEASE) $(CHART_DIR) \
		--namespace $(CHART_NAMESPACE) --create-namespace \
		--set image.repository=$(LOCAL_IMAGE_REPO) \
		--set image.tag=$(LOCAL_IMAGE_TAG) \
		--set image.pullPolicy=IfNotPresent \
		--set operator.enabled=false \
		--wait

.PHONY: cluster-install-kubefisher
cluster-install-kubefisher: ## Install/upgrade chart with cost-patcher + operator (requires `make cluster-k3d-import-kubefisher`)
	$(HELM) upgrade --install $(CHART_RELEASE) $(CHART_DIR) \
		--namespace $(CHART_NAMESPACE) --create-namespace \
		--set image.repository=$(LOCAL_IMAGE_REPO) \
		--set image.tag=$(LOCAL_IMAGE_TAG) \
		--set image.pullPolicy=IfNotPresent \
		--set operator.enabled=true \
		--set operator.webhook.enabled=true \
		--set operator.image.repository=$(LOCAL_OPERATOR_IMAGE_REPO) \
		--set operator.image.tag=$(LOCAL_IMAGE_TAG) \
		--set operator.image.pullPolicy=IfNotPresent \
		--wait

##@ Chart (Helm)

.PHONY: chart-lint
chart-lint: ## Lint the kubefisher Helm chart (strict)
	$(HELM) lint $(CHART_DIR) --strict

.PHONY: chart-template
chart-template: ## Render the chart with both CI value files
	$(HELM) template $(CHART_RELEASE) $(CHART_DIR) \
		--namespace $(CHART_NAMESPACE) \
		-f $(CHART_DIR)/ci/default-values.yaml >/dev/null
	$(HELM) template $(CHART_RELEASE) $(CHART_DIR) \
		--namespace $(CHART_NAMESPACE) \
		-f $(CHART_DIR)/ci/observability-disabled-values.yaml >/dev/null
	@echo "chart templates render OK (default + observability-disabled)"

.PHONY: chart-package
chart-package: ## Package the chart into dist/
	@mkdir -p dist
	$(HELM) package $(CHART_DIR) -d dist/

.PHONY: test setup-envtest envtest
test: setup-envtest ## Run root module unit and integration tests (includes adapter envtest)
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" \
		go test ./... -count=1

setup-envtest: envtest ## Download envtest binaries for adapter integration tests
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path >/dev/null || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary

$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef

define gomodver
$(shell go list -m -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' $(1) 2>/dev/null)
endef

.PHONY: operator-test operator-build operator-manifests operator-run
operator-test: ## Run TeamInferenceQuota operator tests (includes envtest)
	$(MAKE) -C operator test

operator-build: ## Build TeamInferenceQuota manager binary
	$(MAKE) -C operator build

operator-manifests: ## Regenerate operator CRDs and RBAC (controller-gen)
	$(MAKE) -C operator manifests generate

operator-run: ## Run operator manager locally (needs kubeconfig and Prometheus)
	$(MAKE) -C operator run

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/m2khosravi/kubefisher/internal/version.Version=$(VERSION)

.PHONY: kubefisher-build
kubefisher-build: ## Build kubefisher CLI binary to bin/kubefisher (sets version from git)
	@mkdir -p bin
	go build -ldflags="$(LDFLAGS)" -o bin/kubefisher ./cmd/kubefisher
	@ln -sf kubefisher bin/kf

.PHONY: cluster-install-cli
cluster-install-cli: kubefisher-build ## One-shot install via CLI: Prometheus + DCGM + cost-patcher (requires helm in PATH)
	./bin/kubefisher install --namespace $(CHART_NAMESPACE)

.PHONY: cost-patcher-image
cost-patcher-image: ## Build local container image for cost-patcher (requires Docker)
	docker build -f build/cost-patcher/Dockerfile -t $(LOCAL_IMAGE_REPO):$(LOCAL_IMAGE_TAG) .

.PHONY: operator-image
operator-image: ## Build local container image for quota operator (requires Docker)
	$(MAKE) -C operator docker-build IMG=$(LOCAL_OPERATOR_IMAGE_REPO):$(LOCAL_IMAGE_TAG)

.PHONY: release-images
release-images: ## Build and push multi-arch images to ghcr.io (set RELEASE_TAG or run on a v* git tag)
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		-f build/cost-patcher/Dockerfile \
		-t $(GHCR_IMAGE_REPO):$(RELEASE_TAG) \
		-t $(GHCR_IMAGE_REPO):latest \
		--push .
	docker buildx build \
		--platform linux/amd64,linux/arm64 \
		-f operator/Dockerfile \
		-t $(GHCR_OPERATOR_IMAGE_REPO):$(RELEASE_TAG) \
		-t $(GHCR_OPERATOR_IMAGE_REPO):latest \
		--push .

.PHONY: cluster-k3d-import-cost-patcher
cluster-k3d-import-cost-patcher: cost-patcher-image ## Import cost-patcher image into k3d (required before in-cluster cost-patcher runs)
	$(K3D) image import $(LOCAL_IMAGE_REPO):$(LOCAL_IMAGE_TAG) -c $(CLUSTER_NAME)

.PHONY: cluster-k3d-import-operator
cluster-k3d-import-operator: operator-image ## Import operator image into k3d (required before Helm operator Deployment runs)
	$(K3D) image import $(LOCAL_OPERATOR_IMAGE_REPO):$(LOCAL_IMAGE_TAG) -c $(CLUSTER_NAME)

.PHONY: cluster-k3d-import-kubefisher
cluster-k3d-import-kubefisher: cluster-k3d-import-cost-patcher cluster-k3d-import-operator ## Import cost-patcher + operator images into k3d

.PHONY: cluster-wait-cost-patcher
cluster-wait-cost-patcher: ## Wait for cost-patcher Deployment rollout in kubefisher-system (name = Helm release $(CHART_RELEASE))
	$(KUBECTL) rollout status deploy/$(CHART_RELEASE) -n $(CHART_NAMESPACE) --timeout=180s

.PHONY: cluster-install-test-gpu-fake
cluster-install-test-gpu-fake: ## Install minimal GPU-requesting workload (no kubefisher labels — tests "unlabeled" sentinel)
	$(KUBECTL) apply -f $(TEST_GPU_FAKE_MANIFEST)
	$(KUBECTL) rollout status deploy/gpu-fake-workload -n llm-inference --timeout=120s

.PHONY: cluster-install-test-gpu-labeled
cluster-install-test-gpu-labeled: ## Install labeled GPU workload (team-test/test-platform/test-model) for recording rule label assertions
	$(KUBECTL) apply -f $(TEST_GPU_LABELED_MANIFEST)
	$(KUBECTL) rollout status deploy/gpu-labeled-workload -n llm-inference --timeout=120s

.PHONY: cluster-install-vllm-cpu-costtest
cluster-install-vllm-cpu-costtest: ## Install deterministic vLLM token counter generator + ServiceMonitor (for cost/token e2e; no CUDA required)
	$(KUBECTL) apply -f $(VLLM_COSTTOKEN_MOCK_MANIFEST)
	$(KUBECTL) apply -f $(VLLM_COSTTOKEN_MOCK_SERVICEMONITOR)
	$(KUBECTL) rollout status deploy/vllm-costtoken-mock -n llm-inference --timeout=120s

.PHONY: cluster-test-send-vllm-costtest-completions
cluster-test-send-vllm-costtest-completions: ## Deprecated for token e2e (mock counters increase automatically)
	@echo "No-op: vllm-costtoken-mock counters increase automatically"

.PHONY: cluster-verify-cost-annotation-hour
cluster-verify-cost-annotation-hour: ## Assert kubefisher.io/cost-per-hour appears on gpu-fake-workload Deployment
	EXPECT_HOUR=1 EXPECT_TOKEN=0 TIMEOUT_SEC=120 bash scripts/verify_cost_patcher.sh llm-inference gpu-fake-workload

.PHONY: cluster-verify-recording-rule-labels
cluster-verify-recording-rule-labels: ## Assert kubefisher:cost_per_hour carries correct labels for labeled and unlabeled pods (requires pf-prometheus or localhost:9090)
	TIMEOUT_SEC=120 bash scripts/verify_recording_rules.sh

.PHONY: cluster-verify-cost-annotation-token
cluster-verify-cost-annotation-token: ## Assert kubefisher.io/cost-per-token (and cost/hr) on vllm-costtoken-mock
	EXPECT_HOUR=1 EXPECT_TOKEN=1 TIMEOUT_SEC=300 bash scripts/verify_cost_patcher.sh llm-inference vllm-costtoken-mock

.PHONY: cluster-e2e-cost-patcher-hour
cluster-e2e-cost-patcher-hour: ## Import image, apply KubeFisher manifests, fake+labeled GPU pods, wait patcher, verify annotations + recording rule labels
	$(MAKE) cluster-k3d-import-cost-patcher
	$(MAKE) cluster-install-kubefisher-cost
	$(MAKE) cluster-install-test-gpu-fake
	$(MAKE) cluster-install-test-gpu-labeled
	$(MAKE) cluster-wait-cost-patcher
	$(MAKE) cluster-verify-cost-annotation-hour
	@echo "== Waiting 65s for Prometheus to evaluate kubefisher:cost_per_hour (30s interval + scrape lag) =="; \
	sleep 65
	$(MAKE) cluster-verify-recording-rule-labels

.PHONY: cluster-e2e-cost-patcher-token
cluster-e2e-cost-patcher-token: ## vLLM token mock + wait for Prom rate window + verify cost/token
	$(MAKE) cluster-k3d-import-cost-patcher
	$(MAKE) cluster-install-kubefisher-cost
	$(MAKE) cluster-install-vllm-cpu-costtest
	$(MAKE) cluster-wait-cost-patcher
	$(MAKE) cluster-test-send-vllm-costtest-completions
	@echo "== Waiting 310s for Prometheus rate(vllm:prompt_tokens_total+generation_tokens_total[5m]) > 0 =="; \
	sleep 310
	$(MAKE) cluster-verify-cost-annotation-token

