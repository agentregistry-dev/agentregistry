# Declarative CLI

Define agents, MCP servers, skills, and prompts as YAML files and manage them with `arctl apply`, `arctl get`, and `arctl delete`.

## Quick Start

```bash
arctl init agent adk python summarizer --model-provider gemini --model-name gemini-2.5-flash
arctl build summarizer/ --push      # optional: build and push Docker image
arctl apply -f summarizer/agent.yaml
```

## Tags And Mutable Objects

Agents, MCP servers, remote MCP servers, skills, and prompts are taggable artifacts. Set `metadata.tag` to publish a deterministic name you can reference from other manifests; if you omit it, the registry uses the literal `latest` tag.

Providers and deployments are mutable control-plane objects. They use public namespace/name identity, not tags or versions.

```bash
arctl get agent NAME                   # latest
arctl get agent NAME --version stable  # deprecated alias for tag selection
arctl get agent NAME --all-tags        # tag list

arctl delete agent NAME                # latest
arctl delete agent NAME --version stable # deprecated alias for tag selection
arctl delete agent NAME --all-versions   # deprecated alias for deleting every tag

arctl get provider NAME
arctl delete provider NAME
arctl get deployment NAME
arctl delete deployment NAME --force
```

## Resources

The same shape works for every kind — substitute the alias: `agent`, `mcp`, `skill`, `prompt`.

```bash
arctl init <kind> <name> [flags]   # scaffold YAML
arctl build <dir> --push           # optional: build + push image (agent, mcp)
arctl apply -f <file>.yaml         # publish
arctl get <kind> <name>            # read
arctl delete <kind> <name>         # remove
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
