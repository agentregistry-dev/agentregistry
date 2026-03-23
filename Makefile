# https://www.gnu.org/software/make/manual/html_node/Special-Variables.html#Special-Variables
.DEFAULT_GOAL := help

# Load .env into make's variable scope if it exists
ifneq (,$(wildcard .env))
  include .env
  export
endif

# Image configuration
DOCKER_REGISTRY ?= localhost:5001
BASE_IMAGE_REGISTRY ?= ghcr.io
DOCKER_REPO ?= agentregistry-dev/agentregistry
DOCKER_BUILDER ?= docker buildx
DOCKER_BUILD_ARGS ?= --push --platform linux/$(LOCALARCH)
BUILD_DATE ?= $(shell date -u '+%Y-%m-%d')
GIT_COMMIT ?= $(shell git rev-parse --short HEAD || echo "unknown")
VERSION ?= $(shell git describe --tags --always 2>/dev/null | grep v || echo "v0.0.0-$(GIT_COMMIT)")
KAGENT_VERSION ?= v0.8.0-beta6
KAGENT_HELM_VERSION ?= $(shell echo $(KAGENT_VERSION) | sed 's/^v//')
KAGENT_NAMESPACE ?= kagent
KAGENT_HELM_CHART_CRDS ?= oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds
KAGENT_HELM_CHART ?= oci://ghcr.io/kagent-dev/kagent/helm/kagent

# Copy .env.example to .env if it doesn't exist
.env:
	cp .env.example .env
	@echo ".env file created"

LDFLAGS := \
	-s -w \
	-X 'github.com/agentregistry-dev/agentregistry/internal/version.Version=$(VERSION)' \
	-X 'github.com/agentregistry-dev/agentregistry/internal/version.GitCommit=$(GIT_COMMIT)' \
	-X 'github.com/agentregistry-dev/agentregistry/internal/version.BuildDate=$(BUILD_DATE)' \
	-X 'github.com/agentregistry-dev/agentregistry/internal/version.DockerRegistry=$(DOCKER_REGISTRY)'

# Local architecture detection to build for the current platform
LOCALARCH ?= $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')

## Helm / Chart settings
# Override HELM if your helm binary lives elsewhere (e.g. HELM=/usr/local/bin/helm).
HELM ?= helm
# CHART_VERSION strips the leading 'v' from VERSION for use in Chart.yaml (Helm requires semver without the prefix).
CHART_VERSION ?= $(shell echo $(VERSION) | sed 's/^v//')
HELM_CHART_DIR ?= ./charts/agentregistry
HELM_PACKAGE_DIR ?= build/charts
HELM_REGISTRY ?= ghcr.io
HELM_REPO ?= agentregistry-dev/agentregistry
HELM_PLUGIN_UNITTEST_URL ?= https://github.com/helm-unittest/helm-unittest
# Pin the helm-unittest plugin version for reproducibility and allow install flags
HELM_PLUGIN_UNITTEST_VERSION ?= v1.0.3
# Although it is not desirable the verify has to be false until the issues linked below are fixed:
# https://github.com/helm/helm/issues/31490
# https://github.com/Azure/setup-helm/issues/239
# It is not entirely clear as to what is causing the issue exactly because the error message
# is not completely clear is it the plugin that does not support the flag or is it helm or both?
HELM_PLUGIN_INSTALL_FLAGS ?= --verify=false
# Help
# `make help` self-documents targets annotated with `##`.
.PHONY: help
help: NAME_COLUMN_WIDTH=35
help: LINE_COLUMN_WIDTH=5
help: ## Output the self-documenting make targets
	@grep -hnE '^[%a-zA-Z0-9_./-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk '{line=$$0; lineno=line; sub(/:.*/, "", lineno); sub(/^[^:]*:/, "", line); target=line; sub(/:.*/, "", target); desc=line; sub(/^.*##[[:space:]]*/, "", desc); printf "\033[36mL%-$(LINE_COLUMN_WIDTH)s%-$(NAME_COLUMN_WIDTH)s\033[0m %s\n", lineno, target, desc}'

# Install UI dependencies
.PHONY: install-ui
install-ui: ## Install UI dependencies
	@echo "Installing UI dependencies..."
	cd ui && npm install

# Build the Next.js UI (outputs to internal/registry/api/ui/dist)
.PHONY: build-ui
build-ui: install-ui ## Build the Next.js UI for embedding
	@echo "Building Next.js UI for embedding..."
	cd ui && npm run build:export
	@echo "Copying built files to internal/registry/api/ui/dist..."
	cp -r ui/out/* internal/registry/api/ui/dist/
# best effort - bring back the gitignore so that dist folder is kept in git (won't work in docker).
	git checkout -- internal/registry/api/ui/dist/.gitignore || :
	@echo "UI built successfully to internal/registry/api/ui/dist/"

# Clean UI build artifacts
.PHONY: clean-ui
clean-ui: ## Clean UI build artifacts
	@echo "Cleaning UI build artifacts..."
	git clean -xdf ./internal/registry/api/ui/dist/
	git clean -xdf ./ui/out/
	git clean -xdf ./ui/.next/
	@echo "UI artifacts cleaned"

# Build the Go CLI
.PHONY: build-cli
build-cli: mod-download ## Build the Go CLI
	@echo "Building Go CLI..."
	@echo "Downloading Go dependencies..."
	@echo "Building binary..."
	go build -ldflags "$(LDFLAGS)" \
		-o bin/arctl cmd/cli/main.go
	@echo "Binary built successfully: bin/arctl"

# Build the Go server (with embedded UI)
.PHONY: build-server
build-server: mod-download ## Build the Go server binary
	@echo "Building Go CLI..."
	@echo "Downloading Go dependencies..."
	@echo "Building binary..."
	go build -ldflags "$(LDFLAGS)" \
		-o bin/arctl-server cmd/server/main.go
	@echo "Binary built successfully: bin/arctl-server"

# Build everything (UI + Go)
.PHONY: build
build: build-ui build-cli ## Build the UI and Go CLI
	@echo "Build complete!"
	@echo "Run './bin/arctl --help' to get started"

# Install the CLI to GOPATH/bin
.PHONY: install
install: build ## Install the CLI to GOPATH/bin
	@echo "Installing arctl to GOPATH/bin..."
	go install
	@echo "Installation complete! Run 'arctl --help' to get started"

# Run Next.js in development mode
.PHONY: dev-ui
dev-ui: ## Run the Next.js UI in development mode
	@echo "Starting Next.js development server..."
	cd ui && npm run dev

# Start local development environment (docker backend only, no Kind)
.PHONY: run-docker
run-docker: local-registry docker docker-tag-as-dev daemon-start ## Start local development environment (docker backend only, no Kind)
	@echo ""
	@echo "agentregistry is running (docker backend):"
	@echo "  UI:  http://localhost:12121"
	@echo "  API: http://localhost:12121/v0"
	@echo "  CLI: $(ARCTL)"
	@echo ""
	@echo "To stop: make down"

# Start local development environment with Kind cluster
.PHONY: run-k8s
run-k8s: local-registry create-kind-cluster build-cli ## Start local development environment with Kind cluster
	@echo ""
	@echo "agentregistry is running (k8s backend):"
	@echo "  UI:  http://localhost:12121"
	@echo "  API: http://localhost:12121/v0"
	@echo "  CLI: $(ARCTL)"
	@echo ""
	@echo "To stop: make down"

# Start local development environment (default: k8s)
.PHONY: run
run: run-k8s # Start local development environment (default: k8s)

# Stop local development environment
.PHONY: down
down: daemon-stop delete-kind-cluster ## Stop the local development environment
	@echo "agentregistry stopped"

GOTESTSUM ?= go tool gotestsum
ARCTL ?= ./bin/arctl

# Manage local daemon lifecycle via CLI helpers.
.PHONY: daemon-start
daemon-start: build-cli ## Start local daemon via CLI
	$(ARCTL) daemon start

.PHONY: daemon-stop
daemon-stop: ## Stop local daemon via CLI
	$(ARCTL) daemon stop

.PHONY: daemon-stop-purge
daemon-stop-purge: ## Stop local daemon and purge volumes via CLI
	$(ARCTL) daemon stop --purge

# Run Go tests (unit tests only)
.PHONY: test-unit
test-unit: ## Run Go unit tests
	@echo "Running Go unit tests..."
	$(GOTESTSUM) --format testdox -- -tags=unit -timeout 5m ./...

# Run Go tests with integration tests
.PHONY: test
test: ## Run Go integration tests
	@echo "Running Go tests with integration..."
	$(GOTESTSUM) --format testdox -- -tags=integration -timeout 10m ./...

# Run e2e tests against docker backend (skips Kind cluster setup and k8s tests)
.PHONY: test-e2e-docker
test-e2e-docker: local-registry docker docker-tag-as-dev daemon-start
	@set -e; \
	  trap '$(MAKE) --no-print-directory daemon-stop-purge >/dev/null 2>&1 || true' EXIT; \
	  ARCTL_API_BASE_URL=http://localhost:12121/v0 E2E_BACKEND=docker GOOGLE_API_KEY=$(GOOGLE_API_KEY) OPENAI_API_KEY=$(OPENAI_API_KEY) \
	    $(GOTESTSUM) --format testdox -- -v -tags=e2e -timeout 45m ./e2e/...

# Run e2e tests against k8s backend (full Kind cluster setup)
.PHONY: test-e2e-k8s
test-e2e-k8s: setup-kind-cluster build-cli
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) E2E_BACKEND=k8s GOOGLE_API_KEY=$(GOOGLE_API_KEY) OPENAI_API_KEY=$(OPENAI_API_KEY) \
	  $(GOTESTSUM) --format testdox -- -v -tags=e2e -timeout 45m ./e2e/...

# Run e2e tests (default: k8s)
.PHONY: test-e2e
test-e2e: ## Run end-to-end tests (default: k8s)
	@if [ "$(E2E_BACKEND)" = "docker" ]; then \
	  $(MAKE) test-e2e-docker; \
	else \
	  $(MAKE) test-e2e-k8s; \
	fi

.PHONY: gen-openapi
gen-openapi: ## Generate the OpenAPI specification
	@echo "Generating OpenAPI spec..."
	go run ./cmd/tools/gen-openapi -output openapi.yaml

gen-client: gen-openapi install-ui ## Generate the TypeScript client
	@echo "Generating TypeScript client..."
	cd ui && npm run generate

# Run Go tests with coverage
.PHONY: test-coverage
test-coverage: ## Run Go tests with coverage
	@echo "Running Go tests with coverage..."
	go test -ldflags "$(LDFLAGS)" -cover ./...

# Run Go tests with coverage report
.PHONY: test-coverage-report
test-coverage-report: ## Run Go tests with an HTML coverage report
	@echo "Running Go tests with coverage report..."
	go test -ldflags "$(LDFLAGS)" -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Clean all build artifacts
.PHONY: clean
clean: clean-ui ## Clean all build artifacts
	@echo "Cleaning Go build artifacts..."
	rm -rf bin/
	go clean
	@echo "All artifacts cleaned"

# Clean and build everything
.PHONY: all
all: clean build ## Clean and build everything
	@echo "Clean build complete!"

# Quick development build (skips cleaning)
.PHONY: dev-build
dev-build: build-ui ## Build quickly for local development
	@echo "Building Go CLI (development mode)..."
	go build -o bin/arctl cmd/cli/main.go
	@echo "Development build complete!"


# Build custom agent gateway image with npx/uvx support
.PHONY: docker-agentgateway
docker-agentgateway: ## Build the custom agent gateway image
	@echo "Building custom agent gateway image..."
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) -f docker/agentgateway.Dockerfile -t $(DOCKER_REGISTRY)/$(DOCKER_REPO)/arctl-agentgateway:$(VERSION) .
	echo "✓ Agent gateway image built successfully";

.PHONY: docker-server
docker-server: .env ## Build the server Docker image
	@echo "Building server Docker image..."
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) -f docker/server.Dockerfile -t $(DOCKER_REGISTRY)/$(DOCKER_REPO)/server:$(VERSION) --build-arg LDFLAGS="$(LDFLAGS)" .
	@echo "✓ Docker image built successfully"

.PHONY: local-registry
local-registry: ## Ensure the local registry (kind-registry) is running on port 5001
	@echo "Ensuring local registry is running on port 5001..."
	@if [ "$$(docker inspect -f '{{.State.Running}}' kind-registry 2>/dev/null || true)" = "true" ]; then \
		echo "kind-registry already running. Skipping." ; \
	elif docker inspect kind-registry >/dev/null 2>&1; then \
		docker start kind-registry ; \
	else \
		docker run \
		-d --restart=always -p "127.0.0.1:5001:5000" --network bridge --name kind-registry "docker.io/library/registry:2" ; \
	fi

.PHONY: docker
docker: docker-agentgateway docker-server ## Build the project Docker images

.PHONY: docker-tag-as-dev
docker-tag-as-dev: ## Tag and push Docker images as :dev
	@echo "Pulling and tagging as dev..."
	docker pull $(DOCKER_REGISTRY)/$(DOCKER_REPO)/server:$(VERSION)
	docker tag $(DOCKER_REGISTRY)/$(DOCKER_REPO)/server:$(VERSION) $(DOCKER_REGISTRY)/$(DOCKER_REPO)/server:dev
	docker push $(DOCKER_REGISTRY)/$(DOCKER_REPO)/server:dev
	docker pull $(DOCKER_REGISTRY)/$(DOCKER_REPO)/arctl-agentgateway:$(VERSION)
	docker tag $(DOCKER_REGISTRY)/$(DOCKER_REPO)/arctl-agentgateway:$(VERSION) $(DOCKER_REGISTRY)/$(DOCKER_REPO)/arctl-agentgateway:dev
	docker push $(DOCKER_REGISTRY)/$(DOCKER_REPO)/arctl-agentgateway:dev
	@echo "✓ Docker image pulled successfully"

KIND_CLUSTER_NAME ?= agentregistry
KIND_IMAGE_VERSION ?= 1.34.0
KIND_CLUSTER_CONTEXT ?= kind-$(KIND_CLUSTER_NAME)
KIND_NAMESPACE ?= agentregistry

# Use placeholder API keys if not set or empty — real inference is not needed for local/CI cluster
# setup. ?= only applies when undefined; the ifeq guards also cover empty strings
GOOGLE_API_KEY ?= fake-key-for-setup
ifeq ($(strip $(GOOGLE_API_KEY)),)
GOOGLE_API_KEY := fake-key-for-setup
endif
OPENAI_API_KEY ?= fake-key-for-setup
ifeq ($(strip $(OPENAI_API_KEY)),)
OPENAI_API_KEY := fake-key-for-setup
endif

.PHONY: create-kind-cluster
create-kind-cluster: local-registry ## Create a local Kind cluster with MetalLB (skips cluster creation if already exists, always runs post-create steps)
	@if go tool kind get clusters 2>/dev/null | grep -qx "$(KIND_CLUSTER_NAME)"; then \
	  echo "Kind cluster '$(KIND_CLUSTER_NAME)' already exists, skipping cluster creation"; \
	else \
	  KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) \
		KIND_IMAGE_VERSION=$(KIND_IMAGE_VERSION) \
		REG_NAME=kind-registry \
		REG_PORT=5001 \
		bash ./scripts/kind/setup-kind.sh; \
	fi
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) bash ./scripts/kind/setup-metallb.sh

.PHONY: delete-kind-cluster
delete-kind-cluster: ## Delete the local Kind cluster (no-op if it does not exist)
	@go tool kind delete cluster --name $(KIND_CLUSTER_NAME) 2>/dev/null || true

.PHONY: prune-kind-cluster
prune-kind-cluster: ## Prune dangling container images from the Kind control-plane node
	@echo "Pruning dangling images from kind control-plane..."
	docker exec $(KIND_CLUSTER_NAME)-control-plane crictl images --no-trunc | \
	awk '$$1=="<none>" && $$2=="<none>" {print $$3}' | \
	while read -r img; do \
	  if [ -n "$$img" ]; then \
	    docker exec $(KIND_CLUSTER_NAME)-control-plane crictl rmi "$$img"; \
	  fi; \
	done || :

.PHONY: kind-debug
kind-debug: ## Shell into Kind control-plane and run btop for resource monitoring
	@echo "Connecting to kind cluster control plane..."
	docker exec -it $(KIND_CLUSTER_NAME)-control-plane bash -c 'apt-get update -qq && apt-get install -y --no-install-recommends btop htop'
	docker exec -it $(KIND_CLUSTER_NAME)-control-plane bash -c 'btop --utf-force'

BUILD ?= true

.PHONY: install-agentregistry
install-agentregistry: charts-generate ## Build images and Helm install AgentRegistry into the Kind cluster (BUILD=false to skip image builds)
ifeq ($(BUILD),true)
install-agentregistry: docker-server docker-agentgateway
endif
	@JWT_KEY=$$(kubectl --context $(KIND_CLUSTER_CONTEXT) -n $(KIND_NAMESPACE) \
	    get secret agentregistry \
	    -o jsonpath='{.data.AGENT_REGISTRY_JWT_PRIVATE_KEY}' 2>/dev/null | base64 -d); \
	  if [ -z "$$JWT_KEY" ]; then JWT_KEY=$$(openssl rand -hex 32); fi; \
	  helm upgrade --install agentregistry charts/agentregistry \
	    --kube-context $(KIND_CLUSTER_CONTEXT) \
	    --namespace $(KIND_NAMESPACE) \
	    --create-namespace \
	    --set image.pullPolicy=Always \
	    --set image.registry=$(DOCKER_REGISTRY) \
	    --set image.repository=$(DOCKER_REPO) \
	    --set image.tag=$(VERSION) \
	    --set config.jwtPrivateKey="$$JWT_KEY" \
	    --set config.enableAnonymousAuth="true" \
	    --set service.type=LoadBalancer \
	    --set database.postgres.bundled.image.repository=pgvector \
	    --set database.postgres.bundled.image.name=pgvector \
	    --set database.postgres.bundled.image.tag=pg16 \
	    --set database.postgres.vectorEnabled=true \
	    --wait \
	    --timeout=5m;

.PHONY: install-kagent
install-kagent: install-kagent-crds install-kagent-controller ## Deploy kagent CRDs and controller into the Kind cluster

.PHONY: install-kagent-crds
install-kagent-crds: ## Deploy kagent CRDs
	$(HELM) upgrade --install kagent-crds $(KAGENT_HELM_CHART_CRDS) \
	  --kube-context $(KIND_CLUSTER_CONTEXT) \
	  --namespace $(KAGENT_NAMESPACE) \
	  --create-namespace \
	  --version $(KAGENT_HELM_VERSION) \
	  --wait \
	  --timeout=2m

.PHONY: install-kagent-controller
install-kagent-controller: ## Deploy kagent controller (minimal, no agents/tools/UI)
	$(HELM) upgrade --install kagent $(KAGENT_HELM_CHART) \
	  --kube-context $(KIND_CLUSTER_CONTEXT) \
	  --namespace $(KAGENT_NAMESPACE) \
	  --version $(KAGENT_HELM_VERSION) \
	  --set ui.replicas=0 \
	  --set kagent-tools.enabled=false \
	  --set tools.grafana-mcp.enabled=false \
	  --set tools.querydoc.enabled=false \
	  --set agents.argo-rollouts-agent.enabled=false \
	  --set agents.cilium-debug-agent.enabled=false \
	  --set agents.cilium-manager-agent.enabled=false \
	  --set agents.cilium-policy-agent.enabled=false \
	  --set agents.helm-agent.enabled=false \
	  --set agents.istio-agent.enabled=false \
	  --set agents.k8s-agent.enabled=false \
	  --set agents.kgateway-agent.enabled=false \
	  --set agents.observability-agent.enabled=false \
	  --set agents.promql-agent.enabled=false \
	  --wait \
	  --timeout=5m

## Set up a full local K8s dev environment (Kind + AgentRegistry with bundled PostgreSQL + kagent).
.PHONY: setup-kind-cluster
setup-kind-cluster: create-kind-cluster install-kagent install-agentregistry ## Set up the full local Kind development environment

.PHONY: dump-kind-state
dump-kind-state: ## Dump Kind cluster state for debugging (pods, events, kagent logs)
	@echo "=== Kind clusters ==="
	@go tool kind get clusters 2>/dev/null || true
	@echo ""
	@echo "=== Pods ==="
	@kubectl get pods -A --context $(KIND_CLUSTER_CONTEXT) 2>/dev/null || true
	@echo ""
	@echo "=== Pod describe ==="
	@kubectl describe pods -A --context $(KIND_CLUSTER_CONTEXT) 2>/dev/null || true
	@echo ""
	@echo "=== AgentRegistry logs ==="
	@kubectl logs deployment/agentregistry -n $(KIND_NAMESPACE) --context $(KIND_CLUSTER_CONTEXT) --tail=100 2>/dev/null || true
	@echo "=== AgentRegistry previous logs ==="
	@kubectl logs deployment/agentregistry -n $(KIND_NAMESPACE) --context $(KIND_CLUSTER_CONTEXT) --tail=100 --previous 2>/dev/null || true
	@echo ""
	@echo "=== Events ==="
	@kubectl get events -A --sort-by='.lastTimestamp' --context $(KIND_CLUSTER_CONTEXT) 2>/dev/null | tail -50 || true
	@echo ""
	@echo "=== Kagent pods ==="
	@kubectl get pods -n kagent --context $(KIND_CLUSTER_CONTEXT) 2>/dev/null || true
	@echo ""
	@echo "=== Kagent controller logs ==="
	@kubectl logs deployment/kagent-controller -n kagent --context $(KIND_CLUSTER_CONTEXT) --tail=100 2>/dev/null || true

bin/arctl-linux-amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/arctl-linux-amd64 cmd/cli/main.go

bin/arctl-linux-amd64.sha256: bin/arctl-linux-amd64
	sha256sum bin/arctl-linux-amd64 > bin/arctl-linux-amd64.sha256

bin/arctl-linux-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/arctl-linux-arm64 cmd/cli/main.go

bin/arctl-linux-arm64.sha256: bin/arctl-linux-arm64
	sha256sum bin/arctl-linux-arm64 > bin/arctl-linux-arm64.sha256

bin/arctl-darwin-amd64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/arctl-darwin-amd64 cmd/cli/main.go

bin/arctl-darwin-amd64.sha256: bin/arctl-darwin-amd64
	sha256sum bin/arctl-darwin-amd64 > bin/arctl-darwin-amd64.sha256

bin/arctl-darwin-arm64:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/arctl-darwin-arm64 cmd/cli/main.go

bin/arctl-darwin-arm64.sha256: bin/arctl-darwin-arm64
	sha256sum bin/arctl-darwin-arm64 > bin/arctl-darwin-arm64.sha256

bin/arctl-windows-amd64.exe:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/arctl-windows-amd64.exe cmd/cli/main.go

bin/arctl-windows-amd64.exe.sha256: bin/arctl-windows-amd64.exe
	sha256sum bin/arctl-windows-amd64.exe > bin/arctl-windows-amd64.exe.sha256

release-cli: ## Build release CLI archives and checksums
release-cli: bin/arctl-linux-amd64.sha256
release-cli: bin/arctl-linux-arm64.sha256
release-cli: bin/arctl-darwin-amd64.sha256
release-cli: bin/arctl-darwin-arm64.sha256
release-cli: bin/arctl-windows-amd64.exe.sha256

GOLANGCI_LINT ?= go tool golangci-lint
GOLANGCI_LINT_ARGS ?= --fix

.PHONY: lint
lint: ## Run golangci-lint linter
	$(GOLANGCI_LINT) run $(GOLANGCI_LINT_ARGS)

.PHONY: lint-ui
lint-ui: install-ui ## Run eslint on UI code
	cd ui && npm run lint

.PHONY: verify
verify: mod-tidy gen-client ## Run all verification checks
	git diff --exit-code

.PHONY: mod-tidy
mod-tidy: ## Run go mod tidy
	go mod tidy

.PHONY: mod-download
mod-download: ## Run go mod download
	go mod download

# ──────────────────────────────────────────────────────────────────────────────
# Helm / Chart targets
# All targets operate on HELM_CHART_DIR (default: ./charts/agentregistry).
# Override with: make charts-test HELM_CHART_DIR=/path/to/chart
# ──────────────────────────────────────────────────────────────────────────────

# Sanity-check that helm is present. Called as a dependency by all chart targets.
.PHONY: _helm-check
_helm-check:
	@if ! command -v $(HELM) >/dev/null 2>&1; then \
	  echo "ERROR: 'helm' not found in PATH."; \
	  echo "  Install Helm from https://helm.sh or set HELM=/path/to/helm"; \
	  exit 1; \
	fi

# Generate Chart.yaml from Chart-template.yaml using envsubst.
.PHONY: charts-generate
charts-generate: ## Generate Chart.yaml from Chart-template.yaml (uses CHART_VERSION, default derived from git tags)
	@echo "Generating $(HELM_CHART_DIR)/Chart.yaml (version=$(CHART_VERSION))..."
	CHART_VERSION=$(CHART_VERSION) envsubst '$$CHART_VERSION' \
	  < $(HELM_CHART_DIR)/Chart-template.yaml \
	  > $(HELM_CHART_DIR)/Chart.yaml

# Build chart dependencies (resolves Chart.yaml dependencies → charts/ subdir).
.PHONY: charts-deps
charts-deps: charts-generate _helm-check ## Build Helm chart dependencies
	@echo "Building Helm chart dependencies for $(HELM_CHART_DIR)..."
	$(HELM) dependency build $(HELM_CHART_DIR)

# Lint chart with --strict so warnings are treated as errors.
.PHONY: charts-lint
charts-lint: charts-generate charts-deps ## Lint the Helm chart with --strict
	@echo "Linting Helm chart $(HELM_CHART_DIR)..."
	$(HELM) lint $(HELM_CHART_DIR) --strict

# Render chart templates to stdout (smoke test — catches template errors).
# Uses minimum required values to pass chart validation.
.PHONY: charts-render-test
charts-render-test: charts-deps ## Render chart templates as a smoke test
	@echo "Rendering chart templates for $(HELM_CHART_DIR)..."
	$(HELM) template test-release $(HELM_CHART_DIR) \
	  --values $(HELM_CHART_DIR)/values.yaml \
	  --set config.jwtPrivateKey=deadbeef1234567890abcdef12345678

# Package the chart into $(HELM_PACKAGE_DIR)/.
.PHONY: charts-package
charts-package: charts-generate charts-lint ## Package the Helm chart into $(HELM_PACKAGE_DIR)
	@mkdir -p $(HELM_PACKAGE_DIR)
	@echo "Packaging chart $(HELM_CHART_DIR) → $(HELM_PACKAGE_DIR)/"
	$(HELM) package $(HELM_CHART_DIR) -d $(HELM_PACKAGE_DIR)
	@echo "Packaged chart(s):"
	@ls -1 $(HELM_PACKAGE_DIR)/*.tgz

# Package the chart and push to an OCI registry. Caller must be logged in.
# Override registry/repo: make charts-push HELM_REGISTRY=ghcr.io HELM_REPO=org/repo
.PHONY: charts-push
charts-push: charts-package _helm-check ## Package and push chart to the configured OCI registry
	@echo "Pushing $(HELM_PACKAGE_DIR)/agentregistry-$(CHART_VERSION).tgz → oci://$(HELM_REGISTRY)/$(HELM_REPO)/charts"
	$(HELM) push "$(HELM_PACKAGE_DIR)/agentregistry-$(CHART_VERSION).tgz" "oci://$(HELM_REGISTRY)/$(HELM_REPO)/charts"

# Generate SHA-256 checksums for all packaged chart files.
.PHONY: charts-checksum
charts-checksum: ## Generate SHA-256 checksum for the packaged chart in $(HELM_PACKAGE_DIR)
	sha256sum "$(HELM_PACKAGE_DIR)/agentregistry-$(CHART_VERSION).tgz" > "$(HELM_PACKAGE_DIR)/checksums.txt"
	@echo "--- checksum ---"
	@cat $(HELM_PACKAGE_DIR)/checksums.txt

# Full Helm release pipeline: test → push (→ lint → package → generate + deps) → checksum.
# Required env vars for the push step: HELM_REGISTRY_PASSWORD (and optionally HELM_REGISTRY_USERNAME).
# Override version: make charts-release CHART_VERSION=1.2.3
.PHONY: charts-release
charts-release: charts-test charts-push charts-checksum ## Full Helm release: lint, test, package, checksum, and push

# Run helm-unittest against charts/agentregistry/tests/*.
# This target:
#   1. checks that 'helm' is present (fails with a clear message if not)
#   2. checks for the helm-unittest plugin and installs it if missing
#   3. runs the full test suite
.PHONY: charts-test
charts-test: charts-generate _helm-check charts-deps helm-unittest-install ## Run helm-unittest chart tests
	@echo "Running helm-unittest on $(HELM_CHART_DIR)..."
	$(HELM) unittest $(HELM_CHART_DIR) --file "tests/*_test.yaml"

.PHONY: helm-unittest-install
helm-unittest-install: _helm-check  ## Install the helm-unittest plugin if needed
	HELM=$(HELM) \
	HELM_PLUGIN_UNITTEST_URL=$(HELM_PLUGIN_UNITTEST_URL) \
	HELM_PLUGIN_UNITTEST_VERSION=$(HELM_PLUGIN_UNITTEST_VERSION) \
	HELM_PLUGIN_INSTALL_FLAGS="$(HELM_PLUGIN_INSTALL_FLAGS)" \
	bash ./scripts/install-helm-unittest.sh
