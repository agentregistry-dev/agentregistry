# AGENTS.md - Development Guidelines

This document provides guidelines for both AI coding assistants (Claude, Copilot, Cursor, etc.) and human developers working on the AgentRegistry codebase.

## Project Overview

AgentRegistry is a centralized registry for securely curating, discovering, deploying, and managing agentic infrastructure including MCP (Model Context Protocol) servers, agents, and skills.

**Tech Stack:**
- **Backend/CLI:** Go 1.25+
- **Database:** PostgreSQL with pgvector (accessed via pgx)
- **Frontend:** Next.js 14 (App Router) with Tailwind CSS
- **CLI Framework:** Cobra
- **API Framework:** Huma (OpenAPI)

---

## Architecture

### Directory Structure

```
agentregistry/
├── cmd/           # Entry points only - minimal code
│   ├── cli/       # CLI entry point
│   └── server/    # Server entry point
├── pkg/           # Public, reusable packages
├── internal/      # Private implementation
│   ├── registry/  # Core registry implementation
│   │   ├── api/       # HTTP handlers
│   │   ├── database/  # Database layer (pgx)
│   │   ├── service/   # Business logic
│   │   └── ...
│   ├── cli/       # CLI command implementations
│   ├── mcp/       # MCP protocol handling
│   └── daemon/    # Daemon orchestration
├── ui/            # Next.js frontend
└── docker/        # Container configurations
```

### Layer Responsibilities

1. **cmd/** - Entry points only. Delegate immediately to pkg/ or internal/
2. **pkg/** - Public APIs for external consumption and reusability
3. **internal/** - Implementation details, not importable by external packages
4. **internal/registry/database/** - **ONLY** place that accesses the database directly
5. **internal/registry/service/** - Business logic, receives database interface via constructor

---

## Critical Rules

### Database Access

**The database MUST only be accessed by:**
1. The database layer (`internal/registry/database/`)
2. The authorizer component

**No other component should have direct database access.** All data operations must go through the appropriate interfaces.

```go
// CORRECT: Service receives database interface
type RegistryService struct {
    db DatabaseInterface
}

func NewRegistryService(db DatabaseInterface) *RegistryService {
    return &RegistryService{db: db}
}

// INCORRECT: Service creates its own database connection
type RegistryService struct {
    db *pgxpool.Pool  // Direct database access - DO NOT DO THIS
}
```

### Interface Design

Every significant component must define an interface for its dependencies. This enables:
- Unit testing with mocks
- Loose coupling between packages
- Clear contract definitions

```go
// Define interfaces for dependencies
type AgentRepository interface {
    GetAgent(ctx context.Context, id string) (*Agent, error)
    ListAgents(ctx context.Context, opts ListOptions) ([]Agent, error)
    CreateAgent(ctx context.Context, agent *Agent) error
}

// Implementation receives interface, not concrete type
type AgentService struct {
    repo AgentRepository
}
```

### Single Responsibility

Each package and file should have one clear purpose. Signs of mixed responsibilities:
- Files over 500 lines
- Packages importing many unrelated dependencies
- Functions doing multiple unrelated things

**Split large components into focused units.**

---

## Error Handling

Use standard Go error patterns with wrapping:

```go
import (
    "errors"
    "fmt"
)

// Wrap errors with context
func (s *Service) GetAgent(ctx context.Context, id string) (*Agent, error) {
    agent, err := s.repo.GetAgent(ctx, id)
    if err != nil {
        return nil, fmt.Errorf("getting agent %s: %w", id, err)
    }
    return agent, nil
}

// Check error types
if errors.Is(err, ErrNotFound) {
    // handle not found
}

// Define sentinel errors for domain-specific cases
var (
    ErrNotFound     = errors.New("not found")
    ErrUnauthorized = errors.New("unauthorized")
)
```

**Consistency requirements:**
- Always wrap errors with `fmt.Errorf("context: %w", err)`
- Use lowercase error messages (they may be wrapped)
- Define sentinel errors for cases that callers need to check

---

## Logging

Use Go's standard library structured logging:

```go
import "log/slog"

// Use structured logging with context
slog.Info("agent created",
    "agent_id", agent.ID,
    "name", agent.Name,
)

slog.Error("failed to create agent",
    "error", err,
    "agent_name", name,
)
```

---

## Testing

### Requirements

The codebase needs significantly more test coverage. When adding or modifying code:

1. **Write unit tests with mocks** for business logic
2. **Write table-driven tests** for functions with multiple cases
3. **Write integration tests** for database and API operations

### Table-Driven Tests

```go
func TestValidateAgentName(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"valid name", "my-agent", false},
        {"empty name", "", true},
        {"too long", strings.Repeat("a", 256), true},
        {"special chars", "my@agent", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateAgentName(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("ValidateAgentName(%q) error = %v, wantErr %v",
                    tt.input, err, tt.wantErr)
            }
        })
    }
}
```

### Mock-Based Unit Tests

```go
type mockAgentRepo struct {
    agents map[string]*Agent
}

func (m *mockAgentRepo) GetAgent(ctx context.Context, id string) (*Agent, error) {
    if agent, ok := m.agents[id]; ok {
        return agent, nil
    }
    return nil, ErrNotFound
}

func TestAgentService_GetAgent(t *testing.T) {
    repo := &mockAgentRepo{
        agents: map[string]*Agent{
            "agent-1": {ID: "agent-1", Name: "Test Agent"},
        },
    }
    svc := NewAgentService(repo)

    agent, err := svc.GetAgent(context.Background(), "agent-1")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if agent.Name != "Test Agent" {
        t.Errorf("got name %q, want %q", agent.Name, "Test Agent")
    }
}
```

### Running Tests

```bash
# Run unit tests
make test

# Run with coverage
make test-coverage

# Run with HTML coverage report
make test-coverage-report

# Run integration tests
make test-integration
```

---

## Dependency Injection

Use manual constructor injection. No DI frameworks.

```go
// Define what you need
type AgentService struct {
    repo   AgentRepository
    logger *slog.Logger
}

// Accept dependencies via constructor
func NewAgentService(repo AgentRepository, logger *slog.Logger) *AgentService {
    return &AgentService{
        repo:   repo,
        logger: logger,
    }
}

// Wire up in main or initialization code
func main() {
    db := database.NewPostgresDB(connString)
    logger := slog.Default()

    agentRepo := database.NewAgentRepository(db)
    agentSvc := service.NewAgentService(agentRepo, logger)

    // ...
}
```

---

## CLI Development

Use Cobra for CLI commands. Follow existing patterns in `internal/cli/`.

```go
var agentCmd = &cobra.Command{
    Use:   "agent",
    Short: "Manage agents",
}

var agentListCmd = &cobra.Command{
    Use:   "list",
    Short: "List all agents",
    RunE: func(cmd *cobra.Command, args []string) error {
        // Implementation
        return nil
    },
}

func init() {
    agentCmd.AddCommand(agentListCmd)
}
```

---

## API Development

Use Huma for REST APIs. Huma generates OpenAPI documentation automatically.

```go
// Define input/output types
type GetAgentInput struct {
    ID string `path:"id"`
}

type AgentOutput struct {
    Body Agent
}

// Register routes
huma.Get(api, "/agents/{id}", func(ctx context.Context, input *GetAgentInput) (*AgentOutput, error) {
    agent, err := svc.GetAgent(ctx, input.ID)
    if err != nil {
        return nil, err
    }
    return &AgentOutput{Body: *agent}, nil
})
```

---

## Frontend Development

Use Next.js App Router patterns with React Server Components where appropriate.

```tsx
// app/agents/page.tsx
export default async function AgentsPage() {
    const agents = await fetchAgents();
    return (
        <div>
            {agents.map(agent => (
                <AgentCard key={agent.id} agent={agent} />
            ))}
        </div>
    );
}
```

Use shadcn/ui components. Check `ui/components/ui/` for available components.

---

## Git Workflow

### Commit Messages

Use conventional commits:

```
type(scope): subject

body (optional)

footer (optional)
```

**Types:** `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`

**Examples:**
```
feat(agents): add agent deployment status tracking
fix(mcp): handle connection timeout gracefully
test(registry): add unit tests for validation logic
refactor(database): extract interface for agent repository
```

### Branch Naming

```
feature/agent-deployment
fix/mcp-connection-timeout
refactor/database-interfaces
```

---

## AI Assistant Guidelines

When working with this codebase, AI assistants should:

### Do

1. **Read before writing** - Always read existing code before suggesting modifications
2. **Follow existing patterns** - Match the style of surrounding code
3. **Add tests** - Include tests for any new functionality
4. **Use interfaces** - Define interfaces for new dependencies
5. **Keep changes minimal** - Only modify what's necessary for the task
6. **Check database access** - Ensure database operations go through proper layers

### Don't

1. **Access database directly** - Never add direct database access outside the database layer
2. **Create god objects** - Keep components focused and small
3. **Skip error handling** - Always handle and wrap errors appropriately
4. **Add unnecessary abstractions** - Don't over-engineer solutions
5. **Ignore existing interfaces** - Use defined interfaces, don't bypass them
6. **Create new files unnecessarily** - Prefer editing existing files

### When Adding New Features

1. Check if similar functionality exists
2. Identify the appropriate layer (api/service/database)
3. Define interfaces for new dependencies
4. Implement with proper error handling
5. Add unit tests with mocks
6. Add integration tests if touching database/API
7. Update any affected documentation

### When Fixing Bugs

1. Write a failing test that reproduces the bug
2. Fix the bug with minimal changes
3. Verify the test passes
4. Check for similar issues elsewhere
5. Don't refactor unrelated code

---

## Code Review Checklist

- [ ] Database access only through database layer or authorizer
- [ ] New dependencies injected via constructor
- [ ] Interfaces defined for mockability
- [ ] Errors wrapped with context
- [ ] Unit tests with mocks included
- [ ] Table-driven tests for multiple cases
- [ ] No mixed responsibilities in components
- [ ] Conventional commit message format
- [ ] No hardcoded values that should be configurable

---

## Quick Reference

| Task | Command |
|------|---------|
| Build CLI | `make build-cli` |
| Build Server | `make build-server` |
| Run Unit Tests | `make test-unit` |
| Run all Tests | `make test` |
| Run Tests w/ Coverage | `make test-coverage` |
| Coverage HTML Report | `make test-coverage-report` |
| Run Linter | `make lint` |
| Format Code | `make fmt` |
| Build UI | `make build-ui` |
| Dev UI | `make dev-ui` |
| Docker Up | `make docker-compose-up` |
| Docker Down | `make docker-compose-down` |

---

## Related Documentation

- [README.md](./README.md) - Project overview and quick start
- [DEVELOPMENT.md](./DEVELOPMENT.md) - Architecture details
- [CONTRIBUTING.md](./CONTRIBUTING.md) - Contribution guidelines
