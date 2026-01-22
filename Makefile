# Image configuration
DOCKER_REGISTRY ?= localhost:5001
BASE_IMAGE_REGISTRY ?= ghcr.io
DOCKER_REPO ?= agentregistry-dev/agentregistry
DOCKER_BUILDER ?= docker buildx
DOCKER_BUILD_ARGS ?= --push --platform linux/$(LOCALARCH)
BUILD_DATE ?= $(shell date -u '+%Y-%m-%d')
GIT_COMMIT ?= $(shell git rev-parse --short HEAD || echo "unknown")
VERSION ?= $(shell git describe --tags --always 2>/dev/null | grep v || echo "v0.0.0-$(GIT_COMMIT)")

LDFLAGS := \
	-s -w \
	-X 'github.com/agentregistry-dev/agentregistry/internal/version.Version=$(VERSION)' \
	-X 'github.com/agentregistry-dev/agentregistry/internal/version.GitCommit=$(GIT_COMMIT)' \
	-X 'github.com/agentregistry-dev/agentregistry/internal/version.BuildDate=$(BUILD_DATE)' \
	-X 'github.com/agentregistry-dev/agentregistry/internal/version.DockerRegistry=$(DOCKER_REGISTRY)'

# Local architecture detection to build for the current platform
LOCALARCH ?= $(shell uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')

.PHONY: help install-ui build-ui clean-ui build-cli build install dev-ui test clean fmt lint all release-cli docker-compose-up docker-compose-down docker-compose-logs

# Default target
help:
	@echo "Available targets:"
	@echo "  install-ui           - Install UI dependencies"
	@echo "  build-ui             - Build the Next.js UI"
	@echo "  clean-ui             - Clean UI build artifacts"
	@echo "  build-cli             - Build the Go CLI"
	@echo "  build                - Build both UI and Go CLI"
	@echo "  install              - Install the CLI to GOPATH/bin"
	@echo "  dev-ui               - Run Next.js in development mode"
	@echo "  test                 - Run Go tests"
	@echo "  clean                - Clean all build artifacts"
	@echo "  all                  - Clean and build everything"
	@echo "  fmt                  - Run the formatter"
	@echo "  lint                 - Run the linter"
	@echo "  release              - Build and release the CLI"

# Install UI dependencies
install-ui:
	@echo "Installing UI dependencies..."
	cd ui && npm install

# Build the Next.js UI (outputs to internal/registry/api/ui/dist)
build-ui: install-ui
	@echo "Building Next.js UI for embedding..."
	cd ui && npm run build:export
	@echo "Copying built files to internal/registry/api/ui/dist..."
	cp -r ui/out/* internal/registry/api/ui/dist/
# best effort - bring back the gitignore so that dist folder is kept in git (won't work in docker).
	git checkout -- internal/registry/api/ui/dist/.gitignore || :
	@echo "UI built successfully to internal/registry/api/ui/dist/"

# Clean UI build artifacts
clean-ui:
	@echo "Cleaning UI build artifacts..."
	git clean -xdf ./internal/registry/api/ui/dist/
	git clean -xdf ./ui/out/
	git clean -xdf ./ui/.next/
	@echo "UI artifacts cleaned"

# Build the Go CLI
build-cli:
	@echo "Building Go CLI..."
	@echo "Downloading Go dependencies..."
	go mod download
	@echo "Building binary..."
	go build -ldflags "$(LDFLAGS)" \
		-o bin/arctl cmd/cli/main.go
	@echo "Binary built successfully: bin/arctl"

# Build the Go server (with embedded UI)
build-server:
	@echo "Building Go CLI..."
	@echo "Downloading Go dependencies..."
	go mod download
	@echo "Building binary..."
	go build -ldflags "$(LDFLAGS)" \
		-o bin/arctl-server cmd/server/main.go
	@echo "Binary built successfully: bin/arctl-server"

# Build everything (UI + Go)
build: build-ui build-cli
	@echo "Build complete!"
	@echo "Run './bin/arctl --help' to get started"

# Install the CLI to GOPATH/bin
install: build
	@echo "Installing arctl to GOPATH/bin..."
	go install
	@echo "Installation complete! Run 'arctl --help' to get started"

# Run Next.js in development mode
dev-ui:
	@echo "Starting Next.js development server..."
	cd ui && npm run dev

# Run Go tests
test:
	@echo "Running Go tests..."
	go test -ldflags "$(LDFLAGS)" -tags=integration -v ./...

# Clean all build artifacts
clean: clean-ui
	@echo "Cleaning Go build artifacts..."
	rm -rf bin/
	go clean
	@echo "All artifacts cleaned"

# Clean and build everything
all: clean build 
	@echo "Clean build complete!"

# Quick development build (skips cleaning)
dev-build: build-ui
	@echo "Building Go CLI (development mode)..."
	go build -o bin/arctl cmd/cli/main.go
	@echo "Development build complete!"


fmt: goimports
	$(GOIMPORT) -w .
	@echo "✓ Formatted code"


# Build custom agent gateway image with npx/uvx support
docker-agentgateway:
	@echo "Building custom age	nt gateway image..."
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) -f docker/agentgateway.Dockerfile -t $(DOCKER_REGISTRY)/$(DOCKER_REPO)/arctl-agentgateway:$(VERSION) .
	echo "✓ Agent gateway image built successfully";


docker-server:
	@echo "Building server Docker image..."
	$(DOCKER_BUILDER) build $(DOCKER_BUILD_ARGS) -f docker/server.Dockerfile -t $(DOCKER_REGISTRY)/$(DOCKER_REPO)/server:$(VERSION) --build-arg LDFLAGS="$(LDFLAGS)" .
	@echo "✓ Docker image built successfully"


docker-registry:
	@echo "Building running local Docker registry..."
	if docker inspect docker-registry >/dev/null 2>&1; then \
		echo "Registry already running. Skipping build." ; \
	else \
		 docker run \
		-d --restart=always -p "5001:5000" --name docker-registry "docker.io/library/registry:2" ; \
	fi

docker: docker-agentgateway docker-server

docker-tag-as-dev:
	@echo "Pulling and tagging as dev..."
	docker pull $(DOCKER_REGISTRY)/$(DOCKER_REPO)/server:$(VERSION)
	docker tag $(DOCKER_REGISTRY)/$(DOCKER_REPO)/server:$(VERSION) $(DOCKER_REGISTRY)/$(DOCKER_REPO)/server:dev
	docker push $(DOCKER_REGISTRY)/$(DOCKER_REPO)/server:dev
	docker pull $(DOCKER_REGISTRY)/$(DOCKER_REPO)/arctl-agentgateway:$(VERSION)
	docker tag $(DOCKER_REGISTRY)/$(DOCKER_REPO)/arctl-agentgateway:$(VERSION) $(DOCKER_REGISTRY)/$(DOCKER_REPO)/arctl-agentgateway:dev
	docker push $(DOCKER_REGISTRY)/$(DOCKER_REPO)/arctl-agentgateway:dev
	@echo "✓ Docker image pulled successfully"

docker-compose-up: docker docker-tag-as-dev
	@echo "Starting services with Docker Compose..."
	VERSION=$(VERSION) DOCKER_REGISTRY=$(DOCKER_REGISTRY) docker compose -p agentregistry -f internal/daemon/docker-compose.yml up -d --wait --pull always

docker-compose-down:
	VERSION=$(VERSION) DOCKER_REGISTRY=$(DOCKER_REGISTRY) docker compose -p agentregistry -f internal/daemon/docker-compose.yml down

docker-compose-rm:
	VERSION=$(VERSION) DOCKER_REGISTRY=$(DOCKER_REGISTRY) docker compose -p agentregistry -f internal/daemon/docker-compose.yml rm --volumes --force

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

release-cli: bin/arctl-linux-amd64.sha256  
release-cli: bin/arctl-linux-arm64.sha256  
release-cli: bin/arctl-darwin-amd64.sha256  
release-cli: bin/arctl-darwin-arm64.sha256  
release-cli: bin/arctl-windows-amd64.exe.sha256

.PHONY: lint
lint: golangci-lint ## Run golangci-lint linter
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	$(GOLANGCI_LINT) config verify

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)


GOIMPORT = $(LOCALBIN)/goimports
GOIMPORT_VERSION ?= v0.41

GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
GOLANGCI_LINT_VERSION ?= v2.8.0

.PHONY: goimports
goimports: $(GOIMPORT) ## Download goimports locally if necessary.
$(GOIMPORT): $(LOCALBIN)
	$(call go-install-tool,$(GOIMPORT),golang.org/x/tools/cmd/goimports,$(GOIMPORT_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef