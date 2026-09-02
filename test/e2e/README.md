# End-to-end tests

AgentRegistry has two E2E entry points:

| Target | Environment | Coverage |
| --- | --- | --- |
| `make test-cli-e2e` | Local subprocess; PostgreSQL tests run when `localhost:5432` is available | `arctl db migrate` commands, arguments, status, and migration behavior |
| `make test-e2e` | Local Kind cluster | Registry, CLI workflow, and runtime integration |

`make test-e2e` creates or updates Kind, builds `arctl`, and runs every Kubernetes E2E package. The same command is the CI entry point.

## Kubernetes suites

Use `E2E_SUITE` to select which Go tests run:

| Suite | Coverage |
| --- | --- |
| `all` (default) | Every Kubernetes E2E scenario |
| `registry` | Declarative resource lifecycle, batch and tagging behavior, CLI init/build/run/pull workflows, remote MCP references, and private Git sources |
| `kagent` | Agent and MCPServer deployment lifecycle, Agent-to-MCP wiring, and discovery lifecycle |

```shell
make test-e2e E2E_SUITE=kagent
make test-e2e E2E_SUITE=kagent E2E_RUN=TestKagentDiscovery
```

`E2E_RUN` selects one top-level Go test by its exact name.

Reuse an existing prepared environment without installing or restarting components:

```shell
make test-e2e E2E_SETUP=false E2E_SUITE=kagent
```

Generate scenario documentation from suites that record it:

```shell
make generate-e2e-docs E2E_SUITE=kagent
```
