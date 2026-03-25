# Local Kubernetes Development Environment

This directory contains scripts for running AgentRegistry in a local [Kind](https://kind.sigs.k8s.io/) cluster.

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm](https://helm.sh/docs/intro/install/)
- [Make](https://www.gnu.org/software/make/)

## Quick Start

```bash
make setup-kind-cluster
```

This single command sets up the full local environment.

## What It Does

`make setup-kind-cluster` runs the following sub-targets in order:

1. **`make create-kind-cluster`** — creates a Kind cluster named `agentregistry` with a local container registry on `localhost:5001` and MetalLB for LoadBalancer support
2. **`make install-agentregistry`** — builds server images, pushes them to the local registry, and Helm installs AgentRegistry with a bundled database instance

You can also run any sub-target individually, e.g. `make install-agentregistry` to redeploy after a code change.

## Database Details

PostgreSQL is bundled in the Helm chart and deployed automatically. The default configuration is:

| Setting  | Value                            |
|----------|----------------------------------|
| Host     | `agentregistry-postgresql.agentregistry.svc.cluster.local` (in-cluster) |
| Port     | `5432`                           |
| Database | `agentregistry`                  |
| Username | `agentregistry`                  |
| Password | `agentregistry`                  |

Local setup uses `pgvector` for development of vector dependent capabilities.

### Connecting Directly

Port-forward to access PostgreSQL from your local machine:

```bash
kubectl --context kind-agentregistry port-forward -n agentregistry svc/agentregistry-postgresql 5432:5432
```

Then connect with psql:

```bash
psql -h localhost -U agentregistry -d agentregistry
```

### pgvector Extension

The `pgvector` extension is automatically available via the `pgvector/pgvector` image. The AgentRegistry server enables it on startup.

To verify manually:

```sql
SELECT * FROM pg_extension WHERE extname = 'vector';
```

### Data Persistence

- Data is stored in a `PersistentVolumeClaim` and survives pod restarts
- Data is **lost** when the Kind cluster is deleted (`make delete-kind-cluster`)

## Accessing AgentRegistry

Port-forward to access the API/UI:

```bash
kubectl --context kind-agentregistry port-forward -n agentregistry svc/agentregistry 12121:12121
```

Then open `http://localhost:12121` in your browser.

Alternatively, use the MetalLB LoadBalancer IP:

```bash
kubectl --context kind-agentregistry get svc -n agentregistry agentregistry
```

## Teardown

```bash
make delete-kind-cluster
```

This deletes the Kind cluster (and all data).

## Configuration

The setup script accepts environment variables to override defaults:

| Variable            | Default                           | Description                        |
|---------------------|-----------------------------------|------------------------------------|
| `KIND_CLUSTER_NAME` | `agentregistry`                   | Kind cluster name                  |
| `KIND_NAMESPACE`    | `agentregistry`                   | Kubernetes namespace               |
| `DOCKER_REGISTRY`   | `localhost:5001`                  | Local registry address             |
| `DOCKER_REPO`       | `agentregistry-dev/agentregistry` | Image repository prefix for local image builds |
| `VERSION`           | `git describe --tags --always`    | Image tag to deploy                |
| `JWT_KEY`           | Random 32-byte hex                | JWT private key for AgentRegistry  |

Example with custom values:

```bash
JWT_KEY=mysecretkey VERSION=v0.2.0 make setup-kind-cluster
```

## Troubleshooting

### PostgreSQL pod not ready

Check pod logs:

```bash
kubectl --context kind-agentregistry logs -n agentregistry -l app.kubernetes.io/component=database
```

### Images not found

Ensure Docker is running and the local registry is accessible:

```bash
curl http://localhost:5001/v2/_catalog
```

If the registry is empty, rebuild images:

```bash
make docker-server docker-agentgateway
```

### Helm install fails

Check AgentRegistry pod logs:

```bash
kubectl --context kind-agentregistry logs -n agentregistry -l app.kubernetes.io/name=agentregistry
```

### Cluster already exists

The setup script is idempotent — it skips cluster creation if the cluster already exists.

To start fresh:

```bash
make delete-kind-cluster && make setup-kind-cluster
```

## Scripts

| File               | Purpose                                  |
|--------------------|------------------------------------------|
| `setup-kind.sh`    | Creates Kind cluster with local registry |
| `setup-metallb.sh` | Installs and configures MetalLB          |
| `kind-config.yaml` | Kind cluster configuration               |
