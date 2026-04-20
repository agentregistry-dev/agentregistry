# What's Left — v1alpha1 Refactor Punch List

Companion to `REVIEW_GUIDE.md`. `REBUILD_TRACKER.md` has the full per-subsystem inventory; this file is the 3-minute read.

Current branch: `refactor/v1alpha1-types` (36 commits).
Scope remaining: approximately 50% of the subsystem ports. Commits landed so far establish the v1alpha1 foundation + its generic Store + HTTP surface (incl. multi-doc apply + POST /v0/import), seeding, the adapter interface (no natives), the importer pipeline with OSV + Scorecard scanners, all five per-registry validators (OCI + NPM + PyPI + NuGet + MCPB) ported and wired into apply/import, and two deduplication passes that collapsed the kind→store mapping + shared GitHub resolution across scanners.

Order below follows the dependency chain — earlier items unlock later ones.

---

## Near-term (unblock the rest)

### 1. Finish Group 7 (importer) — **MOSTLY DONE**
- [x] Wire `importer.Importer` into `internal/registry/app.Bootstrap` (commit `8d13ee4`).
- [x] Add `POST /v0/import` HTTP endpoint (commit `8d13ee4`).
- [x] Route `cfg.SeedFrom` goroutine through the new Importer (commit `20abd73`).
- [ ] Delete `internal/registry/importer/` wholesale. Blocked by `internal/cli/import.go` (still imports the legacy package). CLI port is on a separate branch per plan; legacy importer stays in tree until that branch merges.
- [ ] (Optional, scope separately) Port `container_scan.go` (Docker Hub popularity metadata) or `dependency_health.go` (deps.dev license rollups). Neither is a security scan; defer unless the UI needs the data.

### 2. Group 6 remainder — registry/URL validators — **DONE**
- [x] OCI registry allowlist + label ownership — ported via `git mv` (commit `4f2f212`).
- [x] NPM / PyPI / NuGet / MCPB "identifier exists in registry" checks — ported via `git mv` (commit `a6ea729`).
- [x] `registries.Dispatcher` + wire-up into apply + import paths (commit `020e2bf`).
- [x] Legacy `internal/registry/validators/registries/` directory empty + removed.
- [x] Legacy `ValidatePackage(ctx, model.Package, name)` dispatcher updated to route every RegistryType through the new validators via on-the-fly `v1alpha1.RegistryPackage` translation — both stacks call the same code.

**Still pending on the validator side** (lives under the broader Group 3 umbrella rather than Group 6): duplicate-URL detection across packages + remotes within a single spec. Moves onto `(Spec).Validate()` when `internal/registry/validators/validators.go ValidateServerJSON` gets ported.

### 3. Group 3 — service-layer dissolution — **IN PROGRESS**
**Status**: version-lock helper hoisted (`2972363`); URL-uniqueness ported; `prompt` + `testing` packages deleted; `agent` / `server` / `skill` / `provider` services still kept (MCP + platform adapters).

- [x] versionutil hoisted to `pkg/api/v1alpha1` as shared helper (`2972363`; git mv, 72% source / 97% test similarity). `IsSemanticVersion` + `CompareVersions` now public; legacy service packages swap imports in the same commit.
- [x] URL-uniqueness port. New `Object` interface method `ValidateUniqueRemoteURLs(ctx, UniqueRemoteURLsFunc)` parallel to `ResolveRefs` / `ValidateRegistries`. Implementation on Agent / MCPServer / Skill; no-op on Prompt / Provider / Deployment. Checker factory `database.NewV1Alpha1UniqueRemoteURLsChecker` uses `Store.FindReferrers` against JSONB `{"remotes":[{"url":"..."}]}` (cross-namespace by design — URL is a global identifier). Wired through `resource.Config` + `ApplyConfig` + `importer.Config`. 7 unit tests + 2 integration tests.
- [~] Agent ref resolution parity audit. Folded into Group 1: any missing output fields from `AgentSpec.ResolveRefs` vs legacy `ResolveAgentManifestSkills/Prompts` will surface as test failures during the handler collapse, at which point they get ported.
- [~] Version-lock policy on the apply handler. **Dropped**: on reread of legacy behavior, `versionutil.CompareVersions` is only used to decide `isNewLatest`, not to reject old-version applies. `Store.Upsert` + `pickLatestVersion` already preserve that semantic on the new side — no additional enforcement needed.
- [x] Delete `internal/registry/service/prompt/` + `testing/fake_registry.go` (B1.e). Handlers gone → no remaining consumers.
- [ ] Delete `internal/registry/service/{agent,server,skill,provider}/`. **Still blocked**: MCP registryserver (Group 9) consumes agent / server / skill; deployment service consumes agent / server / provider; platform/utils (Group 5) consumes agent + server. These services stay alive until their consumers port.

**Why**: handlers (Group 1) call services; until services are dissolved, Group 1 can't fully move to the generic handler.

---

## Medium-term

### 4. Group 1 — HTTP handlers collapse — **IN PROGRESS**

- [x] `resource.Register[T]` generic handler (`ec84636`).
- [x] Delete `internal/registry/api/handlers/v0/apply/` + `common/` (B1.a, commit `2f667eb`). Apply + delete consolidated onto `resource.RegisterApply` with DryRun / Force / DELETE.
- [x] Delete `handlers/v0/{agents,skills,prompts,providers}/` (B1.b, commit `8be4067`). 1,117 LOC removed.
- [x] Delete `handlers/v0/servers/` + `deploymentmeta/` (B1.c). `/v0/servers/...` retired in favor of `/v0/namespaces/{ns}/mcpservers/...`. deploymentmeta inline attachment dropped — UI drills into deployments separately.
- [ ] `server_readmes` BYTEA port — **deferred**. Legacy table still written by seeder/importer and readable by legacy MCP; no v1alpha1 equivalent. Revisit as either an MCPServerSpec field or a new sub-handler once the data model decision is made.
- [ ] Port deployment-specific endpoints (SSE watch, cancel, logs). Watch is a hard seam for Phase 2 KRT — keep `TableWatcher.ChangeHandler` signature stable.
- [x] Port apply-handler knobs: dry-run (B1.a). Force accepted as no-op under v1alpha1.
- [ ] Port embeddings-aware list: `?semantic=<q>` param.
- [x] Deploymentmeta attachment dropped (B1.c).

**Finish signal**: `router/v0.go` imports only `resource`, `health`, `ping`, `version`, `embeddings`, `deployments`. Currently carries all six. `deployments` leaves once B1.f lands.

### 5. Group 4 + 5 — Deployment service + platform adapters
**Status**: `DeploymentAdapter` interface + `noop` reference shipped (`4a6e1a6`). Native adapters NOT ported.

- [ ] Port `internal/registry/platforms/local/` — docker-compose YAML rendering, port allocation, agentgateway-in-front pattern for specs with remotes, restart policy, log-follow, compose down, volume purge (~1000 LOC).
- [ ] Port `internal/registry/platforms/kubernetes/` — Deployment + Service + ConfigMap templating via kagent CRDs, DNS-1123 name sanitization, label ownership, rollout-wait, pod log streaming (~1500 LOC).
- [ ] Port Provider adapter surface (List / Create / Get / Update / Delete Providers) per platform.
- [ ] Port deployment service: `DeployServer / DeployAgent` target-spec fetch + provider resolution; `LaunchDeployment` post-apply orchestration; `UndeployDeployment` cleanup; `GetDeploymentLogs / CancelDeployment` adapter callbacks; `Discovery` for out-of-band deployments.
- [ ] Decide per-platform deployment identity + principal storage (deferred post-refactor per plan).

**Finish signal**: `arctl apply -f examples/declarative/full-stack.yaml` converges to `Ready=True` via a native adapter.

### 6. Group 2 — Go client rewrite — **DONE**
**Status**: client speaks v1alpha1 generically for resource CRUD; deployment + embeddings RPCs still on legacy paths pending their own group ports.

- [x] Generic `Get / GetLatest / List / Apply / DeleteViaApply / Delete` returning `*v1alpha1.RawObject`. Typed consumers unmarshal `Spec` into the concrete `Spec` type.
- [x] Preserved: bearer token, configurable `http.Client`, 404 → `ErrNotFound` sentinel, pagination cursor forwarding via `ListOpts.Cursor` / return `nextCursor`.
- [x] 3 integration tests (`//go:build integration`): round-trip Apply→Get→GetLatest→List→Delete, apply-invalid per-doc failure, ErrNotFound.
- [~] `pkg/models/{agent,manifest,server_response,skill,prompt,deployment,provider}.go` deletion — **still blocked**. Consumers: MCP registryserver (Group 9), platform adapters + utils (Group 5), legacy postgres stores (Group 12), embeddings helpers (Group 8). Delete cascades as those groups port.
- [x] `internal/client/client_deprecated.go` stubs typed per-kind methods (GetServer / CreatePrompt / etc.) returning `errDeprecatedImperative` so the imperative CLI keeps compiling during the declarative-CLI-branch handover. Deleted when that branch merges.

**Finish signal**: `internal/client/client.go` under 400 LOC of kind-agnostic methods. Currently 534 LOC — the ~160 LOC of legacy deployment + embeddings RPCs come out once Groups 4 + 8 land, clearing the <400 bar.

### Further dedup opportunities (nice-to-have, not blocking)

Spotted during the review sweep but deliberately deferred:

- [ ] `resource/handler.go listInput` + `namespacedListInput` — 5 of 6 fields identical. Can share a common struct via Go embedding, but the handler comment explicitly flags why they were split (Huma reflects the whole input; a path tag on a cross-namespace list endpoint would make namespace mandatory there). Test embedding behaviour with Huma before collapsing.
- [ ] `pkg/api/v1alpha1/accessors.go` — 6 kinds × 7 accessor methods = ~80 lines of mechanical boilerplate (`GetAPIVersion / GetKind / SetTypeMeta / GetMetadata / SetMetadata / GetStatus / SetStatus`). Could collapse via embedding a shared base struct (`type typedBase struct { TypeMeta; Metadata ObjectMeta; Status Status }`) into every kind. Changes the public type shape though — scope carefully.
- [ ] `MarshalSpec / UnmarshalSpec` methods on the 6 kinds — one-line boilerplate each. Probably can't be collapsed without giving up typed Spec access at compile time.

---

## Longer-term

### 7. Group 8 — Embeddings indexer
- [ ] Port text assembly per kind onto a v1alpha1 field allowlist (title, description, content).
- [ ] Subscribe to generation-bump NOTIFY events for auto-regen.
- [ ] Manual reindex endpoint: `POST /v0/embeddings/index` with SSE progress stream.
- [ ] Semantic search at `?semantic=<q>&semanticThreshold=<f>`.

### 8. Group 9 — MCP protocol bridge
- [ ] Port `internal/mcp/` tools to v1alpha1 types (tool signatures are user-facing in Claude; the rename matters).
- [ ] Bridge MCP `listResources` to the new generic Store.

### 9. Groups 11 / 12 / 13 — Legacy deletion
- [ ] `pkg/models/*.go` — delete after Group 2 (client).
- [ ] `internal/registry/database/postgres_{agents,servers,skills,prompts,providers,deployments}.go` — delete after Group 4 (deployment service) + all other consumers cut over.
- [ ] `internal/registry/database/migrations/001..011*.sql` — keep in tree for dual-stack; remove at final cutover.
- [ ] `internal/registry/kinds/` — delete after Group 1 (apply handler) fully cuts over.
- [ ] Per-kind CLI CRUD files — delete when the declarative CLI branch merges (separate engineer owns this).

### 10. Group 14 — Enterprise extension points
- [ ] `v1alpha1.Scheme.Register(...)` at enterprise init for proprietary kinds.
- [ ] Enterprise syncer writes `discovered_*` tables in new shape.
- [ ] Enterprise platform adapters re-register via the new interface.
- [ ] Pin enterprise to pre-refactor OSS SHA until Group 6 (Phase 2 KRT rebase) lands; then single enterprise port PR.

### 11. Phase 2 rebase (separate branch, lands last)
- [ ] Rebase `internal-refactor-phase-2` (KRT reconciler, 6 commits, +11k/-9k) onto the post-refactor `main`.
- [ ] Wrap `models.Deployment` → `*v1alpha1.Deployment` at reconciler ingest; unwrap at DB write.
- [ ] Convert reconciler scalar-status checks (`d.Status != "deployed"`) into condition queries (`hasCondition(d.Status.Conditions, "Ready", "True")`).
- [ ] Repoint `UpdateDeploymentState` → `Store.PatchStatus`.
- [ ] Extend SSE watch handler to work over Scheme-registered kinds.

---

## What explicitly is NOT being done

- **Big-bang cutover.** Never. The nuclear approach was attempted once and reverted (preserved as tag `refactor/v1alpha1-types-cutover-reference`). Subsystem-at-a-time only.
- **Backwards-compat shims at end.** When a subsystem's port PR lands, its legacy code is gone. No parallel DTOs, no `//DEPRECATED` annotations.
- **Trivy / image CVE scanner.** Legacy `container_scan.go` is Docker Hub popularity metadata, not a security scanner. A real Trivy integration is net-new work; scope separately if needed.
- **Typed Provider config.** Still `map[string]any`. Revisit during the platform-adapter port.
- **CLI CRUD port.** Being replaced by a declarative CLI on a separate branch by another engineer. Workflow commands (`arctl agent run`, `arctl mcp build`, etc.) stay.

---

## Verification gate for "refactor complete"

From the plan file, the bar is:

1. `make build && make test-unit` green on OSS.
2. `arctl apply -f examples/declarative/full-stack.yaml` returns `applied` for every kind.
3. `arctl get agent <name> -o yaml` round-trips to valid `ar.dev/v1alpha1` including Status.
4. Reconciler writes `Type: "Ready", Status: "True"`; `status.observedGeneration == metadata.generation` after converge.
5. Reapply same YAML returns `unchanged`; generation stable.
6. Edit one spec field, reapply returns `configured`; generation +1; ObservedGeneration lags then catches up.
7. Reverse lookup `GET /v0/agents?mcpServerRef=<name>/<version>` hits the GIN index.
8. `arctl delete -f full-stack.yaml` clean.
9. `cmd/tools/gen-openapi` regen produces reviewed `openapi.yaml` diff.
10. `cd ui && npm run build` after TS client regen.
11. `Validated`, `RefsResolved`, `Published`, `Ready` observable in `status.conditions` at the right lifecycle points.

Currently passing: (1) partial — unit tests green. (3) partial — round-trip works for manifests but the CLI command that drives it hasn't been rebuilt. Everything else gated on Groups 1–6.
