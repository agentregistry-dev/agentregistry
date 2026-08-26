# Claude Code Plugin Marketplace Compatibility (read-only)

Claude Code installs plugins from a `marketplace.json` document — a bare URL to one is enough (`claude plugin marketplace add <url>`). This compatibility layer re-exposes AgentRegistry's `Plugin` resources, already resolved to a concrete git commit pin by the Plugin controller, in that shape. It is **read-only** (no publish/write path) and **additive** — the native `v1alpha1` API is unchanged and remains the source of truth.

Only the URL/git source forms are covered (phase 1). Codex and Cursor require a real git-cloneable marketplace source rather than a bare URL, so they are out of scope for this endpoint; see `docs/design/plugins-harness-phase1-roadmap.md`.

## Endpoint

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/plugin-marketplace/marketplace.json` | The full flattened catalogue of resolved plugins. |

```json
{
  "$schema": "https://json.schemastore.org/claude-code-marketplace.json",
  "name": "agentregistry",
  "owner": { "name": "agentregistry" },
  "plugins": [
    { "name": "code-formatter", "source": { "source": "url", "url": "https://github.com/acme/code-formatter", "sha": "…" }, "description": "…", "version": "1.2.0" }
  ]
}
```

Plugins that aren't `Ready` with a resolved source pin, or that resolved to an OCI source (no representation in this schema), are silently skipped — the document never contains a partial or broken entry.

## Pointing a client at it

```
claude plugin marketplace add https://registry.example.com/plugin-marketplace/marketplace.json
```

(or `.../<prefix>/plugin-marketplace/marketplace.json` when `AGENT_REGISTRY_PLUGIN_MARKETPLACE_COMPAT_PATH_PREFIX` is set).

## Configuration

| Env var | Default | Meaning |
| --- | --- | --- |
| `AGENT_REGISTRY_PLUGIN_MARKETPLACE_COMPAT_ENABLED` | `false` | Toggle the compatibility API. Off by default — opt-in (see Caveats). |
| `AGENT_REGISTRY_PLUGIN_MARKETPLACE_COMPAT_PATH_PREFIX` | `""` | Optional base prefix to mount under (e.g. `/plugins`). Empty serves the standard path at the root. |

## Caveats

- **Off by default; RBAC-aware via the same hook as the native read path.** The endpoint reuses the per-kind `ListFilter` that the native Plugin read path uses. In the **OSS** build this hook is not wired, so the catalogue is flat and unfiltered across all namespaces. A **downstream** build that wires `crud.PerKindHooks` for Plugin gets the same RBAC/tenancy scoping automatically.
- **Uses configured authentication.** OSS does not configure an authn provider, so its catalogue remains permissive. A downstream build that configures one protects this route with the normal authn middleware; its authenticated session reaches the `ListFilter` for RBAC or tenancy scoping.
- **Best-effort field mapping.** Description/version prefer the scanned `plugin.json` manifest, falling back to the Plugin spec's description and an empty version (Claude Code falls back to the resolved commit SHA).
