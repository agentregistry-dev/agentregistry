# Contributing to arctl

Thank you for your interest in contributing to arctl! This document provides guidelines and instructions for contributing.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/agentregistry.git`
3. Add upstream remote: `git remote add upstream https://github.com/agentregistry-dev/agentregistry.git`
4. Create a branch: `git checkout -b feature/my-feature`

## Development Setup

### Prerequisites

- Go 1.22 or later
- Node.js 18 or later
- npm or yarn
- make (optional but recommended)

### Initial Setup

```bash
# Run the setup script
./setup.sh

# Or manually:
go mod download
cd ui && npm install && cd ..
make build
```

## Development Workflow

### Working on the CLI

```bash
# Make changes to cmd/*.go or internal/**/*.go

# Build quickly (without UI rebuild)
go build -o bin/arctl main.go

# Test your changes
./bin/arctl <command>

# Run tests
go test ./...
```

### Working on the UI

```bash
# Start development server with hot reload
make dev-ui
# Opens at http://localhost:3000

# Make changes to ui/app/**/*.tsx

# When ready to test with CLI:
make build-ui
make build-cli
./bin/arctl ui
```

### Working on Both

```bash
# Terminal 1: UI dev server
make dev-ui

# Terminal 2: CLI development
go build -o bin/arctl main.go
./bin/arctl <command>

# When ready for integration test:
make build
./bin/arctl ui
```

## Code Style

### Go

- Follow standard Go conventions
- Use `gofmt` for formatting
- Use `golangci-lint` for linting
- Write meaningful comments for exported functions
- Keep functions small and focused

```bash
# Format code
gofmt -w .

# Run linter
golangci-lint run
```

### TypeScript/React

- Follow Next.js and React best practices
- Use TypeScript for type safety
- Use functional components with hooks
- Keep components small and reusable

```bash
# Lint UI code
cd ui
npm run lint
```

## Testing

### Go Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific test
go test -run TestFunctionName ./...
```

### UI Tests

```bash
cd ui
npm test
```

## Adding New Features

### New CLI Command

1. Create `cmd/mycommand.go`:

```go
package cmd

import (
    "fmt"
    "github.com/spf13/cobra"
)

var myCmd = &cobra.Command{
    Use:   "my-command",
    Short: "Description",
    Run: func(cmd *cobra.Command, args []string) {
        fmt.Println("Implementation")
    },
}

func init() {
    rootCmd.AddCommand(myCmd)
}
```

2. Add tests
3. Update documentation
4. Build and test

### New API Endpoint

1. Add handler in `internal/api/server.go`:

```go
func getMyData(c *gin.Context) {
    data, err := database.GetMyData()
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, data)
}
```

2. Register route:

```go
api.GET("/my-data", getMyData)
```

3. Add database methods
4. Add tests

### New UI Component

1. Create component in `ui/components/`:

```tsx
import { Card } from "@/components/ui/card"

export function MyComponent() {
  return <Card>Content</Card>
}
```

2. Use in page:

```tsx
import { MyComponent } from "@/components/MyComponent"

export default function Page() {
  return <MyComponent />
}
```

3. Build and test

## Database Changes

When adding/modifying database schema:

1. Update schema in `internal/database/database.go`
2. Add/update models in `internal/models/models.go`
3. Add migration logic if needed
4. Update query methods
5. Test with fresh database

## Documentation

Update documentation when adding features:

- `README.md` - Overview and commands
- Inline code comments

## Commit Messages

Follow conventional commits:

```
type(scope): subject

body

footer
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation
- `style`: Formatting
- `refactor`: Code restructuring
- `test`: Tests
- `chore`: Maintenance

Examples:
```
feat(cli): add search command
fix(api): handle empty registry list
docs: update quickstart guide
refactor(db): simplify query methods
```

## Pull Request Process

1. Update documentation
2. Add/update tests
3. Ensure CI passes
4. Update CHANGELOG.md
5. Request review

### PR Checklist

- [ ] Code follows style guidelines
- [ ] Tests added/updated
- [ ] Documentation updated
- [ ] CI passes
- [ ] Commits are clean and meaningful

## Building for Release

```bash
# Full clean build
make all

# Test the binary
./bin/arctl version
./bin/arctl ui

# Create release
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

## Common Issues

### Go Module Issues

```bash
go mod tidy
go mod download
```

### UI Build Failures

```bash
cd ui
rm -rf node_modules package-lock.json .next
npm install
npm run build
```

### Embed Issues

Ensure UI is built before Go build:

```bash
make build-ui
make build-cli
```

## Getting Help

- Open an issue for bugs
- Start a discussion for questions
- Join our Discord (if available)

## Code of Conduct

- Be respectful and inclusive
- Welcome newcomers
- Give constructive feedback
- Focus on what's best for the project


Thank you for contributing! 🎉

