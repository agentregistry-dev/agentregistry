# v1alpha1 API Refactor — Review Guide

Branch: `refactor/v1alpha1-types` (at `6f910f2`, +14 commits since last guide revision at `5e64cec`)
Scope: **75 commits**, +15.6k / −36.2k LOC across 230 files — **net ~20.7k LOC removed**. Unit tests: green on every package except ones currently under active merge-conflict resolution with `main`. Integration tests green for every shipped lane.
Epic: replace the five-layer type stack (`kinds.Document` → `Spec` → wire DTO → `Record` → JSONB blob) with one Kubernetes-style `ar.dev/v1alpha1` envelope that propagates unchanged from YAML → HTTP → Go client → service → DB.

---

## State dashboard (read this first)

```
┌───────────────────────── SHIPPED on refactor/v1alpha1-types ───────────────────────┐
│  ✔ Types foundation         (pkg/api/v1alpha1)              Groups 1 + 19          │
│  ✔ Generic Store + schema   (internal/registry/database)    Groups 2 + 20          │
│  ✔ HTTP surface             (resource.Register + apply)     Groups 3, 9, 21        │
│  ✔ Seed + adapter iface     (v1alpha1 DeploymentAdapter)    Group 4                │
│  ✔ Importer + scanners      (pkg/importer)                  Groups 5, 7, 21        │
│  ✔ Registry validators      (pkg/api/v1alpha1/registries)   Group 8                │
│  ✔ Service-layer dissolve   (versionutil, URL-uniq)         Group 9                │
│  ✔ Legacy handler collapse  (per-kind handlers gone)        Group 10               │
│  ✔ Legacy importer delete   (−4,400 LOC)                    Group 11               │
│  ✔ Go client rewrite        (352 LOC generic + applyBatch)  Groups 12 + 21         │
│  ✔ Platform adapters port   (local+k8s via v1alpha1)        Groups 13 + 17 + 21    │
│  ✔ MCP registryserver port  (generic addKindTools[T])       Groups 14 + 21         │
│  ✔ Final legacy purge       (−14k LOC, embeddings dropped)  Group 15               │
│  ✔ Embeddings restored      (pgvector + Indexer + SSE)      Group 18               │
│  ✔ Scheme hardened          (Validator split, empty-safe)   Group 19               │
│  ✔ Opaque list cursors      (base64 + 400 on malformed)     Group 20               │
│  ✔ EnvelopeFromRaw hoist    (shared MCP + HTTP)             Group 21               │
│  ✔ Declarative client port  (`709a23d`, deprecated stubs removed) Group 22         │
├──────────────────────── CURRENT CHECKOUT / NEXT UP ─────────────────────────────────┤
│  ✔ Merge origin/main complete                                                       │
│  ✔ ProviderMetadata → annotations landed in current checkout                        │
│  ✔ DatabaseFactory Pool() promotion landed in current checkout                      │
│  ✔ ListOpts.ExtraWhere / ExtraArgs landed in current checkout                       │
│  ⚙ Enterprise E0 revert + redo against the new OSS HEAD                             │
├───────────────────────── PENDING (after merge or separate) ─────────────────────────┤
│  ◆ Workflow CLI envelope cleanup (local manifest compatibility + projection code) │
│  ◆ Phase 2 KRT reconciler rebase (async sibling of V1Alpha1Coordinator)           │
│  ◆ UI TypeScript client regen + component fixups                                  │
│  ◆ Deployment watch/SSE endpoint (logs ported; watch deferred)                    │
│  ✔ Generic README surface (`Spec.Readme` + subresource routes) landed             │
│  ◆ Per-platform deployment identities side table                                  │
└────────────────────────────────────────────────────────────────────────────────────┘
```

**What that means for you as the reviewer**: skim the four main pillars (Types / Store / HTTP / Coordinator) in the reading order below. Everything else is either a consumer of those pillars or a deletion cascade.

---

**Merge state.** Every subsystem that has a live consumer is ported. Legacy `public.*` tables, per-kind services, embeddings stack (since re-built on v1alpha1), legacy importer/exporter/seeder, and every legacy HTTP handler are deleted. `internal/client/client_deprecated.go`, `pkg/models/*.go`, and `internal/registry/kinds/` are gone; what remains is workflow CLI/local-manifest cleanup plus the separate Phase 2 KRT rebase.

**Recommended companion reads** (in order): `REBUILD_TRACKER.md` (per-subsystem port inventory + finish-line signals) → this file (commit-grouped reading order) → `REMAINING.md` (the short list of what's still deferred) → `design-docs/V1ALPHA1_PLATFORM_ADAPTERS.md` + `design-docs/V1ALPHA1_IMPORTER_ENRICHMENT.md` (design rationale; every open question was resolved on 2026-04-17). These files are on disk as untracked working-tree entries (removed from git in `5e64cec` so the PR diff stays code-focused) — share them with reviewers out-of-band.

> **Group-numbering convention.** Section **headers** below (`Group 1..23`) are review chunks structuring the reading order. References to `Group N` inside **prose** point at the 14 port subsystems tracked in `REBUILD_TRACKER.md` (e.g. subsystem Group 5 = platform adapters, subsystem Group 9 = MCP bridge). Context disambiguates.

## Parallel agent activity (2026-04-22)

This branch is being worked on by multiple agents in parallel. Current state:

| Session | Lane | Status |
|---|---|---|
| cd2ac3c6 / **this review guide** | Top-down structural review; landed cleanup commits (Groups 18–23) | Idle, reviewing for merge prep |
| 019db6c7 | Architecture sweep; landed `709a23d` and the local P1/A2 seam follow-up | Done in current checkout |
| dfe7395f | Embeddings restore (Group 21) | **Closed** — lane done |
| 34300023 | Enterprise port on `agentregistry-enterprise/refactor/v1alpha1-port` | Ready to redo E0 against the new OSS seam |
| sub-agent | Merge `origin/main` into this branch | **Closed** — merge complete |

---

## TL;DR architecture

```
┌──────────── YAML / JSON manifest ────────────┐
│  apiVersion: ar.dev/v1alpha1                 │
│  kind: Agent | MCPServer | Skill | Prompt |  │
│        Provider | Deployment                 │
│  metadata: { namespace, name, version,       │
│              labels, annotations,            │
│              finalizers, ... }               │
│  spec:   { <kind-specific typed body> }      │
│  status: { observedGeneration, conditions }  │
└──────────────────────────────────────────────┘
                     │
                     ▼ v1alpha1.Scheme.Decode / DecodeMulti
                     │
  ┌──────────── Object interface ────────────┐
  │   Validate()                 structural   │
  │   ResolveRefs(resolver)      cross-kind   │
  │   ValidateRegistries(v)      OCI/NPM/...  │
  │   ValidateUniqueRemoteURLs   cross-row    │
  │   MarshalSpec()              JSONB bytes  │
  └──────────────────────────────────────────┘
                     │
                     ▼ generic database.Store (one Store per kind)
                     │
  ┌────── v1alpha1.* schema (PostgreSQL) ────┐
  │  agents / mcp_servers / skills /         │
  │  prompts / providers / deployments       │
  │  PK = (namespace, name, version)         │
  │  JSONB spec + status, annotations        │
  │  enrichment_findings side table          │
  └──────────────────────────────────────────┘
                     │
                     ▼ PostUpsert / PostDelete hooks (Deployment only)
                     │
  ┌──── V1Alpha1Coordinator (synchronous) ───┐
  │   resolve TargetRef + ProviderRef         │
  │   dispatch by Provider.Spec.Platform      │
  │   merge conditions → Status via PatchStatus│
  │   merge finalizers via PatchFinalizers    │
  └──────────────────────────────────────────┘
                     │
                     ▼
       local adapter | kubernetes adapter | noop | <enterprise>
```

The coordinator is the synchronous sibling of the Phase 2 KRT reconciler. Same adapter map, same status/finalizer write paths, different trigger (HTTP apply vs. NOTIFY watch).

---

## Key design decisions embedded in code

| Decision | Where it lives |
|----------|----------------|
| `ar.dev/v1alpha1` API version | `pkg/api/v1alpha1/doc.go` |
| Composite PK `(namespace, name, version)` | `migrations_v1alpha1/001_v1alpha1_schema.sql` |
| Soft-delete + finalizers | `ObjectMeta.DeletionTimestamp` + `Store.Delete` + `Store.PurgeFinalized` |
| Server-managed generation, bumps on spec change | `Store.Upsert` (`store_v1alpha1.go:108`) |
| Semver-aware `is_latest_version` | `Store.recomputeLatest` (`:503`) + `pickLatestVersion` (`:560`) |
| Status writes disjoint from spec writes | `Store.PatchStatus` (`:222`) |
| Status-only NOTIFY trigger | `v1alpha1.notify_status_change()` in `001_v1alpha1_schema.sql:40-67` |
| Dedicated PostgreSQL schema | `CREATE SCHEMA v1alpha1` (`001_v1alpha1_schema.sql:20`) |
| Refs are pure `ResourceRef{Kind, Namespace, Name, Version}` | `pkg/api/v1alpha1/ref.go` |
| Validation co-located on Spec types | `{agent,mcpserver,skill,prompt,provider,deployment}_validate.go` |
| Cross-kind ref resolution | `database.NewV1Alpha1Resolver` |
| Cross-row URL-uniqueness invariant | `(Object).ValidateUniqueRemoteURLs` + `database.NewV1Alpha1UniqueRemoteURLsChecker` |
| Multi-doc YAML apply (POST + DELETE) | `resource/apply.go` |
| Apply result wire contract | `pkg/api/v0/apply.go` |
| Annotations (non-indexed) | `ObjectMeta.Annotations`; `annotations` JSONB column on every table |
| Scanner plug-in contract | `pkg/importer/scanner.go` |
| Enrichment findings side table | `migrations_v1alpha1/002_enrichment_findings.sql` |
| Built-in Kind list (stable order) | `pkg/api/v1alpha1/doc.go BuiltinKinds` |
| Kind → table mapping | `internal/registry/database/store_v1alpha1_tables.go V1Alpha1TableFor` |
| One-line Stores constructor | `database.NewV1Alpha1Stores(pool)` |
| Generic `Register[*T]` per kind | `resource.RegisterBuiltins` |
| Shared version-lock helper | `pkg/api/v1alpha1/versionutil.go` |
| Unified apply pipeline | `resource.RegisterApply` (POST + DELETE at `/v0/apply`) |
| Generic Go client | `internal/client/client.go` (kind-agnostic methods) |
| Typed client helpers for workflow CLI | `internal/client/typed.go` + `internal/cli/common/deployments.go` |
| Deployment coordinator | `internal/registry/service/deployment/v1alpha1_coordinator.go` |
| PostUpsert / PostDelete hooks on generic handler | `resource.Config{PostUpsert, PostDelete}` (`handler.go:63-79`) |
| Platform adapter registry | `map[platform]types.DeploymentAdapter` built at bootstrap, keyed on `Provider.Spec.Platform` |
| Shared v1alpha1 translate helpers | `internal/registry/platforms/utils/v1alpha1_helpers.go` |
| Deployment logs endpoint | `resource.RegisterDeploymentLogs` |

---

## Reading order

Commits are grouped logically. Each group lists its commits and the ~3-5 files to read. Doc-only commits are omitted from the tables (they're noted by date in git log if you want the paper trail).

### Group 1 — Types foundation (`d54c8ef..81e732e`, 6 code commits)

The typed envelope and the interfaces that everything else binds to.

| Commit | Subject |
|--------|---------|
| `d54c8ef` | v1alpha1 Kubernetes-style envelope types (Agent/MCPServer/Skill/Prompt/Provider/Deployment + Scheme + Conditions) |
| `6715813` | drop Status.Phase in favor of Conditions |
| `f1771c4` | design review feedback: `TemplateRef` → `TargetRef`; drop `UID` |
| `8237dec` | add Namespace + DeletionTimestamp + Finalizers to ObjectMeta |
| `b7d10c3` | add ObjectMeta.Annotations |
| `81e732e` | add `Validate()` + `ResolveRefs()` to Object interface |

**What to pay attention to**
- `pkg/api/v1alpha1/object.go` — `ObjectMeta` shape. Every field is deliberate; Generation / CreatedAt / UpdatedAt / DeletionTimestamp are explicitly server-managed (see doc comment). The shape every client + wire payload commits to.
- `pkg/api/v1alpha1/scheme.go` — `Decode` / `DecodeMulti` / `Register`. Enterprise plugs in additional kinds via `Scheme.Register`. Verify this surface is stable.
- `pkg/api/v1alpha1/validation.go` — regexes (`nameRegex`, `namespaceRegex`, `labelKeyRegex`), reserved version literal `"latest"`, URL policy (https-only for websiteUrl). Policy choices affecting every manifest.
- `pkg/api/v1alpha1/ref.go` — `ResourceRef{Kind, Namespace, Name, Version}`. Blank namespace = "inherit from referrer"; blank version = "latest". Inheritance happens in each kind's `ResolveRefs`.
- `pkg/api/v1alpha1/accessors.go` — every kind gets `GetMetadata / SetMetadata / GetStatus / SetStatus / MarshalSpec / UnmarshalSpec` so generic code treats them uniformly.

### Group 2 — Database (`7ab5a2a`, `a45a439`, 2 commits)

Generic Store + schema.

| Commit | Subject |
|--------|---------|
| `7ab5a2a` | generic Store + schema |
| `a45a439` | isolate v1alpha1 tables in dedicated PostgreSQL schema |

**What to pay attention to**
- `store_v1alpha1.go:108` `Upsert` — generation bump comes from `oldSpec` vs `newSpec`; `opts.Finalizers == nil` preserves, empty slice clears; same for `Annotations`. `recomputeLatest` runs inside the same transaction so `is_latest_version` is never transiently inconsistent.
- `store_v1alpha1.go:222` `PatchStatus` — read → mutate callback → write; never touches generation or spec. **Only path reconcilers and the coordinator use for status writes.** Phase 2 KRT depends on this signature staying stable.
- `store_v1alpha1.go:266` `PatchFinalizers` — same pattern for finalizer list mutation.
- `store_v1alpha1.go:345` `Delete` — soft. Sets `deletion_timestamp` and re-runs `recomputeLatest`. Callers with finalizers see the terminating row until `PurgeFinalized` runs.
- `store_v1alpha1.go:466` `FindReferrers` — GIN-indexed JSONB containment lookup. Powers both cross-kind ref resolution and URL-uniqueness.
- `store_v1alpha1.go:560` `pickLatestVersion` — semver-aware with a fallback to "most-recently-updated" when none of the versions parse. Deliberate (see decisions table).
- `migrations_v1alpha1/001_v1alpha1_schema.sql` — the whole file. Check column types, indexes (GIN on `spec jsonb_path_ops`, partial unique on `is_latest_version`), and the `notify_status_change` trigger payload `{"op":"INSERT|UPDATE|DELETE","id":"<ns>/<name>/<version>"}` (lines 40-67). **Hard seam** for Phase 2 KRT.

### Group 3 — HTTP surface (`ec84636`, `5b2f3a4`, `f3de384`, 3 commits)

Generic resource handler + router wiring + multi-doc apply.

| Commit | Subject |
|--------|---------|
| `ec84636` | generic resource handler (`Register[T v1alpha1.Object]`) |
| `5b2f3a4` | wire v1alpha1 routes at `/v0` |
| `f3de384` | multi-doc YAML batch apply at POST `/v0/apply` |

**What to pay attention to**
- `handler.go:34-80` `Config` struct — the seven knobs reviewers should touch: `Kind`, `PluralKind`, `BasePrefix`, `Store`, `Resolver`, `RegistryValidator`, `UniqueRemoteURLsChecker`, `PostUpsert`, `PostDelete`. The last two are Deployment's only integration point with the adapter stack; everything else the handler does is kind-agnostic.
- `handler.go` `Register[T]` — type parameter constrained to `v1alpha1.Object`. Each endpoint specialized at compile time; no reflection.
- `apply.go` `applyOne` + `prepareApplyDoc` — apply-pipeline order: namespace default → Validate → ResolveRefs → ValidateRegistries → ValidateUniqueRemoteURLs → (Upsert | DryRun short-circuit). Trace one document through.
- `apply.go:65-66` — `DryRun` runs the full pipeline and short-circuits before Upsert; `Force` is accepted for wire compat and is a no-op (version-lock not enforced on apply).
- `apply.go:104-111` — DELETE `/v0/apply` runs validation then `Store.Delete`. Missing rows return `Status="failed"` rather than silent success.
- `router/v0.go` — 136 LOC total. The entire route surface in one file.

### Group 4 — Seed + adapter interface (`ee07520`, `4a6e1a6`, 2 commits)

Small but load-bearing.

| Commit | Subject |
|--------|---------|
| `ee07520` | v1alpha1 builtin MCPServer seeder |
| `4a6e1a6` | v1alpha1 DeploymentAdapter interface + noop reference |

**What to pay attention to**
- `pkg/types/adapter_v1alpha1.go` — `DeploymentAdapter` + `ProviderAdapter` interfaces. `ApplyResult.AddFinalizers` lets adapters declare finalizer tokens they own; coordinator calls `Store.PatchFinalizers`. `ProviderMetadata` is `map[string]string` destined for `ObjectMeta.Annotations`.
- `platforms/noop/adapter.go` — narrowest possible implementation. Any real adapter satisfies the same surface.
- `internal/registry/seed/v1alpha1.go` — reads `seed.json` and writes via the generic Store. Uses the raw pgxpool; noop / DatabaseFactory backends skip seeding.

### Group 5 — Importer pipeline + scanners (`64ff130..2241012`, 9 code commits)

Scanner interface + OSV + Scorecard, ported via `git mv`.

| Commit | Subject |
|--------|---------|
| `64ff130` | Scanner interface + FindingsStore + enrichment schema |
| `e2e6f8f` | importer core pipeline (decode + validate + enrich + upsert) |
| `0ec9297` | `git mv osv_scan.go → pkg/importer/scanners/osv/` (70% similarity) |
| `d2ba661` | wrap OSV in Scanner interface |
| `69eaf58` | OSV endpoint-override Config for unit tests |
| `4ad509c` | OSV unit tests |
| `4e2afd7` | `git mv scorecard_lib.go → pkg/importer/scanners/scorecard/` (99% similarity) |
| `3f65d84` | wrap Scorecard in Scanner interface |
| `a44cad1` | Scorecard unit tests |

**Git-mv-first review pattern.** The `git mv` commits are pure renames with only the package declaration edited. `git log --follow pkg/importer/scanners/osv/osv.go` traces back through `0ec9297` into the original PR that authored the logic.

1. Read the rename commit's diff — small.
2. Read the "wrap in Scanner interface" commit next — new code on top of ported helpers.
3. Read the test commit last.

**What to pay attention to**
- `pkg/importer/scanner.go` — `Scanner` interface + `EnrichmentPrefix` vocabulary (AnnoOSVStatus, AnnoScorecardBucket, etc.). String constants define the on-wire enrichment contract.
- `pkg/importer/importer.go` `importOne` — decode + namespace default + validate + resolve refs + run scanners + upsert + write findings. One function is the whole pipeline.
- `pkg/importer/importer.go` `runScanners` — scanner errors are **isolated**: one bad scanner downgrades `EnrichmentStatus` to `partial` but the import still upserts. Critical behavior.
- `pkg/importer/findings_store.go` `Replace` — atomic DELETE + INSERT per `(object, source)` inside a single transaction.
- `pkg/importer/scanners/osv/osv.go` — three preserved-verbatim helpers (`parseNPMLockForOSV`, `parsePipRequirementsForOSV`, `parseGoModForOSV`) plus a new `Scanner` wrapper. `queryOSVBatchDetailed` is a sibling of legacy `queryOSVBatch` returning per-CVE severity.
- `pkg/importer/scanners/scorecard/scorecard.go` — `runFunc` test hook (unexported) lets unit tests fake the engine without hitting GitHub.

### Group 6 — Deduplication pass (`d31a56e`, `80c903a`, 2 commits)

Structural duplication collapse. No behavior change.

| Commit | Subject |
|--------|---------|
| `d31a56e` | collapse `V1Alpha1Stores` duplication — one source of truth for kind→store |
| `80c903a` | share `GitHubRepoFor` across OSV + Scorecard |

**What to pay attention to**
- `pkg/api/v1alpha1/doc.go BuiltinKinds` — the stable ordered slice. Adding a built-in kind means editing this + `V1Alpha1TableFor` + one `case` in `RegisterBuiltins`'s switch.
- `internal/registry/api/handlers/v0/resource/builtins.go` — the six `Register[*v1alpha1.X]` generic calls. Localized here instead of scattered.
- `router/v0.go registerV1Alpha1Routes` — one `RegisterBuiltins` + one `RegisterApply` + one optional `RegisterDeploymentLogs` (when coordinator is wired).
- `pkg/importer/githubrepo.go` — exported `GitHubRepoFor(obj)` handles Agent, MCPServer, Skill. Any new GitHub-dependent scanner imports it.

### Group 7 — Importer bootstrap + POST /v0/import (`8d13ee4`, `20abd73`, 2 commits)

Closes the server-side half of the importer subsystem.

| Commit | Subject |
|--------|---------|
| `8d13ee4` | wire v1alpha1 Importer + POST `/v0/import` |
| `20abd73` | route `cfg.SeedFrom` through the v1alpha1 Importer |

**What to pay attention to**
- `pkg/importer/importer.go ImportBytes` — decode + loop shared with `Import` via `importStream`; source string labels records for debugging.
- `handlers/v0/resource/import.go` — nil Importer skips route registration. Query params mirror `importer.Options` (namespace, enrich, scans, dryRun, scannedBy).
- `database/store_v1alpha1_tables.go NewV1Alpha1Resolver` — one `ResolverFunc` factory; router, apply handler, and Importer all call it.

### Group 8 — Registry validators port (`4f2f212`, `a6ea729`, `020e2bf`, 3 commits)

Legacy per-registry validators moved to `pkg/api/v1alpha1/registries/` via `git mv` (86-100% rename similarity).

| Commit | Subject |
|--------|---------|
| `4f2f212` | OCI validator → v1alpha1 |
| `a6ea729` | NPM / PyPI / NuGet / MCPB validators → v1alpha1 |
| `020e2bf` | wire `registries.Dispatcher` into apply + import |

**What to pay attention to**
- `pkg/api/v1alpha1/registry_validate.go` — `RegistryPackage` shape + per-kind `ValidateRegistries` + `validatePackages` helper accumulating FieldErrors.
- `pkg/api/v1alpha1/registries/dispatcher.go` — switch routes `RegistryType` to the per-registry validator. Add a `case` here for a new registry type.

### Group 9 — Service-layer hoists (`2972363`, `e9ef6fb`, `2f667eb`, 3 commits)

Business logic moves off legacy service packages onto the v1alpha1 surface.

| Commit | Subject |
|--------|---------|
| `2972363` | hoist versionutil to `pkg/api/v1alpha1` (72% / 97% similarity) |
| `e9ef6fb` | port URL-uniqueness to Object interface |
| `2f667eb` | consolidate `/v0/apply` onto v1alpha1 resource handler (deletes legacy apply handler + common errors package) |

**What to pay attention to**
- `pkg/api/v1alpha1/versionutil.go` — two exported functions. `CompareVersions` is the authority for `isLatest`; apply-path version-locking is explicitly **not** enforced.
- `pkg/api/v1alpha1/remote_urls_validate.go` — within a single manifest, only the first conflicting URL is reported; across manifests, `Store.FindReferrers` does the work via GIN-indexed JSONB containment. Cross-namespace by design.
- `handlers/v0/resource/apply.go` — `DryRun` short-circuits before Upsert; DELETE verb soft-deletes every doc in the body.
- `pkg/api/v0/apply.go` — public wire contract shared by the server, Go client, and CLI.

### Group 10 — Legacy HTTP handler collapse (`8be4067`, `a7ab3a4`, 2 commits)

The big deletion. V1alpha1 routes have been serving equivalent traffic since `5b2f3a4`; legacy per-kind handlers had no live clients. **−3,558 LOC.**

| Commit | Subject |
|--------|---------|
| `8be4067` | delete legacy per-kind handlers (agents/skills/prompts/providers) |
| `a7ab3a4` | delete legacy servers handler + deploymentmeta |

Afterward, `router/v0.go` imports only `resource`, `health`, `ping`, `version` — exactly what the final form ships.

### Group 11 — Legacy importer/exporter/builtin-seed/CLI deletion (`048619f`, 1 commit)

**−4,400 LOC.** Removes `internal/registry/importer/` (1,535 LOC), `internal/registry/exporter/`, legacy `seed/{builtin,readme}.go + seed-readme.json` (1,196 LOC JSON), `internal/cli/{import,export}.go`. Bootstrap simplifies: `runSeedFromImport` takes only `(cfg, v1alpha1Importer)` — no legacy fallback.

### Group 12 — Go client rewrite + prompt service (`20b25d4`, `5aa8339`, follow-up `709a23d`)

| Commit | Subject |
|--------|---------|
| `20b25d4` | delete prompt service + `fake_registry.go` test shim |
| `5aa8339` | rewrite Go client as generic v1alpha1 methods |

**What to pay attention to**
- `internal/client/client.go` is the generic v1alpha1 surface: `Get / GetLatest / List / Apply / DeleteViaApply / Delete / PatchStatus`. Preserved behavior: bearer token, configurable `http.Client`, `404 → ErrNotFound`.
- `709a23d` deleted `internal/client/client_deprecated.go` and moved the remaining declarative provider/deployment callers onto `internal/client/typed.go` + `internal/cli/common/deployments.go`.
- `client_v1alpha1_test.go` is the round-trip test replacing the deleted declarative integration test.

### Group 13 — Platform adapter port + deployment coordinator (`3ea8bc2..7191848`, 7 commits)

The biggest subsystem port. Sub-commit labels `5.a..5.f` in subjects match `REBUILD_TRACKER.md §5`.

| Commit | Subject |
|--------|---------|
| `3ea8bc2` | port local platform adapter to v1alpha1 (5.a) |
| `c4bba08` | port kubernetes platform adapter to v1alpha1 (5.b) |
| `8d4c3ea` | share v1alpha1 translate helpers (5.c) |
| `7d02e9f` | v1alpha1 coordinator first cut (5.d) |
| `4be00e9` | wire coordinator into /v0 + register deployment-logs endpoint (5.e) |
| `2c2ff4e` | **delete legacy deployment surface end-to-end (5.f) — −8,878 LOC** |
| `7191848` | trim `platforms/utils/deployment_adapter_utils.go` to surviving v1alpha1 surface (−625 LOC; file kept — still consumed by `internal/cli/*` + native adapters) |

**What to pay attention to**
- `service/deployment/v1alpha1_coordinator.go:34-70` — `V1Alpha1Coordinator` responsibilities: (1) resolve TargetRef + ProviderRef via the supplied `GetterFunc`; (2) dispatch by `Provider.Spec.Platform`; (3) merge adapter-returned Conditions into `Status` via `PatchStatus`; (4) merge adapter-returned finalizer tokens via `PatchFinalizers`. **Explicitly NOT responsible for the Upsert** — apply handler owns that. This is the seam Phase 2 KRT slots into.
- `handler.go:63-79` `Config.PostUpsert` + `Config.PostDelete` — first-class hooks on the generic handler. Deployment wires `coord.Apply` to `PostUpsert` and `coord.Remove` to `PostDelete`. Kind-agnostic; any future Kind that needs post-persist reconciliation uses the same shape.
- `router/v0.go:109-113` — where the hooks get wired, and why they're `nil` for non-Deployment kinds.
- `deployment_logs.go` — `GET .../deployments/{ns}/{name}/{version}/logs` streams via `coord.Logs` → `adapter.Logs`. `follow` + `tailLines` query flags.
- **Cancel is not a separate endpoint.** By design: `DesiredState="undeployed"` on apply + DELETE handle what cancel used to. See `design-docs/V1ALPHA1_PLATFORM_ADAPTERS.md` for the decision.
- `platforms/utils/v1alpha1_helpers.go` (349 LOC) + `deployment_adapter_utils.go` (460 LOC) — port allocation, env merge, DNS-1123 sanitization, `TranslateMCPServer`, `GenerateInternalNameForDeployment`, `BuildRemoteMCPURL`. Consumed by both native adapters **and** `internal/cli/agent/utils` + `internal/cli/mcp` — the CLI shares translation code with the server.
- `platforms/{local,kubernetes}/v1alpha1_adapter.go` — paired files; each implements `Apply / Remove / Logs / Discover`.

### Group 14 — MCP registryserver port (`bd9410c`, 1 commit)

Tools Claude Code consumes get rewritten to speak v1alpha1. Tool names preserved — breaking them breaks every saved MCP config.

| Commit | Subject |
|--------|---------|
| `bd9410c` | port registryserver to v1alpha1 stores (subsystem Group 9) |

**What to pay attention to**
- `server.go` shrinks 590 → 263 LOC. `list_servers` + `get_server` now go through `Store.List` / `Store.Get`.
- **Name-contains filtering is server-side in Go**, iterating over `Store.List` pages. Fine at current catalog size; flag if the filter moves to a hot path.
- Deferred intentionally: `updated_since` filter only. Semantic search and README fetch are both live on the current checkout.
- `registry_app.go` wires the MCP bridge only when v1alpha1 Stores exist; noop-backend tests never had MCP anyway.

### Group 15 — Final legacy purge (`2cbf1c2`, `df5986c`, `58401c7`, 3 commits)

~14k LOC of legacy deleted once every caller had been ported.

| Commit | Subject | LOC |
|--------|---------|-----|
| `2cbf1c2` | dissolve per-kind services + embeddings stack | −4,968 |
| `df5986c` | delete legacy DB stores + `public.*` migrations | −5,988 |
| `58401c7` | cruft sweep (legacy `validators.go` + related) | −3,050 |

**What to pay attention to**
- `internal/registry/database/migrations/` **does not exist**. Only `migrations_v1alpha1/` remains. Migrator config shrank accordingly.
- **Embeddings is restored** on v1alpha1 in the current checkout. The remaining work is auto-regen / SSE polish / extra providers, not restoring the core feature.
- `pkg/registry/database/database.go` is now the thin root contract (`Pool()` + `Close()`) plus sentinel errors.
- **`pkg/models/*.go` and `internal/registry/kinds/` are gone.** Workflow CLI still has its own internal manifest projection types, but the registry-side DTO stack is deleted.
- `internal/registry/validators/` — only the registries dispatcher + legacy per-kind `ValidatePackage` adapter remain. The 3k-LOC `validators.go` family is gone.

### Group 16 — Design docs + tracker (`d21b566`, `f575541`, `c0a89e2`, et al.)

Paper trail. Reference documents, not code. Every file in this group is on disk as an **untracked** working-tree entry — commit `5e64cec` removed them from git so the PR diff stays code-only. Share them with reviewers out-of-band.

- `REBUILD_TRACKER.md` — per-subsystem port inventory.
- `REMAINING.md` — short punch list of what's deferred + verification gate.
- `design-docs/V1ALPHA1_PLATFORM_ADAPTERS.md` — 8 open questions, all resolved 2026-04-17.
- `design-docs/V1ALPHA1_IMPORTER_ENRICHMENT.md` — 8 open questions, all resolved.
- `DESIGN_COMMENTS_2.md` — @shashankram review comments that seeded several decisions.

---

### Group 17 — Platforms interop trim (`b9f4d3f`, 1 commit)

Small but architecturally-loaded. `platforms/utils` had been importing `apiv0.ServerJSON` (upstream MCP registry type) in `TranslateMCPServer` / `GetRegistryConfig` — leakage of the type layer the refactor explicitly killed. `b9f4d3f` rewrote both helpers to operate on `v1alpha1.MCPServerSpec` natively. After this commit, nothing under `platforms/` imports upstream registry code. Paired with the embeddings restore lane because both shared the "v1alpha1-everywhere" directive that drove the sprint.

### Group 18 — Embeddings restored on v1alpha1 (`b112860..c440525`, 5 commits)

Group 15 (`2cbf1c2`) deleted the whole embeddings stack. The data model (pgvector + indexer + Provider + SSE endpoints) gets rebuilt from scratch on v1alpha1 here, in a clean top-down order. Not a port — green-field.

| Commit | Subject |
|--------|---------|
| `b112860` | add v1alpha1 semantic_embedding columns (pgvector) via additive migration `003_embeddings.sql` |
| `34d72fb` | generic Store gains `SetEmbedding`, `GetEmbeddingMetadata`, `SemanticList` |
| `86a5482` | restore `embeddings.Provider` + OpenAI impl + `EmbeddingsConfig` on v1alpha1 |
| `5be3de1` | v1alpha1-native `Indexer` + resurrected `jobs.Manager` |
| `c440525` | HTTP endpoints + `?semantic=` list + bootstrap wiring |

**What to pay attention to**
- `pkg/semantic/types.go` — `SemanticEmbedding`, `SemanticEmbeddingMetadata`, `SemanticResult`. Intentionally **not** in `pkg/api/v1alpha1` — these are indexer/storage concerns, not API envelope shape. `SemanticResult` holds `*v1alpha1.RawObject`; reverse import would be a smell.
- `internal/registry/database/store_v1alpha1_embeddings.go` — per-kind embedding persistence. Provider / model / dimensions / checksum stamped atomically alongside the vector so later passes can skip unchanged rows.
- `internal/registry/embeddings/indexer.go` — generation-bump NOTIFY subscriber + manual reindex endpoint. Progress streamed via SSE backed by `jobs.Manager`.
- `internal/registry/api/handlers/v0/embeddings/handlers.go` — `POST /v0/embeddings/index` with SSE body.
- **Schema dim fixed at 1536**: OpenAI `text-embedding-3-small`. Non-1536 providers need an additive migration (tracked in `REMAINING.md` Group 8). Open question documented in `REMAINING.md`.
- **Provider interface is ready for Azure OpenAI / Ollama / local** — non-OpenAI providers are a follow-up, not blocked.

### Group 19 — Scheme hardening + Validator split + embeddings relocation (`faa6fe4`, 1 commit)

Post-review tightening of Layer 1. Three distinct wins in one commit.

| Change | Reason |
|--------|--------|
| `splitYAMLDocs` empty-input panic fix | Index-out-of-range on whitespace-only input; `bytes.TrimSpace(data)[0]` now guarded. |
| `Scheme.DecodeInto` kind-check | Previously silently ignored kind mismatch (`DecodeInto(agentYAML, &MCPServer{})` succeeded and filled overlapping fields). Now validates `apiVersion` + `kind` against the destination. |
| Validator interface split | `Object` interface loses `Validate` / `ResolveRefs` / `ValidateRegistries` / `ValidateUniqueRemoteURLs` — those move to a separate `Validator` interface that embeds `Object`. Generic code that only reads/writes envelope shape (CLI `get`, Store, client) no longer has to implement or type-assert 4 no-op methods on kinds that don't validate (Prompt / Provider / Deployment). |
| Embeddings types relocated | `pkg/api/v1alpha1/embeddings.go` → `pkg/semantic/types.go`. API package stays minimal; indexer types own their own home. |
| `PluralFor` cleanup | Hand-rolled `ToLower` + `"s"` byte loop replaced with `strings.ToLower(kind) + "s"`. |

One intentional non-change: accessor embedding collapse was attempted, rolled back. Go's JSON body reflection through Huma doesn't promote fields from an anonymous embedded struct; Request body schema broke. The 60 LOC of explicit per-kind accessors in `accessors.go` stays.

### Group 20 — Store pagination — opaque cursors (`6a011ae`, 1 commit)

`ListOpts.Cursor` was documented as supported but `Store.List` returned a placeholder `"more"` token that never decoded. Fix: base64-encoded JSON cursor carrying `(UpdatedAt, Namespace, Name, Version)` as stable tie-breakers. Malformed cursors surface as `ErrInvalidCursor`; HTTP handler maps that to 400 rather than 500.

**What to pay attention to**
- `store_v1alpha1.go` `encodeCursor` / `decodeCursor` — the canonical format. If a future List filter changes the sort tie-breakers, bump the cursor version field.
- `handler.go runList` — `errors.Is(err, database.ErrInvalidCursor)` → 400 with a stable message.

### Group 21 — HTTP / apply / client / importer / MCP structural cleanup (`1b48a8d..6f910f2`, 6 commits)

Post-review tidy pass on every layer the HTTP surface touches. No behavior change per commit; net ~−100 LOC.

| Commit | Scope |
|--------|-------|
| `1b48a8d` | handler.go dead `json.RawMessage` sentinel; `ApplyResult`/`ApplyStatus*` re-exports dropped (tests import `apitypes` directly); `dryRunKey{}` context smuggle → explicit `dryRun bool`; `prepareApplyDoc` 4-tuple → `preparedDoc{Result,Ready,Store,Meta}`; `importResultWire` dropped (JSON tags hoisted onto `importer.ImportResult`); `RegisterRoutes` early-return; client `DefaultBaseURL` redundancy collapsed |
| `24b88c5` | importer `sliceContains` trampoline deleted; `ImportBytes` no longer silently defaults `ScannedBy` to `"importer-cli"` — HTTP callers always set it, other callers must now too |
| `a3bfd19` | MCP `addAgentTools / addServerTools / addSkillTools / addDeploymentTools` (4 × 25 LOC near-identical) collapsed into one generic `addKindTools[T v1alpha1.Object]` driven by a `kindTools[T]` config struct; tool names preserved (user-facing in Claude) |
| `43354da` | `platforms/{local,kubernetes}/deployment_adapter_*.go` → `adapter.go` / `ops.go` / `platform.go` (pure `git mv`, behavior unchanged). `deployment_adapter_` prefix was vestigial from when legacy v0 adapter interface lived there — it doesn't anymore. `git log --follow` traces blame through the renames. |
| `58ca7d6` | `envelopeFromRow` had two identical copies (HTTP resource handler + MCP bridge). Hoisted to `v1alpha1.EnvelopeFromRaw[T]` so every API surface returns an identically-shaped envelope. |
| `6f910f2` | client `applyBatch` routed through shared `newRequestWithBody` + `doJSON`. Was the only client method not using them — inconsistent error mapping (no `ErrNotFound`, raw body on 4xx). Now consistent. Added `newRequestWithBody(method, path, body, contentType)` helper; `newRequest` delegates with nil/"". |

**What to pay attention to**
- `handler.go` diff between `1b48a8d` and `6f910f2` — it's the core generic handler surface. Every caller hangs off `resource.Register[T]`.
- `apply.go` `preparedDoc` shape — clean alternative to `(result, store, meta, ok)` Go-ism. Pattern worth applying wherever 3+ return values mix with a success flag.
- `43354da` is pure `git mv`. If you're reading blame on `platforms/local/platform.go` or `platforms/kubernetes/platform.go`, follow through the rename.

### Group 22 — In flight: P1s + RBAC filter hook (019db6c7)

These three seams are now present in the current checkout (not yet called out in a dedicated commit in this guide's original review order).

| Landed seam | Scope | Current state |
|-------------|-------|---------------|
| **P1-1** | `V1Alpha1Coordinator.persistApplyResult` merges `ApplyResult.ProviderMetadata` into `Deployment.Metadata.Annotations` via a new atomic `Store.PatchAnnotations` helper. Integration tests cover metadata persistence and preservation of existing annotations. | Landed in current checkout |
| **P1-2** | `pkg/registry/database.Store` now exposes `Pool() *pgxpool.Pool`; ad-hoc `interface{ Pool() *pgxpool.Pool }` assertions are gone from `registry_app.go`, and v1alpha1 wiring gates on `db.Pool() != nil`. | Landed in current checkout |
| **A2** (bundled) | `database.ListOpts` now includes `ExtraWhere string` + `ExtraArgs []any`, with placeholder rebasing so caller fragments can remain `$1..$N` relative to their own args. | Landed in current checkout |

These seams are the last OSS-side blockers that the enterprise port plan was waiting on. The next move is the enterprise E0 redo against this newer OSS baseline.

---

## Attention areas (push hardest here)

Ranked by blast radius.

1. **`ObjectMeta` shape (`object.go`).** Every wire payload + row + client carries this. Push: are `CreatedAt`/`UpdatedAt` exposed intentionally? Should `DeletionTimestamp` be a subresource? Is `Finalizers` on the main wire a good idea?

2. **`notify_status_change` trigger payload (`001_v1alpha1_schema.sql:39-68`).** Locked: `{"op":"INSERT|UPDATE|DELETE","id":"<ns>/<name>/<version>"}`. Phase 2 KRT depends on this. Any change is a breaking wire change.

3. **Coordinator contract (`v1alpha1_coordinator.go:34-70`).** The explicit non-responsibility ("coordinator does NOT Upsert") is the seam KRT slots into. If anyone routes Upserts through the coordinator we lose that seam. Push on whether `PatchStatus` + `PatchFinalizers` are the only writes the coordinator owns.

4. **Platform adapter registry keying.** `map[platform-string]DeploymentAdapter` keyed verbatim on `Provider.Spec.Platform`. Misspelling on either side routes to the unsupported-platform error silently. Consider validating against the registered-adapter set at apply time.

5. **Semver `is_latest_version` rule (`pickLatestVersion`).** Decides which row `GetLatest` returns. Fallback to `updated_at DESC` when semver fails is deliberate — sanity-check whether silent fallback hides bugs.

6. **Validation rules (`validation.go`).** Regexes + reserved version literal `"latest"` + https-only URL policy reject manifests at the API boundary; if they're wrong, real users see errors.

7. **Cross-row URL-uniqueness.** `Store.FindReferrers` with JSONB containment `{"remotes":[{"url":"..."}]}` as the gate. Cross-namespace by design. Verify this matches the legacy behavior it replaced; verify the JSONB containment doesn't match partial URLs or transformations.

8. **Apply pipeline order (`prepareApplyDoc`).** namespace default → Validate → ResolveRefs → ValidateRegistries → ValidateUniqueRemoteURLs → Upsert → (Deployment only) PostUpsert. Any reorder changes which error class surfaces first.

9. **PostUpsert / PostDelete error surface.** Hook errors surface as 500 because the row is already persisted. A failure here = degraded state the caller should retry. Confirm the coordinator's own error paths are idempotent under retry.

10. **Scanner failure isolation (`runScanners`).** One flaky scanner must not block an import. Read `EnrichmentStatus` transitions carefully.

11. **`git mv` rename detection.** OSV `0ec9297` (70%), Scorecard `4e2afd7` (99%), versionutil `2972363` (72%/97%), OCI `4f2f212`, NPM/PyPI/NuGet/MCPB `a6ea729` (86-100%), platform-adapter utils `8d4c3ea`. `git log --follow` traces original authorship everywhere. To verify logic wasn't disturbed, diff legacy vs. post-wrap.

12. **Workflow CLI residue.** Registry-side DTOs are gone, but local workflow manifests still carry a separate internal projection plus dual-format loading. Push on whether flat manifest compatibility still deserves to exist.

13. **MCP server-side substring filter.** `registryserver/server.go` does name-contains filtering in Go over paged `Store.List` results. Fine at current catalog size; risks latency at scale. Flag if the filter moves to a hot path before a `NameContains` option lands on `Store.List`.

---

## Adding a new built-in kind

The path is mechanical:

1. **`pkg/api/v1alpha1/`** — author the envelope, Spec, validator, accessor methods; register in `Scheme` via `MustRegister` in `scheme.go newDefaultScheme`.
2. **`pkg/api/v1alpha1/doc.go`** — append the Kind const to `BuiltinKinds`.
3. **`internal/registry/database/store_v1alpha1_tables.go`** — add the Kind → table entry to `V1Alpha1TableFor`.
4. **`internal/registry/api/handlers/v0/resource/builtins.go`** — add a `case` in `RegisterBuiltins`'s switch so the `Register[*NewKind]` generic call is emitted.
5. **`internal/registry/database/migrations_v1alpha1/`** — add a migration creating the backing table (copy any existing table's shape).

Everything else — router wiring, apply endpoint, resolver, `NewV1Alpha1Stores` — picks up the new kind automatically. If the kind needs post-persist reconciliation, set `Config.PostUpsert` / `Config.PostDelete` in the builtins switch.

Enterprise builds adding proprietary kinds: call `v1alpha1.Scheme.Register(...)` at init, extend the Stores map before passing it in, and call `resource.Register[*YourKind]` in their own setup. No patches to OSS files.

---

## Decisions worth calling out explicitly

Flag anything you disagree with.

- **No backwards-compat shims at the end.** Every legacy subsystem was deleted in the same PR as its port finish-line. ~36k LOC of legacy removed across Groups 10–15.
- **Apply = publish.** No separate publish verb. Applying creates the row and sets `is_latest_version` in one transaction.
- **Pure JSONB + GIN for reverse lookups.** No promoted columns for "agents referencing MCPServer X". GIN-indexed `spec @>` carries the weight — that's also how URL-uniqueness works.
- **Annotations not indexed.** Labels are queryable; annotations are narrative. Three enrichment keys (`osv-status`, `scorecard-bucket`, `last-scanned-stale`) are promoted to both — see `PromotedToLabels` in `pkg/importer/scanner.go`.
- **Status.Phase dropped.** Conditions are the only reconciliation-state surface.
- **Refs are pure.** No inline fields coexisting with ref-style. Anywhere a ref can go, it's `ResourceRef{Kind, Namespace, Name, Version}`.
- **Deployments have the same PK shape.** No UUID; `(namespace, name, version)`.
- **Apply-path version-lock not enforced.** Legacy only used `CompareVersions` to decide `isLatest`, never to reject old-version applies. `Store.Upsert` + `pickLatestVersion` preserve that semantic.
- **Synchronous coordinator + KRT seam.** `V1Alpha1Coordinator` is synchronous; KRT is the async sibling. Same adapter contract, same `PatchStatus`/`PatchFinalizers` writes, different trigger. Coordinator explicitly does NOT Upsert so KRT can replace it without touching the apply handler.
- **PostUpsert / PostDelete as kind-agnostic hooks.** Deployment isn't special in the handler — it just sets two `Config` fields. Any future kind wanting post-persist reconciliation uses the same pattern.
- **Deployment cancel subsumed.** `DesiredState=undeployed` on apply + DELETE replace the legacy cancel endpoint.
- **Embeddings is green-field, not deferred.** Deleted wholesale in `2cbf1c2`. Re-implementation is additive new work, not a port.
- **Temporary deprecated-stub pattern is over.** `client_deprecated.go` existed only for the handoff window and was deleted in `709a23d`; the remaining cleanup is workflow CLI/local-manifest cleanup, not registry DTO deletion.
- **README generalized.** Long-form docs now live on shared `Spec.Readme` fields, list responses strip the heavy body, and generic readme subresource routes serve lazy-loaded content. A thin MCP-server alias remains for existing UI consumers.

---

## How to run tests locally

```bash
make test-unit                                          # 1066 tests, no infra
make test                                               # integration, needs Postgres on :5432
go test -tags=unit ./pkg/api/v1alpha1/...               # envelope + validation
go test -tags=unit ./pkg/importer/scanners/...          # scanner unit tests
go test -tags=integration ./pkg/importer/...            # importer integration
go test -tags=integration ./internal/registry/database/...  # store + schema
go test -tags=integration ./internal/registry/api/handlers/v0/resource/...  # HTTP + DeploymentHooks
go test -tags=integration ./internal/client/...         # generic client round-trip
go test -tags=integration ./internal/registry/service/deployment/...  # coordinator
go test -tags=integration ./internal/registry/platforms/...  # local + kubernetes adapters
```

All of these are currently green.

---

## What's NOT in this PR (deferred)

Short list. See `REMAINING.md` for the full version.

- **Remaining workflow CLI cleanup.** Local manifest compatibility and duplicated internal projection code still need their final cleanup pass.
- **Phase 2 KRT reconciler rebase.** Async sibling of `V1Alpha1Coordinator`. Same adapter contract, same `PatchStatus` / `PatchFinalizers` writes, triggered by the status NOTIFY stream instead of HTTP apply.
- **UI TypeScript client regen + component fixups.** Blocked on Go client finishing — the Go client is already at 352 LOC, so this just needs scheduling.
- **Deployment watch / SSE endpoint.** Logs IS ported; watch is not. Open question whether to add it onto the coordinator or let KRT's NOTIFY subscription serve it.
- **Embeddings polish.** Core pgvector/indexer is back; remaining work is NOTIFY auto-regen, SSE job streaming, and extra providers.
