# AuthZ Matrix

Permissions listed are what the configured `AuthzProvider` is called with. The OSS public provider allows everything; the matrix describes what a non-public provider evaluates.

Resource types recognized by the authz system: `agent`, `server` (MCP server), `skill`, `prompt`, `provider`. **There is no `deployment` resource type**: deployment endpoints authorize against the underlying MCP server or agent the deployment references.

## Agents, servers, skills, prompts

These four kinds share the same endpoint shape. `{kind}` = `agent` | `server` | `skill` | `prompt`.

| Operation | HTTP | Required permissions | Notes |
| --- | --- | --- | --- |
| List | `GET /v0/{kind}s` | none | Filtering is delegated to the provider implementation; the list boundary intentionally skips checks. |
| Get latest tag | `GET /v0/{kind}s/{name}` | `Read` on `{kind}:{name}` | Resolves the literal `latest` tag. |
| Get exact tag | `GET /v0/{kind}s/{name}/{tag}` | `Read` on `{kind}:{name}` | |
| List tags | `GET /v0/{kind}s/{name}/tags` | `Read` on `{kind}:{name}` | |
| Apply | `POST /v0/apply` | `Read` + `Publish` or `Read` + `Edit` on `{kind}:{name}` | Creates or replaces `metadata.tag`; omitted tags resolve to literal `latest`. |
| Delete latest tag | `DELETE /v0/{kind}s/{name}` | `Delete` on `{kind}:{name}` | Deletes the literal `latest` tag. |
| Delete exact tag | `DELETE /v0/{kind}s/{name}/{tag}` | `Delete` on `{kind}:{name}` | |

## Runtimes

**NOTE**: Keyed by `runtimeId`, not name. No edit endpoint is exposed (a DB-layer `UpdateRuntime` method exists but no HTTP route calls it).

| Operation | HTTP | Required permissions | Notes |
| --- | --- | --- | --- |
| List | `GET /v0/runtimes` | none | Filtering is delegated to the runtime implementation; the list boundary intentionally skips checks. |
| Create | `POST /v0/runtimes` | `Publish` on `runtime:{id}` | |
| Get | `GET /v0/runtimes/{runtimeId}` | `Read` on `runtime:{id}` | |
| Delete | `DELETE /v0/runtimes/{runtimeId}` | `Read` + `Delete` on `runtime:{id}` | Service resolves the runtime before deletion, requiring `read`. |

## Deployments

Deployments are identified by `{namespace}/{name}` and authz always evaluates against the underlying artifact (`server` or `agent`) the deployment references. Artifact kind is inferred from `Deployment.Spec.TargetRef.Kind`.

Every deployment lifecycle operation — launching, undeploying, cancelling — gates on `Deploy` against the underlying artifact. The `Delete` verb is reserved for deleting the artifact itself (e.g. `DELETE /v0/servers/{name}/{tag}`), not tearing down a running deployment of it.

| Operation | HTTP | Required permissions |
| --- | --- | --- |
| List | `GET /v0/deployments` | none — filtering delegated to provider implementation |
| Get | `GET /v0/deployments/{name}?namespace={namespace}` | `Read` on target `{agent,server}:{name}` |
| Create / update desired state | `PUT /v0/deployments/{name}?namespace={namespace}` | `Read` on `provider:{id}`; `Read` + `Deploy` on target |
| Delete | `DELETE /v0/deployments/{name}?namespace={namespace}` | `Read` + `Deploy` on target |
| Logs | `GET /v0/deployments/{name}/logs?namespace={namespace}` | `Read` on target |

Agent deployments additionally invoke `Read` on each referenced `skill:{ref}` and `prompt:{ref}` when the platform adapter resolves the agent's manifest before deploying. These reads run under the caller's session (not a system context), so the user triggering the deployment must have `Read` on every manifest-referenced skill and prompt.

**Partial permissions leave stale `Failed` rows.** The Deployment resource row is written before the adapter resolves manifest references. A missing `Read` on any skill/prompt fails inside adapter apply, the caller gets 403, and the row is then patched to a failed condition under system context. No platform resources are created.

## Batch (apply)

| Operation | HTTP | Required permissions | Notes |
| --- | --- | --- | --- |
| Apply | `POST /v0/apply` | Per-document; depends on kind and whether the row already exists | Each document dispatches to its kind handler individually; partial failure is allowed. Artifacts (`agent`/`server`/`skill`/`prompt`): `Read` + `Publish` if the tag is new, `Read` + `Edit` if it already exists. `provider`: `Read` + `Edit` if it exists, `Read` + `Publish` if new. `deployment`: same as `PUT /v0/deployments/{name}?namespace={namespace}`. |
| Delete | `DELETE /v0/apply` | Per-document; depends on kind | Artifacts: `Delete` on `{kind}:{name}`. `provider`: `Read` + `Delete` on `provider:{name}`. `deployment`: `Deploy` on target (see Deployments section). |

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
| `arctl export` | Every individual readme fetch (`GetServerReadme`). List is not a regression because List intentionally skips checks. | `Read` on `server:{name}` per server whose readme is exported. |
