# MCP Registry v0.1 Compatibility (read-only)

AgentRegistry began as a fork of the [official MCP Registry](https://github.com/modelcontextprotocol/registry) but reorganized its public API around Kubernetes-style `v1alpha1` resources (`MCPServer` at `/v0/mcpservers`). That moved it off the upstream `server.json` contract, so registry-aware clients — notably VS Code's MCP gallery — can no longer consume it as an MCP registry.

This compatibility layer re-exposes the MCPServer resources already in AgentRegistry through the **frozen v0.1** MCP Registry read API, in the official `server.json` shape. It is **read-only** (no publish/write path) and **additive** — the native `v1alpha1` API is unchanged and remains the source of truth.

## Endpoints

Served at the standard spec paths (optionally under a configured prefix — see below):

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/v0.1/servers` | List servers. Cursor-paginated. Query: `cursor`, `limit` (≤100), `search`, `updated_since` (RFC3339), `version`, `include_deleted`. |
| `GET` | `/v0.1/servers/{serverName}/versions` | List all versions (tags) of one server. |
| `GET` | `/v0.1/servers/{serverName}/versions/{version}` | Get one version. `{version}` accepts `latest`. |

Responses use the upstream envelope verbatim:

```json
{
  "servers": [
    { "server": { "$schema": "…/2025-12-11/server.schema.json", "name": "…", "version": "…", "packages": [], "remotes": [] },
      "_meta": { "io.modelcontextprotocol.registry/official": { "status": "active", "isLatest": true, "publishedAt": "…", "updatedAt": "…" } } }
  ],
  "metadata": { "nextCursor": "…", "count": 1 }
}
```

### Server names

The catalogue is **flattened across every namespace**. Each server's `name` is `"<namespace>/<resourceName>"` — one forward slash, as the spec requires, unique across namespaces, and reversible. On the get-by-name routes the `{serverName}` segment must be URL-encoded (the slash as `%2F`), e.g. `GET /v0.1/servers/default%2Fweather/versions/latest`.

## Pointing a client at it

Registry-aware clients take a **base URL** and append the standard relative path (`/v0.1/servers`) themselves — you configure the base URL only, never the full path. For VS Code this is the enterprise/organization Copilot policy `McpGalleryServiceUrl` (it is not a per-user setting). Set it to:

- the registry root (e.g. `https://registry.example.com`) when no prefix is configured, or
- `https://registry.example.com/mcp-registry` when `AGENT_REGISTRY_MCP_REGISTRY_COMPAT_PATH_PREFIX=/mcp-registry`.

The client will then request `<base>/v0.1/servers`.

## Configuration

| Env var | Default | Meaning |
| --- | --- | --- |
| `AGENT_REGISTRY_MCP_REGISTRY_COMPAT_ENABLED` | `true` | Toggle the compatibility API. |
| `AGENT_REGISTRY_MCP_REGISTRY_COMPAT_PATH_PREFIX` | `""` | Optional base prefix to mount under (e.g. `/mcp-registry`). Empty serves the spec paths at the root — these do not collide with the native `/v0/*` API. |

## Caveats

- **Anonymous and unfiltered.** The endpoint requires no authentication and reads MCPServer rows directly from the store across all namespaces. It does **not** invoke per-kind authorization or list-filter hooks. In the OSS build reads are already public, so this exposes nothing new; in a downstream build that gates MCPServer reads with RBAC, the compatibility endpoint would bypass those gates. **Disable it (`…COMPAT_ENABLED=false`) in deployments that restrict who may read MCP servers.**
- **v0.1 only.** The legacy, deprecated `v0` API is not served.
- **Best-effort field mapping.** `http` package transports are surfaced as `streamable-http` with a synthesized `http://localhost:<port><path>` URL; a server's catalogue `version` is derived from the package origin (npm/pypi version, OCI tag/digest) and falls back to the tag or `0.0.0`.
