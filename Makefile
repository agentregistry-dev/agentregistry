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
HELM_CHART_DIR ?= ./charts/agentregistry
HELM_PACKAGE_DIR ?= build/charts
HELM_REGISTRY ?= ghcr.io
HELM_REPO ?= agentregistry-dev/agentregistry
# HELM_PUSH_MODE: oci (default, recommended) | repo (legacy chart repo / ChartMuseum)
HELM_PUSH_MODE ?= oci
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

# Start local development environment (docker-compose)
.PHONY: run
run: docker-registry docker-compose-up build-cli ## Start the local development environment
	@echo ""
	@echo "agentregistry is running:"
	@echo "  UI:  http://localhost:12121"
	@echo "  API: http://localhost:12121/v0"
	@echo "  CLI: ./bin/arctl"
	@echo ""
	@echo "To stop: make down"

# Stop local development environment
.PHONY: down
down: docker-compose-down ## Stop the local development environment
	@echo "agentregistry stopped"

# Run Go tests (unit tests only)
.PHONY: test-unit
test-unit: ## Run Go unit tests
	@echo "Running Go unit tests..."
	go tool gotestsum --format testdox -- -tags=unit -timeout 5m ./...

# Run Go tests with integration tests
.PHONY: test
test: ## Run Go integration tests
	@echo "Running Go tests with integration..."
	go tool gotestsum --format testdox -- -tags=integration -timeout 10m ./...

e2e: build-cli ## Run end-to-end tests
	go tool gotestsum --format testdox -- -tags=e2e -timeout 45m ./e2e/...

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
	@echo "Building custom age	nt gateway image..."
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) -f docker/agentgateway.Dockerfile -t $(DOCKER_REGISTRY)/$(DOCKER_REPO)/arctl-agentgateway:$(VERSION) .
	echo "✓ Agent gateway image built successfully";

.PHONY: docker-server
docker-server: .env ## Build the server Docker image
	@echo "Building server Docker image..."
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) -f docker/server.Dockerfile -t $(DOCKER_REGISTRY)/$(DOCKER_REPO)/server:$(VERSION) --build-arg LDFLAGS="$(LDFLAGS)" .
	@echo "✓ Docker image built successfully"

.PHONY: docker-registry
docker-registry: ## Ensure the local Docker registry is running
	@echo "Building running local Docker registry..."
	if docker inspect docker-registry >/dev/null 2>&1; then \
		echo "Registry already running. Skipping build." ; \
	else \
		 docker run \
		-d --restart=always -p "5001:5000" --name docker-registry "docker.io/library/registry:2" ; \
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

.PHONY: docker-compose-up
docker-compose-up: docker docker-tag-as-dev ## Start services with Docker Compose
	@echo "Starting services with Docker Compose..."
	VERSION=$(VERSION) DOCKER_REGISTRY=$(DOCKER_REGISTRY) docker compose -p agentregistry -f internal/daemon/docker-compose.yml up -d --wait --pull always

.PHONY: docker-compose-down
docker-compose-down: ## Stop services managed by Docker Compose
	VERSION=$(VERSION) DOCKER_REGISTRY=$(DOCKER_REGISTRY) docker compose -p agentregistry -f internal/daemon/docker-compose.yml down

.PHONY: docker-compose-rm
docker-compose-rm: ## Remove Docker Compose services and volumes
	VERSION=$(VERSION) DOCKER_REGISTRY=$(DOCKER_REGISTRY) docker compose -p agentregistry -f internal/daemon/docker-compose.yml rm --volumes --force

KIND_CLUSTER_NAME ?= agentregistry
KIND_IMAGE_VERSION ?= 1.34.0
KIND_CLUSTER_CONTEXT ?= kind-$(KIND_CLUSTER_NAME)
KIND_NAMESPACE ?= agentregistry
KIND_BIN = $(shell test -x bin/kind && echo bin/kind || echo kind)
KUBECONFIG_PERM ?= $(shell \
  if [ "$$(uname -s | tr '[:upper:]' '[:lower:]')" = "darwin" ]; then \
    stat -f "%Lp" ~/.kube/config 2>/dev/null || echo "600"; \
  else \
    stat -c "%a" ~/.kube/config 2>/dev/null || echo "600"; \
  fi)

.PHONY: install-tools
install-tools: ## Install required tools into ./bin
	@mkdir -p bin
	GOBIN=$(PWD)/bin go install sigs.k8s.io/kind@v0.27.0
	@echo "✓ kind installed to bin/kind"

.PHONY: create-kind-cluster
create-kind-cluster: install-tools ## Create a local Kind cluster with MetalLB
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) KIND_IMAGE_VERSION=$(KIND_IMAGE_VERSION) bash ./scripts/kind/setup-kind.sh
	KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) bash ./scripts/kind/setup-metallb.sh

.PHONY: use-kind-cluster
use-kind-cluster: ## Merge kind kubeconfig and set default namespace
	@TMP_KUBECONFIG=$$(mktemp) && chmod 600 $$TMP_KUBECONFIG && \
	  $(KIND_BIN) get kubeconfig --name $(KIND_CLUSTER_NAME) > $$TMP_KUBECONFIG && \
	  KUBECONFIG=~/.kube/config:$$TMP_KUBECONFIG kubectl config view --merge --flatten > ~/.kube/config.tmp && \
	  mv ~/.kube/config.tmp ~/.kube/config && chmod $(KUBECONFIG_PERM) ~/.kube/config && \
	  rm -f $$TMP_KUBECONFIG
	kubectl --context $(KIND_CLUSTER_CONTEXT) create namespace $(KIND_NAMESPACE) || true
	kubectl --context $(KIND_CLUSTER_CONTEXT) config set-context --current --namespace $(KIND_NAMESPACE) || true

.PHONY: delete-kind-cluster
delete-kind-cluster: ## Delete the local Kind cluster
	$(KIND_BIN) delete cluster --name $(KIND_CLUSTER_NAME)

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

.PHONY: install-postgresql
install-postgresql: ## Deploy standalone PostgreSQL/pgvector into the Kind cluster
	kubectl --context $(KIND_CLUSTER_CONTEXT) apply -f examples/postgres-pgvector.yaml
	kubectl --context $(KIND_CLUSTER_CONTEXT) -n agentregistry wait --for=condition=ready pod -l app=postgres-pgvector --timeout=120s

BUILD ?= true

.PHONY: install-agentregistry
install-agentregistry: ## Build images and Helm install AgentRegistry into the Kind cluster (BUILD=false to skip image builds)
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
	    --set image.repository=$(DOCKER_REPO)/server \
	    --set image.tag=$(VERSION) \
	    --set database.host=postgres-pgvector.$(KIND_NAMESPACE).svc.cluster.local \
	    --set database.password=agentregistry \
	    --set database.sslMode=disable \
	    --set config.jwtPrivateKey="$$JWT_KEY" \
	    --set config.enableAnonymousAuth="true" \
	    --wait \
	    --timeout=5m;

## Set up a full local K8s dev environment (Kind + PostgreSQL/pgvector + AgentRegistry)
.PHONY: setup-kind-cluster
setup-kind-cluster: create-kind-cluster install-postgresql install-agentregistry ## Set up the full local Kind development environment

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

# Build chart dependencies (resolves Chart.yaml dependencies → charts/ subdir).
.PHONY: charts-deps
charts-deps: _helm-check ## Build Helm chart dependencies
	@echo "Building Helm chart dependencies for $(HELM_CHART_DIR)..."
	$(HELM) dependency build $(HELM_CHART_DIR)

# Lint chart with --strict so warnings are treated as errors.
.PHONY: charts-lint
charts-lint: charts-deps ## Lint the Helm chart with --strict
	@echo "Linting Helm chart $(HELM_CHART_DIR)..."
	$(HELM) lint $(HELM_CHART_DIR) --strict

# Render chart templates to stdout (smoke test — catches template errors).
# Uses minimum required values to pass chart validation.
.PHONY: charts-render-test
charts-render-test: charts-deps ## Render chart templates as a smoke test
	@echo "Rendering chart templates for $(HELM_CHART_DIR)..."
	$(HELM) template test-release $(HELM_CHART_DIR) \
	  --values $(HELM_CHART_DIR)/values.yaml \
	  --set config.jwtPrivateKey=deadbeef1234567890abcdef12345678 \
	  --set database.password=ci-password \
	  --set database.host=postgres.example.com

# Package the chart into $(HELM_PACKAGE_DIR)/.
.PHONY: charts-package
charts-package: charts-lint ## Package the Helm chart into $(HELM_PACKAGE_DIR)
	@mkdir -p $(HELM_PACKAGE_DIR)
	@echo "Packaging chart $(HELM_CHART_DIR) → $(HELM_PACKAGE_DIR)/"
	$(HELM) package $(HELM_CHART_DIR) -d $(HELM_PACKAGE_DIR)
	@echo "Packaged chart(s):"
	@ls -1 $(HELM_PACKAGE_DIR)/*.tgz

# Push packaged chart(s) to an OCI registry.
# Credentials are read from the environment at runtime (never stored in the Makefile):
#   HELM_REGISTRY_USERNAME   – registry username (default: your shell $USER)
#   HELM_REGISTRY_PASSWORD   – registry password / token (required)
# Override registry/repo: make charts-push HELM_REGISTRY=ghcr.io HELM_REPO=org/repo
.PHONY: charts-push
charts-push: charts-package ## Push packaged charts to the configured registry
	@echo "Pushing chart(s) to $(HELM_REGISTRY)/$(HELM_REPO)/charts (mode: $(HELM_PUSH_MODE))"
ifeq ($(HELM_PUSH_MODE),oci)
	@if [ -z "$$HELM_REGISTRY_PASSWORD" ]; then \
	  echo "ERROR: HELM_REGISTRY_PASSWORD is not set. Export it before running this target."; \
	  exit 1; \
	fi
	@printf "%s" "$$HELM_REGISTRY_PASSWORD" | $(HELM) registry login $(HELM_REGISTRY) \
	  --username "$${HELM_REGISTRY_USERNAME:-$$USER}" \
	  --password-stdin
	@for pkg in $(HELM_PACKAGE_DIR)/*.tgz; do \
	  [ -f "$$pkg" ] || continue; \
	  echo "  Pushing $$pkg → oci://$(HELM_REGISTRY)/$(HELM_REPO)/charts"; \
	  $(HELM) push "$$pkg" "oci://$(HELM_REGISTRY)/$(HELM_REPO)/charts"; \
	done
	@$(HELM) registry logout $(HELM_REGISTRY) || true
else
	@echo "Non-OCI push (mode=$(HELM_PUSH_MODE)) — implement repo-specific push logic or use chart-releaser."
	@exit 1
endif

# Run helm-unittest against charts/agentregistry/tests/*.
# This target:
#   1. checks that 'helm' is present (fails with a clear message if not)
#   2. checks for the helm-unittest plugin and installs it if missing
#   3. runs the full test suite
.PHONY: charts-test
charts-test: _helm-check charts-deps helm-unittest-install ## Run helm-unittest chart tests
	@echo "Running helm-unittest on $(HELM_CHART_DIR)..."
	$(HELM) unittest $(HELM_CHART_DIR) --file "tests/*_test.yaml"

.PHONY: helm-unittest-install
helm-unittest-install: _helm-check ## Install the helm-unittest plugin if needed
	@echo "Checking for helm-unittest plugin..."
	@if ! $(HELM) plugin list | awk '{print $$1}' | grep -q '^unittest$$'; then \
	  echo "helm-unittest plugin not found — installing from $(HELM_PLUGIN_UNITTEST_URL)"; \
	  if $(HELM) plugin install $(HELM_PLUGIN_UNITTEST_URL) --version $(HELM_PLUGIN_UNITTEST_VERSION) $(HELM_PLUGIN_INSTALL_FLAGS) ; then \
	    echo "helm-unittest installed (with HELM_PLUGIN_INSTALL_FLAGS)"; \
	  else \
	    echo "Install with HELM_PLUGIN_INSTALL_FLAGS failed; retrying without flags..."; \
	    if $(HELM) plugin install $(HELM_PLUGIN_UNITTEST_URL) --version $(HELM_PLUGIN_UNITTEST_VERSION) ; then \
	      echo "helm-unittest installed (without HELM_PLUGIN_INSTALL_FLAGS)"; \
	    else \
	      echo "ERROR: helm-unittest install failed. Check network / plugin URL."; exit 1; \
	    fi; \
	  fi; \
	else \
	  echo "helm-unittest plugin already installed"; \
	fi

# Convenience: package → push → test.
.PHONY: charts-all
charts-all: charts-push charts-test ## Package, push, and test the Helm chart
	@echo "charts-all complete: packaged, pushed, and tested."
