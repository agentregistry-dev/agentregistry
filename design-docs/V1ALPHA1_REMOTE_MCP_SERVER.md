# Extract `RemoteMCPServer` (and `RemoteAgent`) as top-level v1alpha1 kinds

> Status: **Approved** — team signed off; implementation in progress on `refactor/extract-remote-mcp-server`.
> Scope of this PR:
> 1. Split `MCPServer.Spec.Remotes` out into a new `RemoteMCPServer` kind.
> 2. Symmetric split: `AgentSpec.Remotes` out into a new `RemoteAgent` kind.
> 3. `ResourceRef.Kind` discrimination so `AgentSpec.MCPServers` and `Deployment.Spec.TargetRef` accept either the
>    bundled or the remote variant.
>
> Out of scope (queued for follow-up PRs):
> - The broader v1alpha1 spec reshape (collapsed `AgentSource` / `MCPServerSource`, dropped catalog metadata fields,
>   removed `Skills` / `Prompts` / `Packages` from `AgentSpec`, etc.).
> - Health probing of remote endpoints (`Reachable` / `HandshakeOK` conditions) — deferred until a probe controller
>   is in scope; we do **not** ship unimplemented Status fields.

## 1. Problem

`MCPServer.Spec.Remotes []MCPTransport` overloads the kind. Today an `MCPServer` is doing two unrelated jobs:

1. **Registry template** — catalog entry describing how to *deploy* an MCP server (packages, runtime hints, env/args).
2. **Pre-deployed endpoint pointer** — a URL to an already-running MCP server that the registry only needs to *call*.

Code symptoms of the overload:

- `internal/registry/platforms/utils/deployment_adapter_utils.go:64` — branches on
  `useRemote := len(spec.Remotes) > 0 && (PreferRemote || len(spec.Packages) == 0)`.
- `internal/registry/platforms/utils/helpers.go:46` — auto-flips `PreferRemote` based on which list is populated.
- `MCPServerRunRequest.PreferRemote` exists *only* to disambiguate within a single overloaded resource.
- `MCPServer.Status` has nothing meaningful to say about a remote endpoint (reachability, handshake) because the kind
  also represents a non-deployed template.

Conceptually, a remote MCP server sits at the same level as a `Deployment` — it is *the directly invokable thing*, not a
template. Splitting it into its own kind lets each kind carry the metadata and status semantics that actually fit it.

## 2. Proposed final spec (context for the broader reshape)

The team discussed the following final shape for v1alpha1. **This PR only implements the `RemoteMCPServer` parts**; the
rest is documented here so reviewers see the destination.

```go
// Shared
type Repository struct {
URL       string `json:"url,omitempty"`
Subfolder string `json:"subfolder,omitempty"`
}

type ResourceRef struct {
Kind    string `json:"kind"` // discriminator: "MCPServer" vs "RemoteMCPServer", etc.
Name    string `json:"name"`
Version string `json:"version,omitempty"`
}

// Agent (final shape — not changed in this PR)
type AgentSource struct {
Image      string      `json:"image,omitempty"`
Repository *Repository `json:"repository,omitempty"`
}
type AgentSpec struct {
Title         string        `json:"title,omitempty"`
Description   string        `json:"description,omitempty"`
ModelProvider string        `json:"modelProvider,omitempty"`
ModelName     string        `json:"modelName,omitempty"`
Source        AgentSource   `json:"source,omitempty"`
MCPServers    []ResourceRef `json:"mcpServers,omitempty"` // Kind = "MCPServer" or "RemoteMCPServer"
}

// MCPServer (final shape — not changed in this PR)
type MCPServerSource struct {
Package    *MCPPackage `json:"package,omitempty"`
Repository *Repository `json:"repository,omitempty"`
}
type MCPServerSpec struct {
Title       string          `json:"title,omitempty"`
Description string          `json:"description,omitempty"`
Source      MCPServerSource `json:"source,omitempty"`
}

// RemoteMCPServer (NEW — this PR)
type RemoteMCPServer struct {
    TypeMeta `json:",inline"`
    Metadata ObjectMeta          `json:"metadata"`
    Spec     RemoteMCPServerSpec `json:"spec"`
    Status   Status              `json:"status,omitzero"`
}
type RemoteMCPServerSpec struct {
    Title       string       `json:"title,omitempty"`
    Description string       `json:"description,omitempty"`
    // Validator rejects empty Remote: Type and URL are both required.
    Remote      MCPTransport `json:"remote,omitempty"`
}

// RemoteAgent (NEW — this PR; symmetric to RemoteMCPServer)
type RemoteAgent struct {
    TypeMeta `json:",inline"`
    Metadata ObjectMeta       `json:"metadata"`
    Spec     RemoteAgentSpec  `json:"spec"`
    Status   Status           `json:"status,omitzero"`
}
type RemoteAgentSpec struct {
    Title       string      `json:"title,omitempty"`
    Description string      `json:"description,omitempty"`
    // Remote is the connection endpoint of the running agent.
    // Validator rejects empty Remote: Type and URL are both required.
    Remote      AgentRemote `json:"remote,omitempty"`
}
```

`AgentSpec.Remotes []AgentRemote` is **removed** in this PR (split into `RemoteAgent` rows by the migration).
`MCPServerSpec.Remotes []MCPTransport` is **removed** in this PR (split into `RemoteMCPServer` rows by the migration).

### Why a single `Remote`, not `Remotes []`

`MCPServer.Remotes` was a slice but the deployment translator already only ever read `spec.Remotes[0]` (
`deployment_adapter_utils.go:87`). One running endpoint per `RemoteMCPServer` matches reality. Multi-endpoint fail-over
is a different concern (load balancer / service alias) and can be modeled separately if it ever materializes.

### Discrimination via `ResourceRef.Kind`

`AgentSpec.MCPServers []ResourceRef` accepts either kind, discriminated by `ResourceRef.Kind`:

- `Kind: "MCPServer"` (default) — bundled template; registry deploys it via `Deployment`.
- `Kind: "RemoteMCPServer"` — already running; registry only needs to call it.

`Deployment.Spec.TargetRef` similarly accepts:

- `Kind: "Agent"` / `Kind: "MCPServer"` — bundled, lifecycle-managed.
- `Kind: "RemoteAgent"` / `Kind: "RemoteMCPServer"` — already running; the adapter does a thin pass-through to
  register the endpoint with the platform (no container/process lifecycle).

This keeps a single field on `AgentSpec` and avoids inventing a parallel `RemoteMCPServers` field. The validator in
`agent_validate.go` is updated to accept both kinds; the resolver dispatches on `Kind`.

## 3. Architecture: lifecycle of `RemoteMCPServer` and `RemoteAgent`

Both new kinds describe an endpoint that *already exists*. The registry's job is to:

1. Persist the resource (CRUD, same as every other v1alpha1 kind).
2. When a `Deployment` references the resource, make the downstream platform (kagent on Kubernetes today) aware of
   the endpoint so agents can be wired to it.

Health probing of remote endpoints (reachability / handshake) is **deferred** to a follow-up PR. We do not ship
unimplemented Status fields in this PR; the `Status` envelope exists but no condition keys are populated by this
change. Future probe controller (likely platform-side reporting via kagent rather than registry-side, to avoid
network-policy false negatives) will populate `Reachable` / `HandshakeOK` / `LastChecked` when it lands.

### Provider binding (decision: option B)

`RemoteMCPServerSpec` and `RemoteAgentSpec` stay provider-agnostic. The binding to a `Provider` lives on a
`Deployment` resource whose `TargetRef.Kind` is `RemoteMCPServer` or `RemoteAgent`. The existing deployment dispatch
table extends to the two new target kinds; per-platform adapters branch on `TargetRef.Kind` to do a thin
pass-through (register the endpoint with the platform; no container/process lifecycle).

```
RemoteMCPServer / RemoteAgent  ◄─── stored as v1alpha1 rows (CRUD only)
           ▲
           │ TargetRef.Kind discriminator
           │
Deployment Upsert (HTTP handler)
           │
           ▼
deployment.Coordinator.Apply ──► DeploymentAdapter (per Provider.Platform)
                                       │
                                       └─► kagent.dev/v1alpha2.RemoteMCPServer CR
                                           (for RemoteMCPServer targets)
                                           or kagent.dev/v1alpha2.RemoteAgent CR
                                           (for RemoteAgent targets)
                                           or container lifecycle for bundled targets
```

### Why reuse `Deployment` rather than a per-kind coordinator

Reuses the existing dispatch + adapter table; one coordinator instead of three. The "already running, just register"
semantic is captured by the adapter doing a thin pass-through when `TargetRef.Kind` is one of the remote variants —
no build/run plumbing fires, only the kagent CR emit.

### Interim until KRT (Phase 2)

The deployment `Coordinator` is the synchronous mirror of the future KRT reconciler. Extending it to handle the
remote target kinds means the KRT migration picks up the new kinds 1:1 with no API or behavioral change.

## 4. Naming collision: `RemoteMCPServer`

The name is currently used in two other places:

- `internal/registry/platforms/types/types.go:71` — internal platform DTO (`{Scheme, Host, Port, Path, Headers}`)
  emitted by the translator and consumed by adapters.
- `kagent.dev/v1alpha2.RemoteMCPServer` — downstream kagent CRD we emit into the cluster.

**Proposed resolution:** rename the platform DTO to `RemoteMCPTarget`. Touches:

- `internal/registry/platforms/types/types.go`
- `internal/registry/platforms/kubernetes/platform.go` (`kubernetesTranslateRemoteMCPServer`, list/delete helpers)
- `internal/registry/platforms/utils/deployment_adapter_utils.go` (`BuildRemoteMCPURL`, struct field)
- Test files under `internal/registry/platforms/{kubernetes,utils,local}/`

`v1alpha2.RemoteMCPServer` (kagent) stays — its package qualifier (`v1alpha2.`) disambiguates it from our new
`v1alpha1.RemoteMCPServer`.

## 5. Implementation plan

### 5.1 New files

- `pkg/api/v1alpha1/remotemcpserver.go` — `RemoteMCPServer` + `RemoteMCPServerSpec`.
- `pkg/api/v1alpha1/remotemcpserver_validate.go` — `Remote.Type` ∈ supported transports, `Remote.URL` non-empty +
  parseable. Empty `Remote` rejected.
- `pkg/api/v1alpha1/remoteagent.go` — `RemoteAgent` + `RemoteAgentSpec`.
- `pkg/api/v1alpha1/remoteagent_validate.go` — `Remote.Type` non-empty, `Remote.URL` non-empty + parseable. Empty
  `Remote` rejected.

No per-kind coordinator service packages — the existing `internal/registry/service/deployment/coordinator.go` is
extended to dispatch on `TargetRef.Kind`.

### 5.2 Modifications

- `pkg/api/v1alpha1/doc.go` — add `KindRemoteMCPServer = "RemoteMCPServer"` and `KindRemoteAgent = "RemoteAgent"`;
  append both to `BuiltinKinds`.
- `pkg/api/v1alpha1/scheme.go` — `MustRegister` both new kinds.
- `pkg/api/v1alpha1/mcpserver.go` — **delete** `MCPServerSpec.Remotes`. (NB: the broader `MCPServerSource` collapse
  is out of scope; this PR only removes the `Remotes` field.)
- `pkg/api/v1alpha1/agent.go` — **delete** `AgentSpec.Remotes`. `AgentRemote` type remains (it's the element type of
  `RemoteAgentSpec.Remote`).
- `pkg/api/v1alpha1/mcpserver_validate.go` — drop the `Remotes` validation loop.
- `pkg/api/v1alpha1/agent_validate.go` — drop the `Remotes` validation loop; accept `KindMCPServer` *or*
  `KindRemoteMCPServer` in `AgentSpec.MCPServers` ref validation.
- `pkg/api/v1alpha1/deployment_validate.go:64` — `validateRef(s.TargetRef, KindAgent, KindMCPServer, KindRemoteAgent,
  KindRemoteMCPServer)`.
- `internal/registry/api/handlers/v0/crud/bindings.go` — register binders for the two new kinds.
- `internal/registry/database/v1alpha1/v1alpha1.go` — register stores for `v1alpha1.remotemcpservers` and
  `v1alpha1.remoteagents`.
- `internal/registry/platforms/types/types.go` — rename `RemoteMCPServer` → `RemoteMCPTarget`; update field tag in
  `MCPServer.Remote`.
- `internal/registry/platforms/utils/deployment_adapter_utils.go` — drop `useRemote`/`usePackage` branching; remove
  `translateRemoteMCPServer`; remove `MCPServerRunRequest.PreferRemote`. New helper translates a
  `*v1alpha1.RemoteMCPServer` directly into `*platformtypes.MCPServer{Remote: ...}`.
- `internal/registry/platforms/utils/helpers.go:46` — drop the `PreferRemote` autoset.
- `internal/registry/platforms/kubernetes/platform.go` — adapter dispatches on `TargetRef.Kind`. `RemoteMCPServer`
  emits `kagent.dev/v1alpha2.RemoteMCPServer` directly from the new v1alpha1 row (not from a synthesized
  `MCPServer.Remotes`). `RemoteAgent` emits the kagent equivalent.
- `internal/registry/platforms/local/adapter.go` — pass-through for both remote kinds (no container lifecycle).
- `internal/registry/platforms/noop/adapter.go` — identity for both remote kinds.
- `internal/registry/service/deployment/coordinator.go` — `Getter`-based target resolution extends to the two new
  kinds (`Coordinator.Apply` already routes via `Getter`; the Getter map gains entries).
- `internal/cli/agent/manifest/resolve.go:74` — resolver dispatches on `ResourceRef.Kind`; remote path fetches a
  `RemoteMCPServer` resource instead of reading `MCPServer.Remotes`.
- `internal/registry/embeddings/helpers.go:27` — drop `Remotes` from MCPServer embedding; add embedding helpers for
  `RemoteMCPServer` and `RemoteAgent`.
- `internal/registry/seed/seed.go` — emit sibling `RemoteMCPServer` / `RemoteAgent` rows for any seed entry currently
  inlining a `Remotes` field into `MCPServer` / `Agent`.

### 5.3 Importer

`internal/registry/api/handlers/v0/importpipeline/` — when the upstream `modelcontextprotocol/registry` `ServerJSON`
carries both `packages` and `remotes`, emit two records: one `MCPServer` (packages only) and one `RemoteMCPServer`
per remote. If only `remotes` is present, the imported entry becomes a `RemoteMCPServer` and no `MCPServer` row is
created.

To keep catalog grouping intact in the UI, the importer stamps
`metadata.annotations["agentregistry.dev/related-mcpserver"]` linking siblings. UI hint only — non-load-bearing.

### 5.4 Authorization

Per the round-2 review fix (H2), add `KindRemoteMCPServer` and `KindRemoteAgent` to the enterprise
`v1alpha1KindArtifactType` table. The OSS boot guard ensures every `BuiltinKinds` entry has authz coverage and will
panic on startup without it. **Paired enterprise PR** lands first; OSS PR merges after.

### 5.5 OpenAPI + UI

- `make gen-openapi && make gen-client` — regenerates TypeScript client.
- UI list + detail pages for `RemoteMCPServer` and `RemoteAgent` (small — three-field specs). UI shim layer per
  recent PRs.
- Catalog page surfaces `RemoteMCPServer` alongside `MCPServer`, grouped by the `related-mcpserver` annotation when
  present.

### 5.6 Tests

- `pkg/api/v1alpha1/remotemcpserver_test.go` and `remoteagent_test.go` — round-trip JSON/YAML, validator coverage
  (empty Remote rejected; missing Type / URL rejected).
- `pkg/api/v1alpha1/scheme_test.go` — extend to cover both new kinds.
- `pkg/api/v1alpha1/validation_test.go` — agent referencing a `RemoteMCPServer` via `Kind` discrimination; deployment
  with `TargetRef.Kind = "RemoteAgent"` / `"RemoteMCPServer"`.
- `internal/registry/service/deployment/coordinator_test.go` — extend with fixtures covering the two new target
  kinds.
- `internal/registry/platforms/kubernetes/adapter_test.go` — `RemoteMCPServer` and `RemoteAgent` emit kagent CRs;
  deletion cleans them up.
- `e2e/mcp_test.go` — end-to-end create-RemoteMCPServer → wire-to-Agent → invoke.
- New `e2e/remote_agent_test.go` — symmetric coverage for `RemoteAgent`.

## 6. Migration

We are alpha; storage migration is acceptable. Plan:

1. **One-shot SQL migration** at server startup, idempotent via a `schema_migrations` row:
    - For every row in `v1alpha1.mcpservers` whose spec contains a non-empty `remotes` array:
        - Insert one row per remote into `v1alpha1.remotemcpservers` (name = `<original>-remote-<i>` if multiple;
          just `<original>` if singular and the original has no packages).
        - If the original has no packages, **delete** the source `mcpservers` row.
        - Otherwise, **strip** `remotes` from the source row's spec.
    - Same logic, applied to `v1alpha1.agents` rows: split non-empty `remotes` into `v1alpha1.remoteagents`.
    - Ref rewrite: any `Deployment.Spec.TargetRef.Kind == "MCPServer"` whose target became remote-only flips to
      `Kind == "RemoteMCPServer"`. Same for `Agent` → `RemoteAgent`.
2. Seed re-emission: re-run seed against the new shape so dev environments stabilize.
3. Reject new writes to `MCPServerSpec.Remotes` and `AgentSpec.Remotes` from the moment the migration runs (the
   field deletions in v1alpha1 are the gate — they no longer round-trip).

## 7. Breaking changes

- `MCPServerSpec.Remotes` removed. Existing manifests with `remotes:` under an `MCPServer` must split.
- `AgentSpec.Remotes` removed. Existing manifests with `remotes:` under an `Agent` must split.
- `MCPServerRunRequest.PreferRemote` removed. Any caller passing `--prefer-remote` (CLI surface) loses the flag — it
  was only ever meaningful inside the overloaded kind.
- Internal type rename `platformtypes.RemoteMCPServer` → `platformtypes.RemoteMCPTarget` (no public-API impact;
  internal-only DTO).

Documented in release notes.

## 8. Estimated cost

Comparable to one of the v1alpha1 incremental-port group PRs (~1–2 days):

- Most of the work is mechanical scaffolding (types, store registration, binder, validator, OpenAPI/UI regen).
- Net code change is a *reduction*: the `useRemote` branching and `PreferRemote` plumbing both disappear.
- Largest moving piece is the platform adapter split + kagent CR emission moving to its own coordinator path.

## 9. Decisions (team signed off)

1. **Provider binding** — option **(B)**: `RemoteMCPServerSpec` / `RemoteAgentSpec` stay provider-agnostic; binding
   lives on `Deployment.Spec.TargetRef`. No per-kind coordinators; `deployment.Coordinator` + adapters dispatch on
   `TargetRef.Kind`.
2. **Single endpoint** — `Remote` is **singular** (`MCPTransport` for `RemoteMCPServer`, `AgentRemote` for
   `RemoteAgent`). Multi-endpoint fail-over is a separate concern.
3. **Validator strictness** — empty `Remote` is **rejected** at write time. `Type` and `URL` are both required.
4. **Catalog grouping annotation** — importer **populates**
   `metadata.annotations["agentregistry.dev/related-mcpserver"]` on split rows. UI hint only.
5. **Symmetric `RemoteAgent` refactor** — **in scope** for this PR. `AgentSpec.Remotes` removed; new `RemoteAgent`
   kind added; same migration logic applies.
6. **Status conditions / probe** — **deferred**. No probe in this PR; no `Reachable` / `HandshakeOK` / `LastChecked`
   condition keys land. The `Status` envelope exists on the new kinds but no fields are populated by this change.
   Probe controller (likely platform-side reporting via kagent) is a separate follow-up.
7. **Migration ordering** — **one-shot at server startup**, idempotent via `schema_migrations` row.
8. **Authz parity** — **paired enterprise PR** lands first to add `KindRemoteMCPServer` and `KindRemoteAgent` to
   `v1alpha1KindArtifactType`; OSS PR merges after to satisfy the boot guard.
