# Declarative CLI

Define agents, MCP servers, skills, and prompts as YAML files and manage them with `arctl apply`, `arctl get`, and `arctl delete`.

## Quick Start

```bash
arctl init agent adk python summarizer --model-provider gemini --model-name gemini-2.0-flash
arctl build summarizer/ --push    # optional: build and push Docker image
arctl apply -f summarizer/agent.yaml
```

## Resource Types

| Kind | get | delete |
|------|-----|--------|
| `Agent` | `arctl get agents` | `arctl delete agent NAME --version VERSION` |
| `MCPServer` | `arctl get mcps` | `arctl delete mcp NAME --version VERSION` |
| `Skill` | `arctl get skills` | `arctl delete skill NAME --version VERSION` |
| `Prompt` | `arctl get prompts` | `arctl delete prompt NAME --version VERSION` |

## Agents

```bash
arctl init agent adk python summarizer --model-provider gemini --model-name gemini-2.0-flash
arctl build summarizer/ --push    # optional: build and push Docker image
arctl apply -f summarizer/agent.yaml
arctl get agent summarizer
arctl delete agent summarizer --version 0.1.0
```

## MCP Servers

```bash
arctl init mcp fastmcp-python acme/my-server
arctl build my-server/ --push    # optional: build and push Docker image
arctl apply -f my-server/mcp.yaml
arctl get mcps
arctl delete mcp acme/my-server --version 0.1.0
```

## Skills & Prompts

```bash
arctl init skill summarize --category nlp
arctl apply -f summarize/skill.yaml
arctl get skills
arctl delete skill summarize --version 0.1.0

arctl init prompt summarizer-system-prompt
arctl apply -f summarizer-system-prompt.yaml
arctl get prompts
arctl delete prompt summarizer-system-prompt --version 0.1.0
```

## Tips

```bash
# Apply multiple resources from one file (separated by ---)
# Resources are applied in document order — define dependencies before the agent
arctl apply -f full-stack.yaml

# List all resource types at once
arctl get all
```

See [`examples/declarative/`](../examples/declarative/) for ready-to-use YAML files, including [`full-stack.yaml`](../examples/declarative/full-stack.yaml) which defines an agent and all its dependencies in a single file.
