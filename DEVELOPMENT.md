# Development Guide

## Local Kubernetes Environment

The fastest way to run the full stack locally is with [Kind](https://kind.sigs.k8s.io/). A single `make` target creates the cluster, deploys PostgreSQL/pgvector, builds the server image, and installs AgentRegistry via Helm.

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm](https://helm.sh/docs/intro/install/)

> `kind` is installed automatically into `./bin/kind` by `make install-tools` — no manual installation needed.

### Full setup

```bash
make setup-kind-cluster
```

This runs three steps in order:

| Step | Target | What it does |
|------|--------|-------------|
| 1 | `create-kind-cluster` | Installs `kind` to `./bin/`, creates Kind cluster + local registry (`localhost:5001`) + MetalLB |
| 2 | `install-postgresql` | Deploys `pgvector/pgvector:0.8.2-pg16` into the `agentregistry` namespace |
| 3 | `install-agentregistry` | Builds server image, pushes to local registry, Helm installs AgentRegistry |

Each target can also be run independently — useful when iterating on code:

```bash
# Rebuild and redeploy after a code change (cluster and PG stay up)
make install-agentregistry

# Skip image builds if the images are already up to date
make install-agentregistry BUILD=false
```

On subsequent runs, `install-agentregistry` reuses the `jwtPrivateKey` already stored in the cluster secret so tokens remain valid across redeploys.

### Accessing the services

```bash
# AgentRegistry API/UI
kubectl --context kind-agentregistry port-forward -n agentregistry svc/agentregistry 12121:12121
# open http://localhost:12121

# PostgreSQL (for direct inspection)
kubectl --context kind-agentregistry port-forward -n agentregistry svc/postgres-pgvector 5432:5432
psql -h localhost -U agentregistry -d agent-registry
```

### Teardown

```bash
make delete-kind-cluster
```

See [`scripts/kind/README.md`](scripts/kind/README.md) for more detail on configuration, troubleshooting, and overriding defaults.

---

## Local Docker Compose Environment

```bash
make run   # starts registry server + daemon via docker-compose
make down  # stops everything
```

The UI is available at `http://localhost:12121`.

---

# Architecture Overview

### 1. CLI Layer (cmd/)

Built with [Cobra](https://github.com/spf13/cobra), provides all command-line functionality:

- **Registry Management**: connect, disconnect, refresh
- **Resource Discovery**: list, search, show
- **Installation**: install, uninstall
- **Configuration**: configure clients
- **UI**: launch web interface

Each command has placeholder implementations ready to be filled with actual logic.

### 2. Data Layer (internal/database/)

Uses **SQLite** for local storage:

**Tables:**
- `registries` - Connected registries
- `servers` - MCP servers from registries
- `skills` - Skills from registries
- `installations` - Installed resources

**Location:** `~/.arctl/arctl.db`

The schema is based on the MCP Registry JSON schema provided, supporting the full `ServerDetail` structure.

### 3. API Layer (internal/api/)

Built with [Gin](https://github.com/gin-gonic/gin), provides REST API:

**Endpoints:**
- `GET /api/health` - Health check
- `GET /api/registries` - List registries
- `GET /api/servers` - List MCP servers
- `GET /api/skills` - List skills
- `GET /api/installations` - List installed resources
- `GET /*` - Serve embedded UI

**Port:** 8080 (configurable with `--port`)

### 4. UI Layer (ui/)

Built with:
- **Framework:** Next.js 14 (App Router)
- **Language:** TypeScript
- **Styling:** Tailwind CSS
- **Components:** shadcn/ui
- **Icons:** Lucide React

**Features:**
- Dashboard with statistics
- Resource browser (registries, MCP servers, skills)
- Real-time data from API
- Responsive design
- Installation status indicators

**Build Output:** Static files exported to `internal/registry/api/ui/dist/`

## Data Flow

### CLI Command Execution

```
User Input
    ↓
Cobra Command (cmd/)
    ↓
Business Logic (TODO)
    ↓
Database Layer (internal/database/)
    ↓
SQLite (~/.arctl/arctl.db)
```

### Web UI Request

```
Browser Request
    ↓
Gin Router (internal/api/)
    ↓
API Handler
    ↓
Database Query
    ↓
JSON Response
    ↓
React Component (ui/)
    ↓
User Interface
```

## Embedding Strategy

### How It Works

1. **Build Phase** (`make build-ui`):
   - Next.js builds static files
   - Output goes to `internal/registry/api/ui/dist/`

2. **Compile Phase** (`make build-cli`):
   - Go's `embed` directive includes entire `ui/dist/` directory
   - Files become part of the binary

3. **Runtime Phase** (`./bin/arctl ui`):
   - Gin serves files from embedded FS
   - No external dependencies needed

### Embed Directive

```go
//go:embed ui/dist/*
var embeddedUI embed.FS
```

This embeds all files in `internal/registry/api/ui/dist/` at compile time.

## Build Process

### Development

```bash
# UI only (hot reload)
make dev-ui

# CLI only (quick iteration)
go build -o bin/arctl main.go
```

### Production

```bash
# Full build with embedding
make build

# Creates: ./bin/arctl (single binary with UI embedded)
```

## Extension Points

### Adding a New CLI Command

1. Create `cmd/mycommand.go`
2. Define the command with Cobra
3. Add to `rootCmd` in `init()`
4. Implement logic (call database layer)

### Adding a New API Endpoint

1. Add handler in `internal/api/server.go`
2. Register route in `StartServer()`
3. Call database layer
4. Return JSON response

### Adding a New UI Page

1. Create `ui/app/mypage/page.tsx`
2. Fetch data from `/api/*` endpoints
3. Use shadcn components for UI
4. Rebuild with `make build-ui`

### Adding Database Tables

1. Update schema in `internal/database/database.go`
2. Add model in `internal/models/models.go`
3. Add query methods in database package
4. Database auto-migrates on first run

## Security Considerations

### Database

- Stored in user's home directory (`~/.arctl/`)
- No network access
- File permissions: 0755 (directory), default (file)

### API Server

- Localhost only by default
- CORS not configured (local use)
- No authentication (local tool)

### Embedded UI

- Static files only
- No server-side execution
- Served from memory (embedded)

## Contributing

When adding features:

1. Add placeholder implementations first
2. Create tests (TODO)
3. Update documentation
4. Rebuild with `make build`
5. Test the binary

## Resources

- [Cobra Documentation](https://cobra.dev/)
- [Gin Documentation](https://gin-gonic.com/)
- [Next.js Documentation](https://nextjs.org/docs)
- [shadcn/ui Components](https://ui.shadcn.com/)
- [MCP Protocol Specification](https://spec.modelcontextprotocol.io/)

