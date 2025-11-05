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

