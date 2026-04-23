# Design Decisions — v1alpha1 Refactor

One-stop log of the non-obvious choices made during the
`refactor/v1alpha1-types` branch. Each entry records the decision, the
alternative considered, and the reasoning — future contributors should
re-read this before re-opening a closed question.

Paired reading: `REVIEW_GUIDE.md` (per-commit reading order),
`REBUILD_TRACKER.md` (subsystem-level finish-line signals),
`REMAINING.md` (3-minute what's-left), `design-docs/*.md` (deep-dive
design drafts).

---

## Strategy

### S1. Subsystem-at-a-time, never big-bang

A nuclear-cutover attempt landed once at tag
`refactor/v1alpha1-types-cutover-reference` (15k+ LOC deleted in a
single commit) and was reverted. Every commit on this branch leaves the
build green and the system runnable. Each subsystem's legacy code is
gone at its port PR; there is no long-lived "both stacks alive"
steady-state.

**Why:** the cutover attempt broke too many cross-package assumptions
at once — reviewing and de-risking a 15k-LOC diff is not feasible. The
incremental approach is slower to type but faster to merge.

### S2. No parallel DTOs / `// DEPRECATED` annotations in the final state

When a subsystem ports, its legacy types + wire shapes are deleted in
the same commit. No "we'll clean it up later" stubs — except where
cross-branch coordination explicitly needs them (see S3).

**Why:** every `// DEPRECATED` survives three release cycles and
becomes permanent. Deleting at port-PR time enforces the discipline.

### S3. `client_deprecated.go` was the one temporary exception — removed in `709a23d`

During the handoff window the branch carried
`internal/client/client_deprecated.go`, stubbing the legacy per-kind
imperative methods (`GetServer`, `CreatePrompt`, `DeployAgent`, etc.)
with `errDeprecatedImperative` so another engineer's CLI branch could
keep compiling.

Follow-up commit `709a23d` deleted the file after porting the
provider/deployment/declarative CLI paths onto the generic v1alpha1
client + typed helpers. The remaining cleanup now lives in workflow CLI
manifest compatibility/projection code, not in runtime-error client
stubs or registry-side DTO packages.

**Why the temporary exception existed:** breaking the imperative CLI
build mid-handoff would have blocked the parallel CLI work. The stubs
bought enough time to port those paths without re-implementing the
legacy contract.

---

## Type system

### T1. `v1alpha1.ResolverFunc` is existence-only; `GetterFunc` is the getter

`ResolverFunc func(ctx, ResourceRef) error` predates the platform
adapter port. When Group 5 needed to fetch nested MCPServer refs to
build agentgateway upstream configs, rather than reshape `ResolverFunc`
to return `(Object, error)` — which would break ~8 call sites
(validation, scheme, apply handler, importer, tests) — we added a
sibling `GetterFunc func(ctx, ResourceRef) (Object, error)` and
threaded it through `ApplyInput.Getter`.

**Alternative considered:** change `ResolverFunc` to return `Object`.
**Trade-off:** two sibling types slightly expand the v1alpha1 surface,
but existing callers stay one-line.

### T2. Object interface grew `ValidateUniqueRemoteURLs` parallel to `ResolveRefs` / `ValidateRegistries`

URL-uniqueness is a cross-row invariant (no two MCPServers claim the
same remote URL). Rather than inject it at the handler or Store level,
the v1alpha1 `Object` interface gained a dedicated method that
dispatches through a `UniqueRemoteURLsFunc` backed by
`Store.FindReferrers` against a JSONB containment fragment.

**Why:** parity with how `ResolveRefs(ctx, resolver)` and
`ValidateRegistries(ctx, validator)` work — every I/O-bound validation
is a method on the envelope that takes an injected callback. The
pattern is uniform; the handler wiring is trivial.

### T3. `accessors.go` boilerplate preserved

Six kinds × seven accessor methods (`GetAPIVersion / GetKind /
SetTypeMeta / GetMetadata / SetMetadata / GetStatus / SetStatus`) ≈
~80 lines of mechanical boilerplate. An embedded `typedBase` struct
could eliminate it, but changing the public shape of every kind is
large downstream blast radius and the Marshal/Unmarshal pair can't be
collapsed without giving up typed Spec.

**Decision:** leave as-is until a concrete reader/extension benefits
from the collapse.

---

## Deployment lifecycle

### D1. Apply is ALWAYS async

Every `DeploymentAdapter.Apply` returns immediately with a
`Progressing=True` condition + finalizer. Convergence to `Ready=True`
is an adapter-internal watch loop writing status via
`Store.PatchStatus`. The coordinator doesn't block on convergence.

**Alternative considered:** block in `Apply` for local adapter (docker
compose up is synchronous anyway) and stream for kubernetes. Rejected
because mixed semantics per-adapter undermine the uniform reconciler
contract — Phase 2 KRT walks one loop regardless of platform.

### D2. Finalizer ownership is with the adapter

Each adapter declares `AddFinalizers = ["<platform>.agentregistry.solo.io/cleanup"]`
on `ApplyResult` and drops the same token on `RemoveResult`. The
coordinator is a dumb courier — calls `Store.PatchFinalizers` with the
declared set.

**Why:** no central finalizer registry. Enterprise-added platforms own
their finalizer tokens under their own prefix; naming collisions are
their problem.

### D3. Cancel is subsumed by DesiredState+DELETE

The legacy interface had `Cancel(ctx, deployment)`; the v1alpha1
interface doesn't. Cancel now happens via either:
- Client PUTs spec with `desiredState: undeployed` → reconciler routes
  to `adapter.Remove`.
- Client DELETEs the Deployment row → soft-delete sets
  `deletionTimestamp` → reconciler routes to `adapter.Remove`.

**Why:** two verbs that mean "stop running" is one too many. The
DesiredState flag subsumes the common "pause without deleting" case;
DELETE handles the rest.

### D4. Provider config stays `map[string]any`

`v1alpha1.ProviderSpec.Config map[string]any` — no schema check at
apply time. Adapters unmarshal the map at Apply into their own typed
config struct (e.g. `LocalProviderConfig`, `KubernetesProviderConfig`)
and surface a `ProviderConfigured=False` condition on parse failure.

**Alternative:** preflight validation hook on the apply handler.
Rejected: over-engineering for demo-mode. Revisit if enterprise
multi-cluster misconfigurations become a support burden.

### D5. Provider credentials stay inline in `Config`

Legacy already stored kubeconfig as a raw string under
`provider.Config.kubeconfig`. The v1alpha1 port preserves that. A
proper `SecretRef` → K8s Secrets / Vault / AWS SM design is deferred
to a dedicated secret-management pass.

**Why:** inventing a secret-backend abstraction here would sprawl the
PR. Authz already gates who can read Provider rows; that's the hand-off
point for now.

### D6. Discovery returns `[]DiscoveryResult`, caller writes

`DeploymentAdapter.Discover` returns entries; the enterprise Syncer
persists them to `discovered_*` tables. The adapter never writes
outside its own Deployment's `Status.PatchStatus`.

**Why:** separation of concerns — adapters stay pure (no direct
cross-table writes). Each table becomes the Syncer's responsibility,
which is where policy (merge, conflict resolution) already lives.

### D7. `PostUpsert` failure returns HTTP 500 with the row already persisted

If `V1Alpha1Coordinator.Apply` (wired to `Config.PostUpsert`)
returns an error, `handler.go:306-308` returns `500 Internal Server
Error` — but `Store.Upsert` committed in its own transaction on line
292 before the hook fired. The caller sees a 500 with a persisted
row; retrying is safe because `Upsert` is idempotent on a matching
spec (no generation bump) and `coord.Apply` is designed to be
reapplied.

**Alternative considered:** wrap Upsert + PostUpsert in one
transaction so a hook failure rolls back the row. Rejected because:
1. Adapter calls can be long-running (docker compose up, kubectl
   apply) and holding a DB transaction across them would serialize
   applies + risk timeouts.
2. Apply is ALWAYS async per D1 — the row persisting is the correct
   contract, and the adapter is responsible for writing terminal
   state into `Status.Conditions` via `PatchStatus` on its own watch
   loop.

**Phase 2 KRT dissolves this.** When KRT replaces the synchronous
coordinator, `PostUpsert` becomes a no-op and dispatch moves to the
NOTIFY-driven reconciler. The apply handler no longer blocks on
adapter work; the caller observes convergence by polling
`Status.Conditions`.

**Until then, document for clients:** a 500 from an apply means
"the row is stored, adapter failed to drive it to Ready; poll
Status or retry apply". The hook logs the failure; subsequent
reapplies with unchanged spec re-run PostUpsert without bumping
generation.

---

## HTTP surface

### H1. Deployment HTTP handlers collapse into the generic resource handler

No bespoke `handlers/v0/deployments/` package. PUT at
`/v0/namespaces/{ns}/deployments/{name}/{v}` goes through the same
`resource.Register[*v1alpha1.Deployment]` code path as every other
kind, with two optional hooks (`PostUpsert`, `PostDelete`) that fire
after the Store write.

**Why:** the per-kind handler pattern accumulated copy-paste every
time a new kind landed. One generic handler + hooks means new kinds
need only a Store entry in the BuiltinKinds map.

### H2. Logs endpoint is non-streaming for now

`GET /v0/namespaces/{ns}/deployments/{name}/{v}/logs` drains
`coord.Logs` and returns a JSON array. Both adapters currently return
immediately-closed channels — real log streaming will swap the handler
for SSE/chunked at the same path without breaking the coordinator.

**Why:** huma v2 has limited first-class SSE support, and the adapter
Logs implementations don't return real lines yet. Getting the path +
shape locked in lets clients plan integration; the transport upgrade
is mechanical.

### H3. README blobs don't move yet

The old `server_readmes` BYTEA table was too server-specific for the new
model. The replacement is a shared `Spec.Readme` block on Agent,
MCPServer, Skill, and Prompt, plus generic readme subresource routes for
lazy-loading the heavy markdown body. Collection endpoints strip
`Readme.Content` so list calls stay cheap. A thin MCP-server alias route
remains temporarily because downstream UIs still call the old path.

**Decision:** generalize the data model rather than keep an MCP-only
table. Revisit separate storage only if inline-spec size becomes an
actual performance problem.

---

## Platform adapter interface

### A1. Single struct satisfies both interfaces during Group 5 transition

For 5.a–e, each concrete adapter struct satisfied both
`registrytypes.DeploymentPlatformAdapter` (legacy) and
`pkg/types.DeploymentAdapter` (v1alpha1). Method names collided on
`Discover`, so the legacy one was renamed `LegacyDiscover`.

**Alternative:** a wrapper struct per adapter. Rejected — the legacy
surface is deleted in 5.f anyway, and one struct keeps the transition
easier to reason about.

### A2. `providerBridgeFromV1Alpha1` for kubernetes adapter

Kubernetes helpers (`kubernetesGetClient`, `kubernetesApplyPlatformConfig`,
`kubernetesDeleteResourcesByDeploymentID`) were built around
`*models.Provider`. Rather than rewrite them for v1alpha1, a small
shim projects `*v1alpha1.Provider` onto a minimal `*models.Provider`
(ID + Config only). The shim collapsed in 5.f once the legacy
adapter methods went; the helpers now take the v1alpha1 provider
directly.

### A3. Translation helpers live in `platforms/utils` not duplicated

Local + Kubernetes adapters produce identical `platformtypes.MCPServer`
/ `Agent` structs from `v1alpha1.Spec`. Rather than near-duplicate
code in each adapter package, the shared translate helpers
(`SpecToPlatformMCPServer`, `SpecToPlatformAgent`,
`SplitDeploymentRuntimeInputs`) live in `platforms/utils`. Adapter
knobs (Namespace handling, KagentURL) sit on per-call Opts structs.

### A4. Legacy `utils.TranslateMCPServer` preserved, not ported

The v1alpha1 translate helpers call through to
`utils.TranslateMCPServer` — which speaks the legacy `apiv0.ServerJSON`
shape. A tiny adapter (`mcpServerSpecToServerJSON`) projects
`v1alpha1.MCPServerSpec` onto that shape. Same translator serves
both v1alpha1 adapters and the imperative CLI
(`internal/cli/mcp/run.go`, `internal/cli/agent/utils/registry_translator.go`).

**Why:** ~250 LOC of package-registry dispatch logic (NPM/PyPI/OCI
runtime selection, arg processing, env merge) is already correct and
well-tested. Re-deriving it from v1alpha1 types directly would duplicate
that logic. The adapter layer is net +20 LOC; the alternative would have
been net −0 with +300 duplicated LOC.

---

## Database

### DB1. Two PostgreSQL schemas: `v1alpha1.*` (live) + `public.*` (gone)

The branch started with `v1alpha1.*` coexisting alongside `public.*`.
As of Group 12/13 the `public.*` schema + its 11 legacy migrations are
deleted. Production DBs run only the `migrations_v1alpha1/` set
(version offset 200 for compatibility with any enterprise offset 100).

**Why:** dual-schema was a crutch for the transition. With Group 3 done
(no service reads from public.*) + Group 12 done (no postgres_*.go
exists), the dual-write + dual-read pattern is pure overhead.

### DB2. Generic Store, per-kind kinds registry

One `internaldb.Store` type bound to a table; per-kind instances
produced by `NewV1Alpha1Stores(pool) map[string]*Store`, keyed by Kind
name. The Scheme (`pkg/api/v1alpha1.Default`) is the single source of
truth for kind → spec-type mapping.

**Alternative:** per-kind Store types. Rejected — generic + typed
callers gets the compile-time safety via method return types
(`*v1alpha1.RawObject`) + callers decode into typed Spec. Changes to
add a kind: one map entry.

### DB3. Legacy `pkg/registry/database` collapsed to sentinel errors + thin `Store{Pool,Close}`

The public contract `database.Store` interface used to expose
`Servers()`, `Agents()`, etc. returning per-kind readers/stores. Now
it exposes just the sentinel errors plus a tiny root store contract:
`Pool() *pgxpool.Pool` and `Close() error`. `Pool()` lets callers
construct `NewV1Alpha1Stores` without ad-hoc type assertions; backends
without a real PostgreSQL pool return nil and the v1alpha1 routes/seed
path gate on that.

**Why:** the per-kind reader interfaces existed only for the services
that read from them. Services dissolved in Group 3 → interfaces
orphaned → deleted.

---

## MCP bridge

### M1. Tool signatures preserve names; structured outputs change to v1alpha1

Tool names (`list_agents`, `get_server`, `list_deployments`, etc.) stay
identical so existing Claude conversations with stored tool args keep
working. Structured output shape changed from `models.ServerResponse`
/ `apiv0.ServerListResponse` to v1alpha1 envelopes
(`{items: []v1alpha1.MCPServer, nextCursor, count}`).

**Why:** tool names are part of Claude's user-facing prompt cache;
renaming them is a breaking change for saved agents. Output shape
sits behind the `StructuredContent` wrapper which is parsed per-call
— clients re-learn the shape without needing a migration.

### M2. `semantic_search`, `updated_since` filters dropped

MCP tool input retains the argument names (to avoid re-teaching),
but `semantic_search` + `semantic_threshold` + `updated_since` are
ignored until Group 8 re-lands them on the generic Store.

**Why:** the legacy filters were backed by service-layer helpers that
built their own SQL. The v1alpha1 generic Store doesn't expose
`UpdatedSince` or semantic options yet. Drop-and-restore is cleaner
than carrying half-implemented filters.

### M3. `deploy_server` / `deploy_agent` / `remove_deployment` MCP tools retired

These called `deploymentsvc.Registry.DeployServer` etc., which was
deleted in Group 5.f. MCP clients that need to deploy now call the
v1alpha1 apply HTTP surface directly:
`PUT /v0/namespaces/{ns}/deployments/{name}/{v}`.

**Why:** re-implementing the tools against the coordinator would need
ProviderRef + TargetRef resolution plumbing inside the MCP bridge —
duplicating work the HTTP layer already does. Clients that want
deploy-via-MCP can use the MCP server's `http` transport.

---

## What the Phase 2 rebase must know

Phase 2 is a separate branch (`internal-refactor-phase-2`, KRT
reconciler) that lands after this refactor merges. When it rebases:

- Replace the synchronous `V1Alpha1Coordinator.Apply/Remove` path with
  NOTIFY-driven reconciliation. The HTTP handler's `PostUpsert` /
  `PostDelete` hooks become no-ops; the reconciler loop owns the
  dispatch.
- Condition-based status queries: `hasCondition(d.Status.Conditions,
  "Ready", ConditionTrue)` replaces any surviving scalar-status
  checks (`d.Status != "deployed"`).
- `UpdateDeploymentState` → `Store.PatchStatus` with a mutator closure
  that calls `SetCondition`.
- Extend the SSE watch handler (currently not yet re-introduced) over
  Scheme-registered kinds so the UI can subscribe by kind.
