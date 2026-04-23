# v1alpha1 Port Tracker

> **Purpose**: every legacy subsystem that spoke `pkg/models` DTOs and
> per-kind database stores had to be ported to speak `pkg/api/v1alpha1`
> types and the generic `internal/registry/database.Store`. This tracker
> enumerated each subsystem; only the remaining entries are kept here.
>
> **Companion reads**: `REVIEW_GUIDE.md` (per-commit reading order),
> `DECISIONS.md` (design choices + rationale), `REMAINING.md` (3-minute
> punch list).
>
> **Delete an entry** once that subsystem's port PR has merged.
> **Delete this file** once every entry is resolved.

**Branch state**:
- Current: `refactor/v1alpha1-types` (~58 commits, **net −21k LOC**).
- 10 of the original 14 groups are done — entries pruned from this
  file per the "delete once resolved" rule. See `REMAINING.md` §"What
  is now DONE" for the summary table with commit refs.

**Generic primitives in place**:
- `pkg/api/v1alpha1` — typed envelopes (Agent, MCPServer, Skill,
  Prompt, Provider, Deployment), `Object` interface with
  `Validate / ResolveRefs / ValidateRegistries /
  ValidateUniqueRemoteURLs`, Scheme, Conditions, ResourceRef,
  GetterFunc + ResolverFunc.
- `internal/registry/database.Store` — generic per-table CRUD over
  v1alpha1 rows with `Upsert` (generation + semver is_latest),
  `PatchStatus`, `PatchFinalizers`, `List`, `FindReferrers`,
  `GetLatest`, `PurgeFinalized`.
- `pkg/types.DeploymentAdapter` — v1alpha1 platform adapter interface
  with `Apply / Remove / Logs / Discover / SupportedTargetKinds`.
- `service/deployment.V1Alpha1Coordinator` — synchronous
  apply-time orchestrator; replaced by Phase 2 KRT reconciler.
- `resource.Register[T]` + `RegisterBuiltins` + `RegisterApply` +
  `RegisterImport` + `RegisterDeploymentLogs` — generic HTTP surface.
- `internal/mcp/registryserver` — v1alpha1-native MCP tools.

---

## 8. Embeddings / pgvector indexer — **NOT STARTED (net-new work)**

**Status**: legacy indexer + provider + jobs + handlers deleted in
commit `2cbf1c2`. v1alpha1-native pipeline is net-new work against
the generic Store + NOTIFY stream.

**Port plan**:
- Additive migration `002_v1alpha1_embeddings.sql` adds
  `semantic_embedding`, `semantic_embedding_provider`, `..._model`,
  `..._dimensions` columns to every `v1alpha1.*` table.
- Indexer reads rows via Store, generates embedding, writes via
  `store.SetEmbedding(namespace, name, version, vec)` (new method).
- SSE index endpoint targets v1alpha1 rows.
- Subscribe to per-table status-change NOTIFY for incremental reindex
  on spec change.

**Business logic to preserve** (from the deleted legacy impl):
- Per-kind text assembly (which Spec fields contribute to the
  embedding input — different for Agent vs Prompt).
- Embedding regen on generation bump.
- Manual reindex of all rows via `POST /v0/embeddings/index`.
- SSE progress streaming with job state machine.
- Semantic search via `?semantic=<q>` param in list endpoints.
- `?semanticThreshold=` param.
- Provider abstraction so Azure OpenAI / Ollama / local models plug in.

**Finish-line signal**: `arctl embeddings index --watch` surfaces
progress; `GET /v0/agents?semantic=payments` returns relevance-ordered
matches.

---

## 10. CLI (`arctl`) — **SKIP**

Per plan, the legacy CLI is being replaced by a different engineer on
a separate branch with a declarative-only model (`arctl apply -f` as
the primary verb). We do NOT port the per-kind imperative commands
here.

**Preserved content** (restore when needed):
- `internal/cli/agent/frameworks/adk/python/templates/` (already in
  cutover-reference tag under `d04d03d`; retrieve with
  `git checkout refactor/v1alpha1-types-cutover-reference -- internal/cli/agent/frameworks/adk/python/templates`).
- `internal/cli/mcp/frameworks/{golang,python}/templates/` (same).
- `internal/cli/skill/templates/hello-world/` (same).

---

## 11. `pkg/models` DTOs / `internal/registry/kinds` — workflow CLI cleanup remaining

**Current remaining consumers**: workflow CLI + manifest paths
(`internal/cli/{agent,mcp,scheme}/`, `internal/cli/agent/project`,
`internal/cli/agent/frameworks/common`), selected platform translation
helpers, and `internal/registry/kinds/`.
`internal/client/client_deprecated.go` was deleted in `709a23d`; the
remaining package is no longer blocked on a single declarative-CLI
merge, just on finishing those real ports.

**Finish-line signal**: `grep -r pkg/models internal pkg cmd`
returns no hits → delete the package.

---

## 14. OSS/enterprise extension points — **NOT STARTED**

**Current state**: `pkg/types.AppOptions` exposes:
- `DatabaseFactory` — wraps/extends the base store (enterprise uses
  it for migrations + authz).
- `ProviderPlatforms map[string]ProviderPlatformAdapter` — per-platform
  provider CRUD.
- `DeploymentAdapters map[string]DeploymentAdapter` — v1alpha1
  adapter map; enterprise plugs in cloud-provider adapters here.
- `ExtraRoutes`, `HTTPServerFactory`, `OnHTTPServerCreated`,
  `UIHandler`, `AuthnProvider`, `AuthzProvider`.

**Port plan**:
- `DatabaseFactory` signature stays; the legacy `database.Store`
  interface is now the thin root `Pool()/Close()` contract so
  enterprise wrappers can expose the underlying pgx pool without
  ad-hoc type assertions.
- `ProviderPlatformAdapter` now uses v1alpha1 `Provider` resources; the
  remaining cleanup is deletion of the old DTO package once workflow CLI
  callers are ported.
- `DeploymentPlatformAdapter` interface deleted — enterprise adapters
  register via `DeploymentAdapter` (the v1alpha1 interface).
- Enterprise kind registration via `v1alpha1.Scheme.Register` — already
  supported.
- `ExtraRoutes` unchanged — huma-level hook.

**Business logic to preserve**:
- [ ] Enterprise must be able to plug in additional kinds, register
      additional platform adapters, and wrap the database from outside
      the OSS module.
- [ ] Enterprise Syncer writes to `discovered_*` tables — adjust for
      the v1alpha1-shape records from `DeploymentAdapter.Discover()`.

**Finish-line signal**: enterprise builds against the v1alpha1 OSS
`main` branch with zero code changes other than updated adapter
signatures + kind registration calls.

---

## Phase 2 rebase — **SEPARATE BRANCH**

`internal-refactor-phase-2` (KRT reconciler, 6 commits, +11k/-9k)
rebases onto post-refactor `main`:

- Replace the synchronous `V1Alpha1Coordinator` apply path with
  NOTIFY-driven reconciliation. HTTP handler hooks become no-ops; the
  reconciler loop owns adapter dispatch.
- Condition-based status queries
  (`hasCondition(Conditions, "Ready", ConditionTrue)`) replace scalar
  status string checks.
- Status writes route through `Store.PatchStatus` with closures that
  call `SetCondition`; finalizer drops via `Store.PatchFinalizers`.
- Extend SSE watch handler to work over Scheme-registered kinds.
