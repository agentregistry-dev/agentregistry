# Declarative CLI

Define agents, MCP servers, skills, and prompts as YAML files and manage them with `arctl apply`, `arctl get`, and `arctl delete`.

## Quick Start

```bash
arctl init agent summarizer --framework adk --language python --model-provider gemini --model-name gemini-2.5-flash
arctl build summarizer/ --push    # optional: build and push Docker image
arctl apply -f summarizer/agent.yaml
```

`arctl init agent NAME` and `arctl init mcp NAME` pick a framework + language interactively unless `--framework` and `--language` are provided. Run `arctl init agent NAME` (or `arctl init mcp NAME`) on its own to see the available choices. All resource `metadata.name` values (Agent, Skill, Prompt, Deployment, MCPServer) must be DNS-1123 subdomain: lowercase alphanumeric, hyphens, and dots; max 253 chars; each dot-separated segment must start and end with alphanumeric (max 63 chars per segment). Examples: `my-server`, `io.example.mcp`. Agent names additionally cannot contain hyphens or dots and cannot collide with Python keywords (`class`, `import`, `return`, …) because they become Python identifiers in generated code.

## Tags And Mutable Objects

Agents, MCP servers, remote MCP servers, skills, and prompts are taggable artifacts. Set `metadata.tag` to publish a deterministic name you can reference from other manifests; if you omit it, the registry uses the literal `latest` tag.

Providers and deployments are mutable control-plane objects. They use public namespace/name identity, not tags or versions.

```bash
arctl init agent summarizer --framework adk --language python --model-provider gemini --model-name gemini-2.5-flash
arctl build summarizer/ --push    # optional: build and push Docker image
arctl apply -f summarizer/agent.yaml

arctl get agent summarizer               # latest
arctl get agent summarizer --tag stable  # exact tag
arctl get agent summarizer --all-tags    # tag list

arctl delete agent summarizer                # latest
arctl delete agent summarizer --tag stable   # exact tag
arctl delete agent summarizer --all-tags     # delete every tag
```

Run locally with `arctl run` from inside the project directory (it reads `arctl.yaml` to pick the right framework):

```bash
cd summarizer/
arctl run             # build + run via the framework
arctl run --watch     # rebuild and restart on file change
arctl run --dry-run   # print the command without executing
```

`arctl run` rejects an `mcp.yaml` with `spec.remote` and no `spec.source` — nothing to build locally. To inspect a remote MCP's tools:

```bash
npx -y @modelcontextprotocol/inspector --server-url <url>
```

### Wiring MCP dependencies into a new agent

`arctl init agent` takes two repeatable flags:

- `--mcp <ref>` — adds the MCPServer to `agent.yaml.spec.mcpServers[]`. Accepts `name` or `name@tag` (defaults to `latest`). For remote catalog entries (`spec.remote` set), also appends an `MCP_SERVERS_CONFIG` entry to `.env`. Source-mode entries skip the `.env` write.
- `--local-mcp <path>` — wires `.env` against a sibling `arctl init mcp` project at `http://host.docker.internal:<port>/mcp` (port read from its `arctl.yaml`).

Repeatable; combined into one `MCP_SERVERS_CONFIG` line.

## MCP Servers

```bash
arctl init mcp my-server --framework fastmcp --language python
arctl build my-server/ --push    # optional: build and push Docker image
arctl apply -f my-server/mcp.yaml
arctl get mcps
arctl delete mcp my-server --tag stable
```

`arctl run` also works for MCP server projects — it dispatches to the framework selected in `arctl.yaml`.

### Registering public-catalogue MCP packages

Public MCP packages on npm / PyPI / OCI / NuGet declare their ownership using the upstream MCP-ecosystem `namespace/name` convention (e.g. `io.github.modelcontextprotocol/server-fetch`). The registry's ownership validator compares this against `metadata.name` by default.
When that shape doesn't fit the `metadata.name` requirements (DNS-1123 subdomain — slashes not allowed), set `spec.source.package.serverName` to the upstream identity and the validator will use it as the comparison target instead:

```yaml
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: mcp-fetch
spec:
  source:
    package:
      # ...
      serverName: io.github.modelcontextprotocol/server-fetch  # upstream catalogue identity
```

## Skills & Prompts

```bash
arctl init skill summarize
arctl apply -f summarize/skill.yaml
arctl get skills
arctl delete skill summarize --tag stable

arctl init prompt summarizer-system-prompt
arctl apply -f summarizer-system-prompt.yaml
arctl get prompts
arctl delete prompt summarizer-system-prompt --tag stable
```

## Pulling Resources

Fetch a registered resource's source back to a local directory:

```bash
arctl pull agent summarizer
arctl pull mcp my-server ./vendor/my-server
arctl pull skill summarize --version 1.2.0
```

## Tips

```bash
# Multi-resource file (separated by ---). Apply order = document order, so
# define dependencies (MCP servers, skills, prompts) before the agent.
arctl apply -f full-stack.yaml

# List all resource types at once
arctl get all
```

See [`examples/`](../examples/) for ready-to-use YAML, including [`full-stack.yaml`](../examples/full-stack.yaml) — an agent and all its dependencies in one file.
