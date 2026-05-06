# Declarative CLI

Define agents, MCP servers, skills, and prompts as YAML files and manage them with `arctl apply`, `arctl get`, and `arctl delete`.

## Quick Start

```bash
arctl init agent adk python summarizer --model-provider gemini --model-name gemini-2.5-flash
arctl build summarizer/ --push      # optional: build and push Docker image
arctl apply -f summarizer/agent.yaml
```

## Versioning

Versions are system-assigned monotonic integers. First `apply` of a new name produces `1`; re-applying the same spec is a no-op; a changed spec produces `2`, `3`, …. Older versions are immutable. Deleting every version frees the name.

```bash
arctl get agent NAME                   # latest
arctl get agent NAME --version 1       # specific
arctl get agent NAME --all-versions    # history

arctl delete agent NAME                # latest
arctl delete agent NAME --version 1    # specific
arctl delete agent NAME --all-versions # frees the name
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
