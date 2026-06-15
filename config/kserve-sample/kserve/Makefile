SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help

# Config
K3D_CONFIG ?= config/k3d-config.yaml
CLUSTER_NAME ?= kubefisher-dev

KUBECTL ?= kubectl
HELM ?= helm
K3D ?= k3d

CERT_MANAGER_VERSION ?= v1.15.5
ENVOY_GATEWAY_HELM_VERSION ?= v1.2.6
KSERVE_VERSION ?= v0.17.0
GATEWAY_API_VERSION ?= v1.2.1

.PHONY: help
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_.-]+:.*##/ { printf "  %-28s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

##@ Local cluster

.PHONY: cluster-up
cluster-up: ## Create the local k3d dev cluster
	$(K3D) cluster create --config $(K3D_CONFIG)
	$(MAKE) cluster-gpu
	$(MAKE) cluster-install
	$(MAKE) cluster-verify
	$(KUBECTL) get nodes

.PHONY: cluster-down
cluster-down: ## Destroy the local k3d cluster
	$(K3D) cluster delete $(CLUSTER_NAME)

.PHONY: cluster-reset
cluster-reset: cluster-down cluster-up ## Destroy and recreate from scratch

.PHONY: cluster-install
cluster-install: \
	cluster-install-cert-manager \
	cluster-install-gateway-api \
	cluster-install-envoy-gateway \
	cluster-install-kserve \
	cluster-install-keda \
	cluster-install-prometheus ## Install cluster dependencies for dev

.PHONY: cluster-gpu
cluster-gpu: cluster-label-gpu cluster-patch-gpu ## Apply fake GPU labels + capacity

.PHONY: cluster-label-gpu
cluster-label-gpu: ## Add fake GPU labels to agent nodes
	$(KUBECTL) label node k3d-$(CLUSTER_NAME)-agent-0 \
		accelerator=nvidia-a10g \
		cloud.google.com/gke-accelerator=nvidia-tesla-a10 --overwrite
	$(KUBECTL) label node k3d-$(CLUSTER_NAME)-agent-1 \
		accelerator=nvidia-a10g \
		cloud.google.com/gke-accelerator=nvidia-tesla-a10 \
		kubefisher.io/spot=true --overwrite

.PHONY: cluster-patch-gpu
cluster-patch-gpu: ## Patch node capacity to advertise fake GPUs
	@$(KUBECTL) proxy --port=8001 >/tmp/kubectl-proxy-$(CLUSTER_NAME).log 2>&1 & \
	pid="$$!"; \
	trap 'kill "$$pid" >/dev/null 2>&1 || true' EXIT; \
	sleep 2; \
	for n in 0 1; do \
		curl -fsS --header "Content-Type: application/json-patch+json" \
			--request PATCH \
			--data '[{"op":"add","path":"/status/capacity/nvidia.com~1gpu","value":"2"}]' \
			http://localhost:8001/api/v1/nodes/k3d-$(CLUSTER_NAME)-agent-$$n/status >/dev/null; \
	done

.PHONY: helm-repos
helm-repos: ## Add/update required Helm repos
	@$(HELM) repo add jetstack https://charts.jetstack.io >/dev/null 2>&1 || true
	@$(HELM) repo add kedacore https://kedacore.github.io/charts >/dev/null 2>&1 || true
	@$(HELM) repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
	@$(HELM) repo update

.PHONY: cluster-install-cert-manager
cluster-install-cert-manager: ## Install cert-manager (required by KServe)
	$(MAKE) helm-repos
	$(HELM) upgrade --install cert-manager jetstack/cert-manager \
		--namespace cert-manager --create-namespace \
		--version $(CERT_MANAGER_VERSION) \
		--set crds.enabled=true \
		--wait

.PHONY: cluster-install-gateway-api
cluster-install-gateway-api: ## Install Kubernetes Gateway API CRDs
	$(KUBECTL) apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/$(GATEWAY_API_VERSION)/standard-install.yaml

.PHONY: cluster-install-envoy-gateway
cluster-install-envoy-gateway: ## Install Envoy Gateway (Gateway API controller)
	$(HELM) upgrade --install eg oci://docker.io/envoyproxy/gateway-helm \
		--namespace envoy-gateway-system --create-namespace \
		--version $(ENVOY_GATEWAY_HELM_VERSION) \
		--wait

.PHONY: cluster-install-kserve
cluster-install-kserve: ## Install KServe (standard/raw mode, Gateway API enabled)
	$(HELM) upgrade --install kserve-crd oci://ghcr.io/kserve/charts/kserve-crd \
		--namespace kserve --create-namespace \
		--version $(KSERVE_VERSION) \
		--wait
	# Work around Helm templated-CRD upgrade behavior: ensure CSC CRD exists before kserve-resources.
	$(KUBECTL) apply -f https://raw.githubusercontent.com/kserve/kserve/$(KSERVE_VERSION)/charts/kserve-crd/files/serving.kserve.io_clusterstoragecontainers.yaml
	$(KUBECTL) wait --for=condition=Established crd/clusterstoragecontainers.serving.kserve.io --timeout=60s
	$(HELM) upgrade --install kserve oci://ghcr.io/kserve/charts/kserve-resources \
		--namespace kserve \
		--version $(KSERVE_VERSION) \
		--set kserve.controller.deploymentMode=Standard \
		--set kserve.controller.gateway.ingressGateway.enableGatewayApi=true \
		--set kserve.controller.gateway.ingressGateway.createGateway=true \
		--set kserve.controller.gateway.ingressGateway.kserveGateway=kserve/kserve-ingress-gateway \
		--wait
	$(MAKE) cluster-install-kserve-runtimes

.PHONY: cluster-install-kserve-runtimes
cluster-install-kserve-runtimes: ## Install built-in runtimes used by samples
	$(KUBECTL) apply -f config/runtimes/kserve-sklearnserver.yaml

.PHONY: cluster-install-keda
cluster-install-keda: ## Install KEDA
	$(MAKE) helm-repos
	$(HELM) upgrade --install keda kedacore/keda \
		--namespace keda --create-namespace \
		--wait

.PHONY: cluster-install-prometheus
cluster-install-prometheus: ## Install kube-prometheus-stack
	$(MAKE) helm-repos
	$(HELM) upgrade --install prometheus prometheus-community/kube-prometheus-stack \
		--namespace monitoring --create-namespace \
		--wait

.PHONY: cluster-status
cluster-status: ## Show node status and labels
	$(KUBECTL) get nodes --show-labels
	$(KUBECTL) get nodes -o custom-columns=NAME:.metadata.name,GPUs:.status.capacity.nvidia\\.com/gpu

.PHONY: cluster-verify
cluster-verify: ## Verify cluster has KServe + KEDA + fake GPUs
	@echo "== Nodes =="; \
	$(KUBECTL) get nodes; \
	echo ""; \
	echo "== Fake GPU capacity =="; \
	$(KUBECTL) get nodes -o custom-columns=NAME:.metadata.name,GPUs:.status.capacity.nvidia\\.com/gpu; \
	echo ""; \
	echo "== CRDs =="; \
	$(KUBECTL) get crd inferenceservices.serving.kserve.io >/dev/null; \
	$(KUBECTL) get crd servingruntimes.serving.kserve.io >/dev/null; \
	$(KUBECTL) get crd clusterstoragecontainers.serving.kserve.io >/dev/null; \
	$(KUBECTL) get crd scaledobjects.keda.sh >/dev/null; \
	echo "OK"

##@ Development

.PHONY: test-serve
test-serve: ## Deploy sklearn-iris and curl predict endpoint
	@ns="kserve-test"; \
	name="sklearn-iris"; \
	local_port="8585"; \
	echo "== Applying InferenceService =="; \
	$(KUBECTL) get ns "$$ns" >/dev/null 2>&1 || $(KUBECTL) create ns "$$ns"; \
	$(KUBECTL) apply -f config/samples/sklearn-iris.yaml; \
	echo ""; \
	echo "== Waiting for predictor Pod to be Ready (gateway may still be unready) =="; \
	$(KUBECTL) wait -n "$$ns" --for=condition=Ready pod -l "app=isvc.$$name-predictor" --timeout=600s; \
	echo ""; \
	sleep 30; \
	echo "== Finding service to port-forward =="; \
	svc="$$( $(KUBECTL) get svc -n "$$ns" -l "serving.kserve.io/inferenceservice=$$name" -o jsonpath='{.items[0].metadata.name}' )"; \
	if [[ -z "$$svc" ]]; then echo "ERROR: no Service found for InferenceService=$$name" >&2; exit 1; fi; \
	echo "Service: $$svc"; \
	echo ""; \
	echo "== Port-forwarding and calling predict =="; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp" >/dev/null 2>&1 || true' EXIT; \
	pflog="$$tmp/port-forward.log"; \
	printf '%s\n' \
		'{' \
		'  "instances": [' \
		'    [6.8,  2.8,  4.8,  1.4],' \
		'    [6.0,  3.4,  4.5,  1.6]' \
		'  ]' \
		'}' > "$$tmp/iris-input.json"; \
	$(KUBECTL) -n "$$ns" port-forward --address 127.0.0.1 "svc/$$svc" "$$local_port:80" >"$$pflog" 2>&1 & \
	pid="$$!"; \
	trap 'kill "$$pid" >/dev/null 2>&1 || true' EXIT; \
	for i in {1..30}; do \
		if curl -fsS "http://127.0.0.1:$$local_port/" >/dev/null 2>&1; then break; fi; \
		if ! kill -0 "$$pid" >/dev/null 2>&1; then \
			echo "ERROR: port-forward exited early"; \
			sed -n '1,120p' "$$pflog" || true; \
			exit 1; \
		fi; \
		sleep 1; \
	done; \
	for i in {1..30}; do \
		if curl -fsS -H "Content-Type: application/json" "http://127.0.0.1:$$local_port/v1/models/$$name:predict" -d @"$$tmp/iris-input.json"; then \
			echo ""; \
			echo "OK"; \
			exit 0; \
		fi; \
		sleep 2; \
	done; \
	echo "ERROR: predict request did not succeed in time" >&2; \
	sed -n '1,120p' "$$pflog" || true; \
	exit 1

.PHONY: dev-run
dev-run: manifests generate ## Run controller locally against k3d
	go run ./cmd/main.go

.PHONY: gen
gen: ## Regenerate manifests and deepcopy
	make manifests generate