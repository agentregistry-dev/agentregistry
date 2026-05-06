# Declarative CLI

Define agents, MCP servers, skills, and prompts as YAML files and manage them with `arctl apply`, `arctl get`, and `arctl delete`.

## Quick Start

```bash
arctl init agent summarizer --framework adk --language python --model-provider gemini --model-name gemini-2.5-flash
arctl build summarizer/ --push    # optional: build and push Docker image
arctl apply -f summarizer/agent.yaml
```

`arctl init agent NAME` and `arctl init mcp NAMESPACE/NAME` pick a framework + language interactively unless `--framework` and `--language` are provided. Run `arctl init agent NAME` (or `arctl init mcp NAMESPACE/NAME`) on its own to see the available choices.

## Resource Types

| Kind | get | delete |
|------|-----|--------|
| `Agent` | `arctl get agents` | `arctl delete agent NAME --version VERSION` |
| `MCPServer` | `arctl get mcps` | `arctl delete mcp NAME --version VERSION` |
| `Skill` | `arctl get skills` | `arctl delete skill NAME --version VERSION` |
| `Prompt` | `arctl get prompts` | `arctl delete prompt NAME --version VERSION` |

## Agents

```bash
arctl init agent summarizer --framework adk --language python --model-provider gemini --model-name gemini-2.5-flash
arctl build summarizer/ --push    # optional: build and push Docker image
arctl apply -f summarizer/agent.yaml
arctl get agent summarizer
arctl delete agent summarizer --version 0.1.0
```

Run locally with `arctl run` from inside the project directory (it reads `arctl.yaml` to pick the right plugin):

```bash
cd summarizer/
arctl run             # build + run via the framework plugin
arctl run --watch     # rebuild and restart on file change
arctl run --dry-run   # print the command without executing
```

## MCP Servers

```bash
arctl init mcp acme/my-server --framework fastmcp --language python
arctl build my-server/ --push    # optional: build and push Docker image
arctl apply -f my-server/mcp.yaml
arctl get mcps
arctl delete mcp acme/my-server --version 0.1.0
```

`arctl run` also works for MCP server projects — it dispatches to the plugin selected in `arctl.yaml`.

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

## Pulling Resources

Fetch a registered resource's source back to a local directory:

```bash
arctl pull agent summarizer
arctl pull mcp acme/my-server ./vendor/my-server
arctl pull skill summarize --version 1.2.0
```

## Tips

```bash
# Apply multiple resources from one file (separated by ---)
# Resources are applied in document order — define dependencies before the agent
arctl apply -f full-stack.yaml

# List all resource types at once
arctl get all
```

See [`examples/`](../examples/) for ready-to-use YAML files, including [`full-stack.yaml`](../examples/full-stack.yaml) which defines an agent and all its dependencies in a single file.
