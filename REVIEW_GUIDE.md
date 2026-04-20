# v1alpha1 API Refactor — Review Guide

Branch: `refactor/v1alpha1-types`
Scope: 47 commits, ~12.3k LOC added / ~12.9k deleted across 137 files — **net negative** now that Groups 1 (HTTP handler collapse), 2 (Go client rewrite), and most of 7 (legacy importer/exporter/builtin-seed deletion) have landed.
Epic: replace the five-layer type stack (`kinds.Document` → `Spec` → wire DTO → `Record` → JSONB blob) with a single Kubernetes-style `ar.dev/v1alpha1` envelope that flows YAML → HTTP → Go client → service → DB.

This guide is written so a reviewer can pick it up cold and land at a specific commit in under five minutes. Each section points at the commits to read together, what the intent was, and where the design debate sits.

**Recommended companion reads** (in order): `REBUILD_TRACKER.md` (per-subsystem port inventory + finish-line signals) → this file (commit-grouped reading order) → `REMAINING.md` (3-minute punch list of what's still deferred) → `design-docs/V1ALPHA1_PLATFORM_ADAPTERS.md` + `design-docs/V1ALPHA1_IMPORTER_ENRICHMENT.md` (design rationale; every open question was resolved on 2026-04-17).

> **Group-numbering convention.** The `Group N` section **headers** below are commit-review chunks — they structure the reading order. References to `Group N` inside section **prose** point at the 14 port subsystems tracked in `REBUILD_TRACKER.md` (e.g. Group 3 = service-layer dissolution, Group 5 = platform adapters, Group 9 = MCP protocol bridge). Two numbering schemes, both prefixed `Group`; context disambiguates.

---

## TL;DR architecture

```
┌──────────────── YAML / JSON manifest ────────────────┐
│   apiVersion: ar.dev/v1alpha1                        │
│   kind: Agent | MCPServer | Skill | Prompt | ...     │
│   metadata: { namespace, name, version, labels,      │
│               annotations, finalizers, ... }         │
│   spec:   { <kind-specific typed body> }             │
│   status: { observedGeneration, conditions: [...] }  │
└──────────────────────────────────────────────────────┘
                        │
                        ▼ v1alpha1.Scheme.Decode / DecodeMulti
                        │
  ┌───────────────── Object interface ─────────────────┐
  │   Validate()                structural + field rules │
  │   ResolveRefs(resolver)     cross-kind ref existence │
  │   ValidateRegistries(v)     OCI/NPM/PyPI/NuGet/MCPB  │
  │   ValidateUniqueRemoteURLs  cross-row URL invariant  │
  │   MarshalSpec()             JSONB payload for store  │
  └─────────────────────────────────────────────────────┘
                        │
                        ▼ generic database.Store
                        │
  ┌──────────────── v1alpha1.* (new schema) ───────────┐
  │   agents / mcp_servers / skills / prompts /        │
  │   providers / deployments — identical shape       │
  │   PK=(namespace, name, version), JSONB spec+status │
  │   plus enrichment_findings side table              │
  └─────────────────────────────────────────────────────┘
```

Key design decisions embedded in the code:

| Decision | Where it lives |
|----------|----------------|
| `ar.dev/v1alpha1` API version | `pkg/api/v1alpha1/doc.go` |
| Composite PK `(namespace, name, version)` | `migrations_v1alpha1/001_v1alpha1_schema.sql` |
| Soft delete + finalizers | `ObjectMeta.DeletionTimestamp`, `Store.Delete`, `Store.PurgeFinalized` |
| Server-managed generation, bumps on spec change | `Store.Upsert` (`store_v1alpha1.go`) |
| Semver-aware `is_latest_version` | `Store.recomputeLatest` / `pickLatestVersion` |
| Status writes disjoint from spec writes | `Store.PatchStatus` |
| Dedicated PostgreSQL schema for coexistence | `CREATE SCHEMA v1alpha1` in `001_v1alpha1_schema.sql` |
| Status-only NOTIFY trigger | `v1alpha1.notify_status_change()` |
| Refs are pure `ResourceRef{Kind, Namespace, Name, Version}` | `pkg/api/v1alpha1/ref.go` |
| Validation co-located on Spec types | `{agent,mcpserver,skill,prompt,provider,deployment}_validate.go` |
| Cross-kind ref resolution through resolver | `router/v0.go registerV1Alpha1Routes` |
| Multi-doc YAML apply | `resource/apply.go` |
| Annotations as K8s-style free-form | `ObjectMeta.Annotations`; `annotations` JSONB column on every table |
| Scanner plug-in contract | `pkg/importer/scanner.go` |
| Enrichment findings in a side table | `migrations_v1alpha1/002_enrichment_findings.sql` |
| Built-in Kind list (stable order) | `pkg/api/v1alpha1/doc.go BuiltinKinds` |
| Kind → table mapping | `internal/registry/database/store_v1alpha1_tables.go V1Alpha1TableFor` |
| One-line Stores constructor | `database.NewV1Alpha1Stores(pool)` |
| Six generic `Register[*T]` calls | behind `resource.RegisterBuiltins` |
| Shared version-lock helper | `pkg/api/v1alpha1/versionutil.go` (`IsSemanticVersion`, `CompareVersions`) |
| Unified apply pipeline | `resource.RegisterApply` mounts POST + DELETE at `/v0/apply` |
| Apply result wire contract | `internal/registry/api/apitypes/apply.go` |
| Cross-row URL-uniqueness invariant | `(Object).ValidateUniqueRemoteURLs` + `database.NewV1Alpha1UniqueRemoteURLsChecker` |
| Generic Go client | `internal/client/client.go` (Get / GetLatest / List / Apply / DeleteViaApply / Delete) |
| Deprecated imperative client stubs | `internal/client/client_deprecated.go` (returns `errDeprecatedImperative`) |

---

## Reading order

Commits are logically grouped. Review in this order and you'll never be missing context when you hit the next commit.

### Group 1 — Types foundation (`d54c8ef..81e732e`, 7 commits)

The typed envelope and everything that hangs off it. Read these in order.

| Commit | Subject | Focus |
|--------|---------|-------|
| `d54c8ef` | v1alpha1 Kubernetes-style envelope types | Core Agent/MCPServer/Skill/Prompt/Provider/Deployment structs + Scheme + Conditions. The entire package foundation. |
| `6715813` | drop Status.Phase in favor of Conditions | Follow-up to `d54c8ef` — Phase removed, Conditions are the only source of truth for reconciliation state. |
| `f1771c4` | apply design review feedback | `TemplateRef` → `TargetRef` rename; drops `UID` from ObjectMeta; review-driven cleanups. |
| `8237dec` | add Namespace + DeletionTimestamp + Finalizers | K8s-style identity + soft-delete fields added to `ObjectMeta`. Every kind + DB migration picks these up. |
| `b7d10c3` | add ObjectMeta.Annotations | K8s-style free-form metadata, non-indexed. Distinct from labels (indexed, short values). |
| `81e732e` | Validate() + ResolveRefs() on Object interface | The port-target for legacy `internal/registry/validators/`. Co-located with the Spec so validation surface is discoverable per kind. |
| `947e327` | mark validators Group 6 structural port complete | Tracker maintenance. |

**What to pay attention to**
- `pkg/api/v1alpha1/object.go:48-73` — `ObjectMeta` shape. Every field is deliberate; Generation/CreatedAt/UpdatedAt/DeletionTimestamp are explicitly server-managed (see comment). This is the shape all clients + the wire format commit to.
- `pkg/api/v1alpha1/scheme.go` — Decode / DecodeMulti / Register. The registry pattern lets enterprise plug in additional kinds via `Scheme.Register`. Verify this surface is stable.
- `pkg/api/v1alpha1/validation.go` — regex rules (`nameRegex`, `namespaceRegex`, `labelKeyRegex`, `versionRangeRegex`), reserved version literal `"latest"`, URL policy (https-only for websiteUrl). These are policy choices that affect every manifest. Push on them now.
- `pkg/api/v1alpha1/ref.go` — `ResourceRef{Kind, Namespace, Name, Version}`. Blank namespace = "inherit from referrer"; blank version = "latest". Inheritance happens in each kind's `ResolveRefs`.
- Accessor generation (`pkg/api/v1alpha1/accessors.go`) — every kind gets `GetMetadata() / SetMetadata / GetStatus / SetStatus / MarshalSpec / UnmarshalSpec` so generic code treats them uniformly.

### Group 2 — Database (`7ab5a2a`, `a45a439`, 2 commits)

The generic Store and the schema that backs it.

| Commit | Subject | Focus |
|--------|---------|-------|
| `7ab5a2a` | generic Store + schema | One `Store` type bound to one table, serving every kind. Migration file creates all six tables identically. |
| `a45a439` | isolate v1alpha1 tables in dedicated PostgreSQL schema | Coexistence move: put the new tables under `v1alpha1.*` so legacy `public.agents`, `public.servers` keep serving their users during the port. |

**What to pay attention to**
- `internal/registry/database/store_v1alpha1.go:108-216` — `Upsert`. Read carefully: `oldSpec` vs `newSpec` determines generation bump; `opts.Finalizers nil == preserve`, `empty slice == clear`; `opts.Annotations` mirrors the same pattern; `recomputeLatest` runs inside the same transaction so `is_latest_version` is never transiently inconsistent.
- `internal/registry/database/store_v1alpha1.go:222-260` — `PatchStatus`. Read → mutate callback → write; never touches generation or spec. This is the only way reconcilers talk to status rows. The KRT rebase (Phase 2) depends on this signature staying stable.
- `internal/registry/database/store_v1alpha1.go:343-374` — `Delete` is soft. Sets `deletion_timestamp` and re-runs `recomputeLatest` so the terminating row loses `is_latest_version`. Callers with finalizers see the terminating row until `PatchFinalizers` empties the list; then `PurgeFinalized` hard-deletes.
- `internal/registry/database/store_v1alpha1.go:560-580` — `pickLatestVersion`. Semver-aware with a fallback to "most-recently-updated" when none of the versions parse. Fallback is deliberate (see decisions table).
- `internal/registry/database/migrations_v1alpha1/001_v1alpha1_schema.sql` — the whole file. Check column types (varchar lengths), indexes (GIN on `spec jsonb_path_ops`, partial unique on `is_latest_version`), the `notify_status_change` trigger payload shape (`{"op":"...", "id":"<ns>/<name>/<ver>"}` — this is a **hard seam** the Phase 2 KRT rebase depends on).
- `internal/registry/database/postgres.go:126-129` — both migrators run side-by-side. Legacy `public.*` migrations keep running; `v1alpha1.*` migrations run after. That's how dual-stack coexistence works.

### Group 3 — HTTP surface (`ec84636`, `5b2f3a4`, `f3de384`, 3 commits)

The generic resource handler, the router wiring, and the multi-doc apply endpoint.

| Commit | Subject | Focus |
|--------|---------|-------|
| `ec84636` | generic resource handler | `resource.Register[T v1alpha1.Object]` — one function registers GET/LIST/PUT/DELETE for any kind. Reflection-free; driven by Go generics + the Object interface. |
| `5b2f3a4` | wire v1alpha1 routes alongside legacy at /v0 | The router mounts `/v0/namespaces/{ns}/{plural}/{name}/{version}` for every kind with a registered Store. Includes the cross-kind `ResolverFunc` that dispatches to per-kind Stores for dangling-ref checks. |
| `f3de384` | multi-doc YAML batch apply at POST /v0/apply | Accepts a `---`-separated stream of manifests, decodes each with Scheme, validates, resolves refs, Upserts per-kind. Document-level failures are captured in the `Results` slice and do not short-circuit the batch. |

**What to pay attention to**
- `internal/registry/api/handlers/v0/resource/handler.go` — the `Register[T]` type parameter constrains to `v1alpha1.Object`. Each endpoint is defined once and specialized at compile time.
- `internal/registry/api/handlers/v0/resource/handler.go` ~line 60 — `UpsertOpts` carries both `Annotations` and `Finalizers` from `metadata` through to the Store. Worth verifying no field is dropped silently.
- `internal/registry/api/handlers/v0/resource/apply.go applyOne` — the heart of the apply pipeline: namespace defaulting, validate, resolve refs, marshal spec, Upsert. Trace one document all the way through.
- `internal/registry/api/router/v0.go registerV1Alpha1Routes` — cross-kind resolution wiring; a ref to an MCPServer from an Agent manifest flows through the resolver into the MCPServer Store's `Get`.

### Group 4 — Seed + adapter interface (`ee07520`, `4a6e1a6`, 2 commits)

Small but load-bearing.

| Commit | Subject | Focus |
|--------|---------|-------|
| `ee07520` | v1alpha1 builtin MCPServer seeder | `internal/registry/seed/v1alpha1.go` reads the existing `seed.json` and writes via the generic Store so v1alpha1 paths have curated data on first boot. |
| `4a6e1a6` | v1alpha1 DeploymentAdapter interface + noop reference | `pkg/types/adapter_v1alpha1.go` establishes the Apply/Remove/Logs/Discover contract for platform adapters. `internal/registry/platforms/noop/adapter.go` is the reference implementation. No native adapter porting — that's a follow-up. |

**What to pay attention to**
- `pkg/types/adapter_v1alpha1.go` — `DeploymentAdapter` + `ProviderAdapter` interfaces; `ApplyResult.AddFinalizers` lets adapters declare finalizers they'll own; `ProviderMetadata` is `map[string]string` destined for ObjectMeta.Annotations after reconciler plumbing lands.
- `internal/registry/platforms/noop/adapter.go` — if you're skeptical about the adapter contract, read this. It's the narrowest possible implementation; any real adapter has to satisfy the same surface.
- `internal/registry/seed/v1alpha1.go` — the seeder uses the raw pgxpool. Backends that don't expose a pool (noop/DatabaseFactory test shims) skip seeding entirely. The legacy seeder + its `seed-readme.json` were deleted in Group 11; this is the only seeder left.

### Group 5 — Importer pipeline + scanners (`64ff130..2241012`, 11 commits)

The longest chain. This established the new `pkg/importer` Scanner interface and ported OSV + Scorecard onto it via `git mv`. The legacy `internal/registry/importer/` package stayed alive through Groups 5–9; Group 11 deletes it.

| Commit | Subject |
|--------|---------|
| `64ff130` | Scanner interface + FindingsStore + enrichment schema |
| `e2e6f8f` | importer core pipeline (decode + validate + enrich + upsert) |
| `e8aa6bd` | tracker update |
| `0ec9297` | **refactor**: `git mv osv_scan.go → pkg/importer/scanners/osv/` |
| `d2ba661` | wrap ported OSV scanner in Scanner interface |
| `69eaf58` | OSV scanner endpoint-override Config for unit tests |
| `4ad509c` | OSV scanner unit tests |
| `4e2afd7` | **refactor**: `git mv scorecard_lib.go → pkg/importer/scanners/scorecard/` (99% similarity) |
| `3f65d84` | wrap ported Scorecard scanner in Scanner interface |
| `a44cad1` | Scorecard scanner unit tests |
| `2241012` | tracker update |

**Git-mv-first review pattern.** The two `refactor(importer): move ... →` commits are pure renames with only the package declaration edited. `git log --follow pkg/importer/scanners/osv/osv.go` traces back through `0ec9297` into the original PR #18. Reviewers:

1. Read the rename commit's diff — should be small (70% similarity for OSV, 99% for Scorecard).
2. Read the "wrap in Scanner interface" commit next — where new code gets added on top of the ported helpers.
3. Read the test commit last.

**What to pay attention to**
- `pkg/importer/scanner.go` — the `Scanner` interface and the `EnrichmentPrefix` vocabulary (AnnoOSVStatus, AnnoScorecardBucket, etc.). These string constants define the on-wire enrichment contract.
- `pkg/importer/importer.go importOne` — decodes, defaults namespace, validates, resolves refs, runs scanners, upserts, writes findings. One function is the whole pipeline.
- `pkg/importer/importer.go runScanners` — scanner errors are isolated: one bad scanner downgrades `EnrichmentStatus` to `partial`, but the import still upserts. Critical behavior.
- `pkg/importer/findings_store.go Replace` — atomic DELETE + INSERT per `(object, source)` inside a single transaction.
- `pkg/importer/scanners/osv/osv.go` — three preserved-verbatim helpers (`parseNPMLockForOSV`, `parsePipRequirementsForOSV`, `parseGoModForOSV`) and a new `Scanner` wrapper on top. `queryOSVBatchDetailed` is a sibling of legacy `queryOSVBatch` with richer per-CVE severity.
- `pkg/importer/scanners/scorecard/scorecard.go` — `runFunc` test hook (unexported) lets unit tests fake the scorecard engine without hitting GitHub.

### Group 6 — Deduplication pass (`d31a56e`, `80c903a`, 2 commits)

Post-review cleanup. These don't change behavior; they collapse structural duplication reviewers flagged.

| Commit | Subject | Focus |
|--------|---------|-------|
| `d31a56e` | collapse V1Alpha1Stores duplication | `V1Alpha1Stores` becomes `map[string]*Store`. `BuiltinKinds` + `V1Alpha1TableFor` + `NewV1Alpha1Stores` + `RegisterBuiltins` consolidate the three former copies (struct fields, `storeForKind` switch, apply handler's stores map). |
| `80c903a` | share GitHubRepoFor across OSV + Scorecard | Verbatim `repoFor` + GitHub URL parser in both scanner packages moved to `pkg/importer/githubrepo.go`. |

**What to pay attention to**
- `pkg/api/v1alpha1/doc.go BuiltinKinds` — the stable ordered slice. Adding a new built-in kind means editing this + `V1Alpha1TableFor` + one case in `RegisterBuiltins`'s switch.
- `internal/registry/api/handlers/v0/resource/builtins.go` — houses the six `Register[*v1alpha1.X]` generic calls. Can't be collapsed further (Go generics are compile-time) but localized here, not scattered.
- `internal/registry/api/router/v0.go registerV1Alpha1Routes` — one `RegisterBuiltins` call plus one `RegisterApply` call. The resolver closure uses `stores[ref.Kind]` directly.
- `pkg/importer/githubrepo.go` — exported `GitHubRepoFor(obj)` handles Agent, MCPServer, Skill.

### Group 7 — Importer bootstrap wire-up (`8d13ee4`, `20abd73`, 2 commits)

Closes the server-side half of the importer subsystem. The Importer + scanners from Group 5 are constructed at bootstrap and exposed via HTTP.

| Commit | Subject | Focus |
|--------|---------|-------|
| `8d13ee4` | wire v1alpha1 Importer + POST /v0/import | New `Importer.ImportBytes` method; `POST /v0/import` endpoint; `internaldb.NewV1Alpha1Resolver` extracted so router + Importer + bootstrap share one ref-existence definition. 8 integration tests. |
| `20abd73` | route `cfg.SeedFrom` through the v1alpha1 Importer | Hoist Stores + Importer construction up to bootstrap top. SeedFrom prefers the v1alpha1 Importer; Group 11 later removed the legacy fallback entirely. |

**What to pay attention to**
- `pkg/importer/importer.go ImportBytes` — decode + loop shared with `Import` via `importStream`; source string labels records for debugging.
- `internal/registry/api/handlers/v0/resource/import.go` — nil Importer skips route registration. Query params mirror `importer.Options` (namespace, enrich, scans, dryRun, scannedBy).
- `internal/registry/database/store_v1alpha1_tables.go NewV1Alpha1Resolver` — one ResolverFunc factory; router, apply handler, and Importer all call it.

### Group 8 — Registry validators port (`4f2f212`, `a6ea729`, `020e2bf`, 3 commits)

Legacy per-registry validators (OCI allowlist + label match; NPM/PyPI/NuGet "identifier exists"; MCPB checksum) moved from `internal/registry/validators/registries/` to `pkg/api/v1alpha1/registries/` via `git mv`. 86-100% rename similarity — reviewers can diff each to see only the `model.Package` → `v1alpha1.RegistryPackage` type swap.

| Commit | Subject | Focus |
|--------|---------|-------|
| `4f2f212` | port OCI validator to v1alpha1 | `git mv oci.go{,_test.go}`; add `v1alpha1.RegistryPackage` + `RegistryValidatorFunc` + `(Object).ValidateRegistries` interface surface. |
| `a6ea729` | port NPM / PyPI / NuGet / MCPB validators | Same pattern across four validators. Re-exports `RegistryType{NPM,...}` + `RegistryURL{NPM,...}` constants in v1alpha1 so seed data + manifests round-trip unchanged. Legacy registries directory deleted. |
| `020e2bf` | wire registries.Dispatcher into apply + import | `registries.Dispatcher` is the v1alpha1-native `RegistryValidatorFunc`. Threaded through `resource.Config` + `ApplyConfig` + `RegisterBuiltins` + `Importer.Config`. Bootstrap passes it everywhere. |

**What to pay attention to**
- `pkg/api/v1alpha1/registry_validate.go` — the `RegistryPackage` shape, per-kind `ValidateRegistries` methods, and `validatePackages` helper that accumulates FieldErrors per bad package.
- `pkg/api/v1alpha1/registries/dispatcher.go` — how the switch routes RegistryType to the per-registry validator. Add a new `case` here when a new registry type lands.
- `internal/registry/validators/package.go` — legacy `ValidatePackage(model.Package)` translates to `v1alpha1.RegistryPackage` on the fly and calls the same dispatcher. Both stacks share validator code until the last legacy consumer ports.

### Group 9 — Service-layer dissolve: hoists (`2972363`, `e9ef6fb`, `2f667eb`, 3 commits)

The first wave of pulling business logic off the legacy `internal/registry/service/` packages onto the v1alpha1 surface. Each commit moves one slice.

| Commit | Subject | Focus |
|--------|---------|-------|
| `2972363` | hoist versionutil to `pkg/api/v1alpha1` | `git mv internal/registry/service/internal/versionutil/{versionutil,versionutil_test}.go pkg/api/v1alpha1/` (72% / 97% similarity). `IsSemanticVersion` + `CompareVersions` keep signatures; 4 legacy service files update imports in the same commit. `EnsureVPrefix` inlined as unexported `ensureVPrefix` because `internal/version` isn't importable from `pkg/*`. |
| `e9ef6fb` | port URL-uniqueness to Object interface | New `(Object).ValidateUniqueRemoteURLs(ctx, UniqueRemoteURLsFunc)` parallel to `ResolveRefs` + `ValidateRegistries`. Impls on Agent / MCPServer / Skill iterate `spec.remotes[*].url`; no-op on Prompt / Provider / Deployment. `database.NewV1Alpha1UniqueRemoteURLsChecker` uses `Store.FindReferrers` with JSONB containment `{"remotes":[{"url":"..."}]}`. **Cross-namespace by design** — a URL is a global real-world identifier. PUT returns 409 Conflict; apply surfaces as `Status="failed"`. 7 unit + 2 integration tests. |
| `2f667eb` | consolidate /v0/apply onto v1alpha1 resource handler | Deletes `internal/registry/api/handlers/v0/apply/` + `handlers/v0/common/apply_errors.go` + ~160 LOC of `kindReg`/`providerApplyFunc`/`deploymentApplyFunc` wiring in `registry_app.go`. `RegisterApply` gains `DryRun` + `Force` query params and a DELETE verb (soft-deletes every doc). `ApplyResult` + status constants hoisted into `internal/registry/api/apitypes/apply.go` so `internal/client` + CLI consume the wire type without pulling a handler package. `RouteOptions.KindRegistry` dropped. |

**What to pay attention to**

- `pkg/api/v1alpha1/versionutil.go` — two exported functions. `CompareVersions` is the authority for `isLatest`; apply-path version-locking is explicitly **not** enforced (legacy only used this to decide latest, not to reject old-version applies — see `REBUILD_TRACKER.md` §3).
- `pkg/api/v1alpha1/remote_urls_validate.go` — within a single manifest, only the **first** conflicting URL is reported; across manifests, `Store.FindReferrers` does the heavy lift via GIN-indexed JSONB lookup. Runs after `ResolveRefs` so dangling-ref errors surface first.
- `internal/registry/api/handlers/v0/resource/apply.go:65-68` — `DryRun` runs validate/resolve/registries/uniqueness pipeline and short-circuits before Upsert; `Force` is accepted for CLI wire compatibility and is a **no-op**.
- `internal/registry/api/handlers/v0/resource/apply.go:104-111` — DELETE `/v0/apply` is new. Validates (for error-surface parity) then calls `Store.Delete`. Missing rows return `Status="failed"`, not silent success.
- `internal/registry/api/apitypes/apply.go` — the wire contract is now OSS-facing package-level. `ApplyStatus{Created,Configured,Unchanged,Deleted,DryRun,Failed}` strings are public.

### Group 10 — Legacy HTTP handler collapse (`8be4067`, `a7ab3a4`, 2 commits)

The big deletion pass. With the v1alpha1 generic handler serving equivalent routes at `/v0/namespaces/{ns}/{plural}/{name}/{version}` since `5b2f3a4`, and `/v0/apply` unified in Group 9, the legacy per-kind handlers had no non-legacy clients. **-3,558 LOC across 9 files.**

| Commit | Subject | Focus |
|--------|---------|-------|
| `8be4067` | delete legacy per-kind handlers (agents/skills/prompts/providers) | Removes `internal/registry/api/handlers/v0/{agents,skills,prompts,providers}/` and their router registrations. -1,117 LOC. |
| `a7ab3a4` | delete legacy servers handler + deploymentmeta | Removes `internal/registry/api/handlers/v0/servers/` (including `edit.go` + `telemetry_test.go`) and `handlers/v0/deploymentmeta/` (inline deployment summary on agent/server list). -2,443 LOC. |

**What's still alive** — each has a specific reason:
- **`internal/registry/api/handlers/v0/deployments/`** — SSE watch + logs + cancel. Hard seam for Phase 2 KRT; Group 4/5 coordinates the port alongside the deployment-service rewrite.
- **`/v0/servers/*/readme` (BYTEA table)** — `server_readmes` table still exists, still written by seed/importer, still read by legacy MCP tools. Deferred until the data-model decision lands (MCPServerSpec field vs. sub-handler).
- **Per-kind service packages** (`internal/registry/service/{agent,server,skill,provider}/`) — still consumed by MCP registryserver (Group 9), platform adapters (Group 5), and a handful of remaining callers. Tracked as Group 3 remainder.

**What to pay attention to**
- `internal/registry/api/router/v0.go` — now only imports `resource`, `health`, `ping`, `version`, `embeddings`, `deployments`, `extensions`, and `apply` via `RegisterApply`. Four of those are `/v0` endpoints the refactor doesn't touch; five (`resource`, `extensions`, `deployments`, `embeddings`, `apply`) are the active surface.
- **Legacy coexistence is now narrower**: `public.*` schema still holds data written by the legacy seeder/importer historically, but no HTTP handler writes to it anymore except deployments (which have their own port path).
- `internal/registry/service/prompt/` removed in `20b25d4` (Group 12). The five remaining service packages are only `agent`, `server`, `skill`, `provider`, `deployment` — down from seven.

### Group 11 — Legacy importer/exporter/builtin-seed/CLI import+export deletion (`048619f`, 1 commit)

Closes Group 7's tracker entry. The legacy one-shot bootstrap tooling pre-dated `pkg/importer`; once the v1alpha1 importer had been the real seed/import path for several PRs, the legacy stack was dead code.

| Commit | Subject | Focus |
|--------|---------|-------|
| `048619f` | delete legacy importer / exporter / builtin-seed / CLI import+export | Drops `internal/registry/importer/` (1,535 LOC), `internal/registry/exporter/`, `internal/registry/seed/{builtin.go, readme.go, seed-readme.json}` (1,196 LOC JSON alone), `internal/cli/{import,export}.go`. Simplifies bootstrap: `runSeedFromImport` signature collapsed to `(cfg, v1alpha1Importer)`; no legacy fallback. **-4,400 LOC.** |

**What to pay attention to**
- `internal/registry/registry_app.go` — `cfg.SeedFrom` now requires the v1alpha1 importer bundle. Without a `*pgxpool.Pool`, the operator sees a warning and skips — deliberate: legacy dependency chain is gone.
- `internal/cli/declarative/integration_test.go` was deleted outright. Its cases exercised legacy `/v0/{agents,servers,skills}/...` routes through client methods that got rewritten in Group 12.
- `internal/client/client_integration_test.go TestClientIntegration_CatalogRoutes_HappyPath` is `t.Skip()`'d with a pointer to Group 12 (replacement integration tests ship there).

### Group 12 — Go client rewrite + prompt service deletion (`20b25d4`, `5aa8339`, 2 commits)

Finishes Group 2 (generic Go client) and pulls Prompt from the legacy service bag.

| Commit | Subject | Focus |
|--------|---------|-------|
| `20b25d4` | delete prompt service + fake_registry test shim | Removes `internal/registry/service/prompt/` and `internal/registry/service/testing/fake_registry.go`. Router's `RegistryServices` struct drops its `Prompt` field. -644 LOC. `registry_service_test.go` keeps its Prompt mock types because `TestResolveAgentManifestPrompts` exercises the Agent service (which reads Prompts for ref resolution), not a Prompt service. |
| `5aa8339` | rewrite Go client as generic v1alpha1 methods | `internal/client/client.go` goes from 1,800 LOC of per-kind typed methods to **534 LOC** of kind-agnostic `Get / GetLatest / List / Apply / DeleteViaApply / Delete / PatchStatus`. Deprecated imperative methods (`GetServer`, `CreatePrompt`, etc.) move to `client_deprecated.go` (133 LOC of stubs returning `errDeprecatedImperative`) to keep the imperative-CLI packages compiling until the declarative CLI branch merges. New `client_v1alpha1_test.go` (160 LOC) covers the generic round-trip. **-2,773 LOC net.** |

**What to pay attention to**
- `internal/client/client.go` — generic methods speak `*v1alpha1.Object` on the wire. Clients supply a Kind string + typed pointer; the client issues the HTTP call and decodes the response. Bearer token, configurable `http.Client`, and `404 → ErrNotFound` all preserved.
- `internal/client/client_deprecated.go` — every method returns `errDeprecatedImperative` at runtime. No server call is made. Deletable in one commit when the imperative CLI branch merges.
- `internal/client/client_v1alpha1_test.go` — new round-trip test is the replacement for the deleted declarative integration test.
- **Finish-line target is <400 LOC**; currently 534. The remaining ~160 LOC is legacy deployment + embeddings RPCs (subsystem Groups 4 + 8); those leave the file as those subsystems port.

### Group 13 — Design docs + tracker (`d21b566`, `f575541`, `c0a89e2`, et al.)

Paper trail. Reference documents, not code changes. Useful for *why* context before reading a specific commit.

- `REBUILD_TRACKER.md` — per-subsystem port inventory. Current state of every group and what's left.
- `REMAINING.md` — 3-minute punch list with verification gate.
- `design-docs/V1ALPHA1_PLATFORM_ADAPTERS.md` — platform-adapter design; 8 open questions, all resolved 2026-04-17.
- `design-docs/V1ALPHA1_IMPORTER_ENRICHMENT.md` — importer + enrichment design; 8 open questions, all resolved.
- `DESIGN_COMMENTS_2.md` — captured review comments from @shashankram that seeded several of the decisions above.

---

## Attention areas (push hardest here)

Ranked by "blast radius if wrong".

1. **`ObjectMeta` shape (`pkg/api/v1alpha1/object.go`)**. Every wire payload, every row, every client carries this. Questions to push: are `CreatedAt`/`UpdatedAt` exposed intentionally? Should `DeletionTimestamp` be hidden behind a separate status subresource? Is `Finalizers` on the wire a good idea or should it be a subresource?

2. **Semver `is_latest_version` rule (`pickLatestVersion`)**. Decides which row `GetLatest` returns. Fallback to `updated_at DESC` when semver fails is deliberate — sanity-check whether silent fallback hides bugs.

3. **`notify_status_change` trigger payload (`001_v1alpha1_schema.sql`)**. Shape locked in: `{"op":"INSERT|UPDATE|DELETE","id":"<namespace>/<name>/<version>"}`. Phase 2 KRT reconciler depends on this. Any change is a breaking wire change.

4. **Validation rules (`pkg/api/v1alpha1/validation.go`)**. Regexes + reserved version literal `"latest"` + URL https-only policy. These reject manifests at the API boundary.

5. **Cross-row URL-uniqueness (`e9ef6fb`)**. `Store.FindReferrers` with a JSONB containment fragment as the cross-row gate. Intentionally cross-namespace — a URL is a global identifier. Verify this matches the legacy behavior it replaced; verify the JSONB containment doesn't match partial URLs or transformations.

6. **Apply pipeline order (`resource/apply.go prepareApplyDoc`)**. Namespace default → Validate → ResolveRefs → ValidateRegistries → ValidateUniqueRemoteURLs → (Upsert | DryRun short-circuit). Any reorder changes which error class surfaces first and which ones callers can short-circuit past.

7. **Scanner failure isolation (`pkg/importer/importer.go runScanners`)**. One flaky scanner must not block the import. Read `EnrichmentStatus` transitions carefully.

8. **`git mv` rename detection** — OSV `0ec9297` (70% similarity), Scorecard `4e2afd7` (99%), versionutil `2972363` (72%/97%), OCI `4f2f212`, NPM/PyPI/NuGet/MCPB `a6ea729` (86-100%). `git log --follow` traces back to original authorship on every one. If you want to verify the logic wasn't disturbed, diff the legacy file against the post-wrap result.

9. **Deprecated client stubs (`client_deprecated.go`)**. Every method is a runtime `errDeprecatedImperative`. If any production code path (not CLI) still calls one of these, we've lost functionality silently until the error surfaces. Grep for `errDeprecatedImperative` usage at review time.

---

## Adding a new built-in kind

After the Group-6 dedup commits, the path is mechanical:

1. **`pkg/api/v1alpha1/`** — author the envelope, Spec, validator, and accessor methods; register in `Scheme` via `MustRegister` in `scheme.go newDefaultScheme`.
2. **`pkg/api/v1alpha1/doc.go`** — append the Kind const to `BuiltinKinds`.
3. **`internal/registry/database/store_v1alpha1_tables.go`** — add the `Kind → table` entry to `V1Alpha1TableFor`.
4. **`internal/registry/api/handlers/v0/resource/builtins.go`** — add a `case` in `RegisterBuiltins`'s switch so the `Register[*NewKind]` generic call is emitted.
5. **`internal/registry/database/migrations_v1alpha1/`** — add a migration that creates the backing table (copy the shape from an existing table).

Everything else — router wiring, apply endpoint, resolver, bootstrap `NewV1Alpha1Stores` — picks up the new kind automatically.

Enterprise / downstream builds adding proprietary kinds: call `v1alpha1.Scheme.Register(...)` at init, extend the Stores map before passing it to the router, and call `resource.Register[*YourKind]` directly in their own setup. No patches to OSS files.

---

## Decisions worth calling out explicitly in review

These came up during design and are embedded in the code now. Flag anything you disagree with.

- **No backwards compat at end.** Legacy code stays runnable during the port (per-subsystem). When a subsystem's port PR lands, its legacy code is gone. Groups 10, 11, 12 are evidence — ~11k LOC of legacy deleted in three PRs.
- **Apply = publish.** No separate publish verb. Applying a manifest creates the row and sets `is_latest_version` in one transaction.
- **Pure JSONB + GIN for reverse lookups.** No promoted columns for "agents referencing MCPServer X". GIN-indexed `spec @>` queries carry the weight — that's also how URL-uniqueness is implemented.
- **Annotations not indexed.** Labels are the queryable surface; annotations are narrative. Three enrichment keys (`osv-status`, `scorecard-bucket`, `last-scanned-stale`) are promoted to both — see `PromotedToLabels` in `pkg/importer/scanner.go`.
- **Status.Phase dropped.** Conditions are the only reconciliation-state surface.
- **Refs are pure.** No inline fields like `AgentSpec.MCPServers[].Type|Command|Args|URL` coexisting with ref-style. Anywhere a ref can go, it's `ResourceRef{Kind, Namespace, Name, Version}`.
- **Deployments have the same PK shape.** No UUID; `(namespace, name, version)` like every other kind.
- **Apply-path version-lock not enforced.** Legacy only used `CompareVersions` to decide `isLatest`, never to reject old-version applies. `Store.Upsert` + `pickLatestVersion` preserve that semantic. `Force` query param accepted for wire compat; no-op.
- **Deprecated-stub pattern over package deletion.** `client_deprecated.go` keeps the imperative CLI compiling with runtime-error stubs rather than deleting methods outright. Lets the declarative CLI branch merge independently.

---

## How to run tests locally

```bash
make test-unit                                          # fast, no infra
make test                                               # integration, needs Postgres on :5432
go test -tags=unit ./pkg/importer/scanners/...          # scanner unit tests specifically
go test -tags=integration ./pkg/importer/...            # importer integration tests
go test -tags=integration ./internal/registry/database/... # store integration tests
go test -tags=integration ./internal/client/...         # v1alpha1 client round-trip
```

All of these must be green on this branch. Group 11 marked `TestClientIntegration_CatalogRoutes_HappyPath` as `t.Skip()`; Group 12's `client_v1alpha1_test.go` is its replacement.

---

## What's NOT in this PR (deferred)

See `REMAINING.md` for the full punch list. Biggest remaining items now that Groups 10–12 have landed:

- **Native platform adapter ports** (`local` docker-compose ~1k LOC; `kubernetes` CRD templating ~1.5k LOC). Interface is in (`pkg/types/adapter_v1alpha1.go`); implementations are not.
- **Deployment service + deployment HTTP endpoints** (SSE watch, cancel, logs). Hard seam for Phase 2 KRT. Coordinated with platform-adapter port.
- **Per-kind service packages deletion** (remaining: `agent`, `server`, `skill`, `provider`, `deployment`). Blockers: MCP registryserver (Group 13), platform adapters (Group 5), and a handful of platform/utils call sites. Prompt is already gone.
- **MCP protocol bridge** (`internal/mcp/registryserver/`). Tools still speak legacy types. Port will rewrite them onto the generic Store.
- **Embeddings indexer port**. Text assembly + SSE progress + `?semantic=` search still on legacy types; needs a `semantic_embedding` column family added to v1alpha1 tables via additive migration.
- **`server_readmes` BYTEA surface**. Data-model decision pending — MCPServerSpec field vs. sub-handler.
- **Go client < 400 LOC target**. Currently 534; remaining ~160 LOC is the legacy deployment + embeddings RPCs that leave when those groups port.
- **`internal/client/client_deprecated.go` deletion**. Blocked by imperative CLI packages that still reference the stubbed methods. Unblocks when the declarative CLI branch merges.
- **`internal/registry/kinds/` deletion**. Used by CLI-side YAML validation before POST. Removes with the declarative CLI.
- **UI TypeScript client regen + component fixups**. Kicks off after the Go client finishes trimming to <400 LOC.
- **Phase 2 KRT reconciler rebase** onto this branch. Wraps `models.Deployment` → `*v1alpha1.Deployment` at ingest, converts scalar-status checks to condition queries, repoints `UpdateDeploymentState` → `Store.PatchStatus`.
- **Legacy SQL migrations → `v1alpha1.*` cutover**. Dual-schema coexistence remains until the last legacy consumer ports.
