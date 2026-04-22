# AuthZ Matrix

Permissions listed are what the configured `AuthzProvider` is called with. The OSS public provider allows everything; the matrix describes what a non-public provider evaluates.

Resource types recognized by the authz system: `agent`, `server` (MCP server), `skill`, `prompt`, `provider`. **There is no `deployment` resource type**: deployment endpoints authorize against the underlying MCP server or agent the deployment references.

## Agents, servers, skills, prompts

These four kinds share the same endpoint shape. `{kind}` = `agent` | `server` | `skill` | `prompt`.

| Operation | HTTP | Required permissions | Notes |
| --- | --- | --- | --- |
| List | `GET /v0/{kind}s` | none | Filtering is delegated to the provider implementation; the list boundary intentionally skips checks. |
| Get version | `GET /v0/{kind}s/{name}/versions/{version}` | `Read` on `{kind}:{name}` | |
| Get versions | `GET /v0/{kind}s/{name}/versions` | `Read` on `{kind}:{name}` | |
| Publish | `POST /v0/{kind}s` | `Read` + `Publish` on `{kind}:{name}` | Creates a new name+version. Duplicate name+version is rejected — update paths are `PATCH /v0/servers/...` (servers only) or `POST /v0/apply` (any kind). `Read` covers pre-create lookups (version existence, latest resolution). |
| Delete version | `DELETE /v0/{kind}s/{name}/versions/{version}` | `Delete` on `{kind}:{name}` | |

## MCP Server-only endpoints

| Operation | HTTP | Required permissions | Notes |
| --- | --- | --- | --- |
| Edit | `PATCH /v0/servers/{name}/versions/{version}` | `Read` + `Edit` on `server:{name}` | Handler fetches the current version before applying the patch. No equivalent endpoint exists for agents, skills, or prompts — updates to those kinds go through `POST /v0/apply`. |
| Get latest readme | `GET /v0/servers/{name}/readme` | `Read` on `server:{name}` | |
| Get version readme | `GET /v0/servers/{name}/versions/{version}/readme` | `Read` on `server:{name}` | |

## Providers

**NOTE**: Keyed by `providerId`, not name. No edit endpoint is exposed (a DB-layer `UpdateProvider` method exists but no HTTP route calls it).

| Operation | HTTP | Required permissions | Notes |
| --- | --- | --- | --- |
| List | `GET /v0/providers` | none | Filtering is delegated to the provider implementation; the list boundary intentionally skips checks. |
| Create | `POST /v0/providers` | `Publish` on `provider:{id}` | |
| Get | `GET /v0/providers/{providerId}` | `Read` on `provider:{id}` | |
| Delete | `DELETE /v0/providers/{providerId}` | `Read` + `Delete` on `provider:{id}` | Service resolves the provider before deletion, requiring `read`. |

## Deployments

Deployments are identified by an opaque `{id}` but authz always evaluates against the underlying artifact (`server` or `agent`) the deployment references. Artifact kind is inferred from the deployment row's `ResourceType`.

Every deployment lifecycle operation — launching, undeploying, cancelling — gates on `Deploy` against the underlying artifact. The `Delete` verb is reserved for deleting the artifact itself (e.g. `DELETE /v0/servers/{name}/versions/{v}`), not tearing down a running deployment of it.

| Operation | HTTP | Required permissions |
| --- | --- | --- |
| List | `GET /v0/deployments` | none — filtering delegated to provider implementation |
| Get | `GET /v0/deployments/{id}` | `Read` on target `{agent,server}:{name}` |
| Create | `POST /v0/deployments` | `Read` on `provider:{id}`; `Read` + `Deploy` on target |
| Delete | `DELETE /v0/deployments/{id}` | `Read` + `Deploy` on target |
| Cancel | `POST /v0/deployments/{id}/cancel` | `Read` + `Deploy` on target |
| Logs | `GET /v0/deployments/{id}/logs` | `Read` on target (read-only) |

Agent deployments additionally invoke `Read` on each referenced `skill:{ref}` and `prompt:{ref}` when the platform adapter resolves the agent's manifest before deploying. These reads run under the caller's session (not a system context), so the user triggering the deployment must have `Read` on every manifest-referenced skill and prompt.

**Partial permissions leave stale `Failed` rows.** The DB deployment row is written (status `Deploying`) inside `CreateManagedDeploymentRecord` before the adapter resolves manifest references. A missing `Read` on any skill/prompt fails inside `adapter.Deploy`, which returns `auth.ErrForbidden` → caller gets 403, but the row is then patched to status `Failed` (under system context) and left in the DB. No platform resources are created.

## Batch (apply)

| Operation | HTTP | Required permissions | Notes |
| --- | --- | --- | --- |
| Apply | `POST /v0/apply` | Per-document; depends on kind and whether the version already exists | Each document dispatches to its kind handler individually; partial failure is allowed. Artifacts (`agent`/`server`/`skill`/`prompt`): `Read` + `Publish` if the version is new, `Read` + `Edit` if it already exists. `provider`: `Read` + `Edit` if it exists, `Read` + `Publish` if new (there is no direct provider update endpoint; apply is the only update path). `deployment`: same as `POST /v0/deployments`. |
| Delete | `DELETE /v0/apply` | Per-document; depends on kind | Artifacts: `Delete` on `{kind}:{name}`. `provider`: `Read` + `Delete` on `provider:{name}`. `deployment`: `Deploy` on target (see Deployments section). |

## Admin

No `(verb, resource)` tuple — the operation is global.

| Operation | HTTP | Required permissions | Notes |
| --- | --- | --- | --- |
| Start embedding index | `POST /v0/embeddings/index` | `IsRegistryAdmin` | Job runs under a system session once the caller is admitted. |
| Stream embedding index | `POST /v0/embeddings/index/stream` | `IsRegistryAdmin` | Registered on the raw mux; authn + admin gate run inline before the job starts. |
| Get index job status | `GET /v0/embeddings/index/{jobId}` | `IsRegistryAdmin` | Gated to avoid leaking job existence. |

## Public

| Operation | HTTP |
| --- | --- |
| Health | `GET /v0/health` |
| Ping | `GET /v0/ping` |
| Version | `GET /v0/version` |
| Docs | `GET /docs` |
| Metrics | `GET /metrics` |
| Logging | `/logging` (localhost-only) |

## Known gaps

Direct-DB CLI commands that construct `auth.Authorizer{Authz: nil}` and therefore short-circuit every DB-layer `Check` to allow. Not a regression vs the trust model of these commands (both require `DATABASE_URL`), but a real gap for audit visibility and for deployments where DB credentials are not equivalent to registry admin.

| Command | What gets bypassed | Permissions that would apply post-refactor |
| --- | --- | --- |
| `arctl import` | Every write through the importer: `Publish` checks on each imported server, and `Edit` checks on the `--update` overwrite path. | `Publish` + `Edit` on `server:{name}` per item. |
| `arctl export` | Every individual readme fetch (`GetServerReadme`). List is not a regression because List intentionally skips checks. | `Read` on `server:{name}` per server whose readme is exported. |
