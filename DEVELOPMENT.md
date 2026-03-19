# Development Guide

## Local Kubernetes Environment

The fastest way to run the full stack locally is with [Kind](https://kind.sigs.k8s.io/). A single `make` target creates the cluster, builds the server image, and installs AgentRegistry via Helm — a PostgreSQL instance with pgvector is bundled and deployed automatically by the Helm chart for local development.

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm](https://helm.sh/docs/intro/install/)
- [envsubst](https://www.gnu.org/software/gettext/manual/html_node/envsubst-Invocation.html) (used to generate `Chart.yaml`)

> `kind` is installed automatically into `./bin/kind` by `make install-tools` — no manual installation needed.

### Full setup

```bash
make setup-kind-cluster
```

This runs two steps in order:

| Step | Target | What it does |
|------|--------|-------------|
| 1 | `create-kind-cluster` | Installs `kind` to `./bin/`, creates Kind cluster + local registry (`localhost:5001`) + MetalLB |
| 2 | `install-agentregistry` | Builds server image, pushes to local registry, Helm installs AgentRegistry (bundled PostgreSQL with pgvector override for local dev) |

Each target can also be run independently — useful when iterating on code:

```bash
# Rebuild and redeploy after a code change (cluster and PG stay up)
make install-agentregistry

# Skip image builds if the images are already up to date
make install-agentregistry BUILD=false
```

`install-agentregistry` automatically runs `charts-generate` first (see [Helm Chart Generation](#helm-chart-generation) below), so `Chart.yaml` is always up to date before deploying.

On subsequent runs, `install-agentregistry` reuses the `jwtPrivateKey` already stored in the cluster secret so tokens remain valid across redeploys.

### Accessing the services

```bash
# AgentRegistry API/UI
kubectl --context kind-agentregistry port-forward -n agentregistry svc/agentregistry 12121:12121
# open http://localhost:12121

# Bundled PostgreSQL (for direct inspection)
kubectl --context kind-agentregistry port-forward -n agentregistry svc/agentregistry-postgresql 5432:5432
psql -h localhost -U agentregistry -d agentregistry
```

### Teardown

```bash
make delete-kind-cluster
```

See [`scripts/kind/README.md`](scripts/kind/README.md) for more detail on configuration, troubleshooting, and overriding defaults.

---

## Helm Chart Generation

`charts/agentregistry/Chart.yaml` is **generated** from `charts/agentregistry/Chart-template.yaml` using `envsubst` and is not committed to the repository. Any `helm` command run directly against the chart directory will fail unless `Chart.yaml` exists.

### Generating Chart.yaml locally

```bash
# Generate with version derived from the latest git tag (e.g. 0.3.0)
make charts-generate

# Generate with an explicit version
make charts-generate CHART_VERSION=0.4.0
```

`CHART_VERSION` defaults to the output of `git describe --tags --abbrev=0` with the leading `v` stripped. If there are no tags, set it explicitly.

Any Makefile target that needs `Chart.yaml` (e.g. `charts-lint`, `charts-test`, `charts-package`, `install-agentregistry`) declares `charts-generate` as a prerequisite and will generate it automatically. You only need to run `make charts-generate` directly if you're invoking `helm` commands by hand.

### Adding Chart.yaml to your editor's ignore hints

Because `charts/agentregistry/Chart.yaml` is gitignored, some editors may flag it as untracked. This is expected — treat `Chart-template.yaml` as the source of truth and do not commit the generated `Chart.yaml`.

### Helm release pipeline

The full release pipeline is encapsulated in a single target:

```bash
# Requires HELM_REGISTRY_PASSWORD to be set; optionally HELM_REGISTRY_USERNAME
make charts-release CHART_VERSION=0.4.0
```

This runs in order: `charts-test` → `charts-push` (lint → package → push) → `charts-checksum`.

---

## Local Docker Compose Environment

```bash
make run   # starts registry server + daemon via docker-compose
make down  # stops everything
```

The UI is available at `http://localhost:12121`.

---

# Architecture Overview

**Tech stack:** Go 1.25+ · PostgreSQL + pgvector (pgx) · [Huma](https://huma.rocks/) (OpenAPI) · [Cobra](https://cobra.dev/) (CLI) · Next.js 14 (App Router) · Tailwind CSS · shadcn/ui

For a detailed breakdown of layers, conventions, and contribution guidelines see [`AGENTS.md`](AGENTS.md).

## Build Process

```bash
# UI only (hot reload)
make dev-ui

# Build CLI binary
make build-cli

# Build server binary
make build-server

# Build UI static assets
make build-ui
```

## Resources

- [Cobra Documentation](https://cobra.dev/)
- [Huma Documentation](https://huma.rocks/)
- [Next.js Documentation](https://nextjs.org/docs)
- [shadcn/ui Components](https://ui.shadcn.com/)
- [MCP Protocol Specification](https://spec.modelcontextprotocol.io/)

