# Declarative CLI

Define agents, MCP servers, skills, and prompts as YAML files and manage them with `arctl apply`, `arctl get`, and `arctl delete`.

## Quick Start

```bash
arctl init agent summarizer --framework adk --language python --model-provider gemini --model-name gemini-2.5-flash
arctl build summarizer/ --push    # optional: build and push Docker image
arctl apply -f summarizer/agent.yaml
```

`arctl init agent NAME` and `arctl init mcp NAMESPACE/NAME` pick a framework + language interactively unless `--framework` and `--language` are provided. Run `arctl init agent NAME` (or `arctl init mcp NAMESPACE/NAME`) on its own to see the available choices.

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

## MCP Servers

```bash
arctl init mcp acme/my-server --framework fastmcp --language python
arctl build my-server/ --push    # optional: build and push Docker image
arctl apply -f my-server/mcp.yaml
arctl get mcps
arctl delete mcp acme/my-server --tag stable
```

`arctl run` also works for MCP server projects — it dispatches to the framework selected in `arctl.yaml`.

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
arctl pull mcp acme/my-server ./vendor/my-server
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
