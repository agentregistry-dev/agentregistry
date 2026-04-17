# v1alpha1 API Refactor — Review Guide

Branch: `refactor/v1alpha1-types`
Scope: 31 commits, +9.9k / -0.4k lines across 63 files.
Epic: replace the five-layer type stack (`kinds.Document` → `Spec` → wire DTO → `Record` → JSONB blob) with a single Kubernetes-style `ar.dev/v1alpha1` envelope that flows YAML → HTTP → Go client → service → DB.

This guide is written so a reviewer can pick it up cold and land at a specific commit in under five minutes. Each section points at the commits to read together, what the intent was, and where the design debate sits.

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
  │   Validate()           structural + field rules     │
  │   ResolveRefs(store)   cross-kind ref existence     │
  │   MarshalSpec()        JSONB payload for the store  │
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
- `internal/registry/api/handlers/v0/resource/apply.go:116-184` — `applyOne`. This is the heart of the apply pipeline: namespace defaulting, validate, resolve refs, marshal spec, Upsert. Trace one document all the way through.
- `internal/registry/api/router/v0.go:144-215` — `registerV1Alpha1Routes` + `storeForKind`. This is where cross-kind resolution is wired; a ref to an MCPServer from an Agent manifest flows through the resolver into the MCPServer Store's `Get`.
- The new routes coexist with legacy handlers at the same `/v0` prefix. Legacy paths (`/v0/servers/...`) stay live for now; clients migrate to namespaced paths over time.

### Group 4 — Seed + adapter interface (`ee07520`, `4a6e1a6`, 2 commits)

Small but load-bearing.

| Commit | Subject | Focus |
|--------|---------|-------|
| `ee07520` | v1alpha1 builtin MCPServer seeder | `internal/registry/seed/v1alpha1.go` reads the existing `seed.json` and writes via the generic Store so v1alpha1 paths have curated data on first boot. Legacy seeder still runs in parallel. |
| `4a6e1a6` | v1alpha1 DeploymentAdapter interface + noop reference | `pkg/types/adapter_v1alpha1.go` establishes the Apply/Remove/Logs/Discover contract for platform adapters. `internal/registry/platforms/noop/adapter.go` is the reference implementation. No native adapter porting — that's a follow-up. |

**What to pay attention to**
- `pkg/types/adapter_v1alpha1.go` — `DeploymentAdapter` + `ProviderAdapter` interfaces; `ApplyResult.AddFinalizers` lets adapters declare finalizers they'll own; `ProviderMetadata` is `map[string]string` destined for ObjectMeta.Annotations after reconciler plumbing lands.
- `internal/registry/platforms/noop/adapter.go` — if you're skeptical about the adapter contract, read this. It's the narrowest possible implementation; any real adapter has to satisfy the same surface.
- `internal/registry/seed/v1alpha1.go:~40-100` — the seeder consults the raw pgxpool. For backends that don't expose a pool (noop/test), the v1alpha1 path is skipped; see `registry_app.go` where this is gated.

### Group 5 — Importer pipeline + scanners (`64ff130..2241012`, 11 commits)

The longest chain. This ports 2.5k LOC of legacy `internal/registry/importer/` onto the new Scanner interface without breaking the legacy importer while the port is in flight.

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

**Git-mv-first review pattern.** The two `refactor(importer): move ... →` commits are pure renames of the legacy file into the new location, with only the package declaration edited. `git log --follow` on `pkg/importer/scanners/osv/osv.go` traces back through `0ec9297` into the original PR #18 that authored the logic. Reviewers can:

1. Read the rename commit's diff — should be small (70% similarity for OSV, 99% for Scorecard).
2. Read the "wrap in Scanner interface" commit next — that's where new code gets added on top of the ported helpers.
3. Read the test commit last.

**What to pay attention to**
- `pkg/importer/scanner.go` — the `Scanner` interface and the `EnrichmentPrefix` vocabulary (AnnoOSVStatus, AnnoScorecardBucket, etc.). These string constants define the on-wire enrichment contract.
- `pkg/importer/importer.go:225-340` — `importOne`. Decodes, defaults namespace, validates, resolves refs, runs scanners, upserts, writes findings. One function is the whole pipeline; step through it mentally.
- `pkg/importer/importer.go:350-407` — `runScanners`. Scanner errors are isolated: one bad scanner downgrades `EnrichmentStatus` to `partial`, but the import still upserts. Critical behavior — the importer never aborts an import because of a flaky external scanner.
- `pkg/importer/findings_store.go:38-76` — `Replace`. Atomic DELETE + INSERT per `(object, source)` inside a single transaction. This is the contract that keeps UI drill-down queries consistent across rescans.
- `pkg/importer/scanners/osv/osv.go` — note the three preserved-verbatim helpers (`parseNPMLockForOSV`, `parsePipRequirementsForOSV`, `parseGoModForOSV`) and the new `Scanner` wrapper on top. `queryOSVBatchDetailed` is a sibling of legacy `queryOSVBatch` with a richer return so findings can carry per-CVE severity.
- `pkg/importer/scanners/scorecard/scorecard.go` — the `runFunc` test hook (unexported) lets unit tests fake the scorecard engine without hitting GitHub. Real invocations go through `runScorecardLibraryDetailed`.

### Group 6 — Deduplication pass (`d31a56e`, `80c903a`, 2 commits)

Post-review cleanup. These don't change behavior; they collapse structural duplication reviewers flagged.

| Commit | Subject | Focus |
|--------|---------|-------|
| `d31a56e` | collapse V1Alpha1Stores duplication | One source of truth for kind→store. `V1Alpha1Stores` becomes `map[string]*Store`. `BuiltinKinds` + `V1Alpha1TableFor` + `NewV1Alpha1Stores` + `RegisterBuiltins` consolidate the three former copies (struct fields, `storeForKind` switch, apply handler's stores map). Router drops -57 lines. |
| `80c903a` | share GitHubRepoFor across OSV + Scorecard | Verbatim `repoFor` + GitHub URL parser in both scanner packages moved to `pkg/importer/githubrepo.go`. Scanners now call `importer.GitHubRepoFor`. -22 LOC net. |

**What to pay attention to**
- `pkg/api/v1alpha1/doc.go BuiltinKinds` — the stable ordered slice. Adding a new built-in kind means editing this + `V1Alpha1TableFor` + one case in `RegisterBuiltins`'s switch.
- `internal/registry/api/handlers/v0/resource/builtins.go` — houses the six `Register[*v1alpha1.X]` generic calls. They can't be collapsed further (Go generics are compile-time) but they're localized here, not scattered in the router.
- `internal/registry/api/router/v0.go registerV1Alpha1Routes` — one `RegisterBuiltins` call plus one `RegisterApply` call. The resolver closure uses `stores[ref.Kind]` directly.
- `pkg/importer/githubrepo.go` — exported `GitHubRepoFor(obj)` handles Agent, MCPServer, Skill. Any future GitHub-only scanner should import it.

### Group 7 — Design docs + tracker (`d21b566`, `f575541`, `c0a89e2`, et al.)

The paper trail. These are reference documents, not code changes. Useful if you want context on *why* before reading a specific commit.

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

3. **`notify_status_change` trigger payload (`001_v1alpha1_schema.sql`)**. Shape is locked in: `{"op":"INSERT|UPDATE|DELETE","id":"<namespace>/<name>/<version>"}`. The Phase 2 KRT reconciler depends on this. Any change is a breaking wire change.

4. **Validation rules (`pkg/api/v1alpha1/validation.go`)**. Regexes + reserved version literal `"latest"` + URL https-only policy. These reject manifests at the API boundary; if they're wrong, real users see errors.

5. **Legacy coexistence (`postgres.go` + `router/v0.go`)**. Both migrators run. Both route sets live. The v1alpha1 path writes to `v1alpha1.*` tables; legacy writes to `public.*`. The two never converge — they're explicitly parallel stacks. Verify no handler accidentally crosses the streams.

6. **Scanner failure isolation (`pkg/importer/importer.go:runScanners`)**. One flaky scanner must not block the import. Read `EnrichmentStatus` transitions carefully.

7. **`git mv` rename detection (OSV + Scorecard commits)**. Rename commit `0ec9297` = 70% similarity; `4e2afd7` = 99%. `git log --follow` traces back to original authorship. If you want to verify the logic wasn't disturbed in the port, diff the legacy file against the post-wrap result.

---

## Adding a new built-in kind

After the Group-6 dedup commits, the path is mechanical. Three files, three edits:

1. **`pkg/api/v1alpha1/`** — author the envelope, Spec, validator, and accessor methods for the new kind; register in `Scheme` via `MustRegister` in `scheme.go newDefaultScheme`.
2. **`pkg/api/v1alpha1/doc.go`** — append the Kind const to `BuiltinKinds`.
3. **`internal/registry/database/store_v1alpha1_tables.go`** — add the `Kind → table` entry to `V1Alpha1TableFor`.
4. **`internal/registry/api/handlers/v0/resource/builtins.go`** — add a `case` in `RegisterBuiltins`'s switch so the `Register[*NewKind]` generic call is emitted.
5. **`internal/registry/database/migrations_v1alpha1/`** — add a migration that creates the backing table (copy the shape from an existing table).

Everything else — router wiring, apply endpoint, resolver, bootstrap `NewV1Alpha1Stores` — picks up the new kind automatically.

Enterprise / downstream builds adding proprietary kinds: call `v1alpha1.Scheme.Register(...)` at init, extend the Stores map before passing it to the router, and call `resource.Register[*YourKind]` directly in their own setup. No patches to OSS files.

---

## Decisions worth calling out explicitly in review

These came up during design and are embedded in the code now. Flag anything you disagree with.

- **No backwards compat at end.** Legacy code stays runnable during the port (per-subsystem). When a subsystem's port PR lands, its legacy code is gone. No parallel DTOs forever.
- **Apply = publish.** No separate publish verb. Applying a manifest creates the row and sets `is_latest_version` in one transaction.
- **Pure JSONB + GIN for reverse lookups.** No promoted columns for "agents referencing MCPServer X". GIN-indexed `spec @>` queries carry the weight.
- **Annotations not indexed.** Labels are the queryable surface; annotations are narrative. Three enrichment keys (`osv-status`, `scorecard-bucket`, `last-scanned-stale`) are promoted to both — see `PromotedToLabels` in `pkg/importer/scanner.go`.
- **Status.Phase dropped.** Conditions are the only reconciliation-state surface.
- **Refs are pure.** No inline fields like `AgentSpec.MCPServers[].Type|Command|Args|URL` coexisting with ref-style. Anywhere a ref can go, it's `ResourceRef{Kind, Namespace, Name, Version}`.
- **Deployments have the same PK shape.** No UUID; `(namespace, name, version)` like every other kind. Users name their deployments.
- **Validator reshape, not relocation.** Old `internal/registry/validators/` logic ports onto `(Spec).Validate(ctx, store) error` methods per kind. Structural ported in `81e732e`; registry/URL-allowlist validators (Group 6 remainder) still pending.

---

## How to run tests locally

```bash
make test-unit                                          # fast, no infra
make test                                               # integration, needs Postgres on :5432
go test -tags=unit ./pkg/importer/scanners/...          # scanner unit tests specifically
go test -tags=integration ./pkg/importer/...            # importer integration tests
go test -tags=integration ./internal/registry/database/... # store integration tests
```

All three must be green on this branch.

---

## What's NOT in this PR (deferred)

See `REMAINING.md` for the full punch list. Highlights:

- Native platform adapter ports (`local` docker-compose ~1k LOC; `kubernetes` CRD templating ~1.5k LOC). Interface is in; implementations are not.
- Importer bootstrap wire-up + `POST /v0/import` HTTP endpoint. The `Importer` struct exists and is fully tested, but no server actually constructs one yet.
- Legacy importer package deletion. Stays in tree until bootstrap wire-up lands.
- Registry-validators port (Group 6 remainder: OCI/NPM/PyPI allowlists, repo-ref resolution).
- Per-kind service packages deletion (Group 3). Business logic still lives in `internal/registry/service/{agent,server,...}/`.
- Go client rewrite (Group 2). Still 1.8k LOC of typed per-kind methods.
- UI TypeScript client regen + component fixups (Group after Go client).
- Phase 2 KRT reconciler rebase onto this branch.
