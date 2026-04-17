# What's Left — v1alpha1 Refactor Punch List

Companion to `REVIEW_GUIDE.md`. `REBUILD_TRACKER.md` has the full per-subsystem inventory; this file is the 3-minute read.

Current branch: `refactor/v1alpha1-types` (31 commits, +9.9k / -0.4k).
Scope remaining: approximately 70% of the subsystem ports. The commits landed so far establish the v1alpha1 foundation + its generic Store + HTTP surface, the multi-doc apply endpoint, seeding, the adapter interface (no natives), the importer pipeline with OSV + Scorecard scanners, and a deduplication pass (`d31a56e`, `80c903a`) that collapsed the kind→store mapping and shared GitHub resolution across scanners.

Order below follows the dependency chain — earlier items unlock later ones.

---

## Near-term (unblock the rest)

### 1. Finish Group 7 (importer)
**Status**: pipeline + Scanner interface + OSV + Scorecard shipped. Wire-up and legacy deletion remain.

- [ ] Wire `importer.Importer` into `internal/registry/app.Bootstrap`. Construct with OSS scanners (OSV + Scorecard), register on the server.
- [ ] Add `POST /v0/import` HTTP endpoint, or route `arctl import` through the existing `/v0/apply` endpoint (decide based on CLI direction).
- [ ] Delete `internal/registry/importer/` wholesale once the above lands. The package currently carries ~2k LOC of legacy orchestration that has no callers after deletion.
- [ ] (Optional, scope separately) Port `container_scan.go` (Docker Hub popularity metadata) or `dependency_health.go` (deps.dev license rollups). Neither is a security scan; defer unless the UI needs the data.

**Why first**: closes a complete subsystem and removes the largest single legacy package. Makes the next port cleaner because there's less dead code.

### 2. Group 6 remainder — registry/URL validators
**Status**: structural port complete (`81e732e`). Registry-type and URL-allowlist validators still in `internal/registry/validators/`.

- [ ] Port OCI registry allowlist (Docker Hub variants, GHCR, Quay, public registries) into a spec-level validator, probably `pkg/api/v1alpha1/registry_validate.go` shared across kinds.
- [ ] Port NPM / PyPI / NuGet / MCPB "identifier exists in registry" checks. These hit external APIs — likely belong in a separate `(Spec).ValidateRegistry(ctx, client)` method that apply handlers can call with a shared rate-limited HTTP client.
- [ ] Port duplicate-URL detection (across packages + remotes within a single spec).
- [ ] Port per-Package `registryType` allowlist enforcement.

**Why next**: `obj.Validate()` is on the hot path for every apply. Leaving registry checks in the legacy validators package means the new path silently doesn't enforce them.

### 3. Group 3 — service-layer dissolution
**Status**: legacy service packages still carry business logic.

- [ ] Move `Agent.PublishAgent` URL-dedup logic onto `AgentSpec.Validate`.
- [ ] Move `Agent.ResolveAgentManifestSkills / ResolveAgentManifestPrompts` onto `AgentSpec.ResolveRefs` (partial — resolver is in place, but specific shape mapping may need work).
- [ ] Move `Server.PublishServer` duplicate-URL detection onto `MCPServerSpec.Validate`.
- [ ] Move version-lock semantics (`internal/registry/service/internal/versionutil`) onto either `Store.Upsert` or a shared `pkg/api/v1alpha1/versionlock.go` helper.
- [ ] Delete `internal/registry/service/{agent,server,skill,prompt,provider,set}/`, `testing/fake_registry.go`, `registry_service_test.go`.

**Why**: handlers (Group 1) call services; until services are dissolved, Group 1 can't fully move to the generic handler.

---

## Medium-term

### 4. Group 1 — HTTP handlers collapse
**Status**: generic `resource.Register[T]` shipped (`ec84636`). Legacy per-kind handlers still live.

- [ ] Delete `internal/registry/api/handlers/v0/{agents,servers,skills,prompts,providers,deployments}/` once Group 3 is done (handlers currently call services).
- [ ] Port `server_readmes` (rename to `mcpserver_readmes`, keep separate BYTEA table) — still legacy-backed.
- [ ] Port deployment-specific endpoints (SSE watch, cancel, logs). Watch is a hard seam for Phase 2 KRT — keep `TableWatcher.ChangeHandler` signature stable.
- [ ] Port apply-handler knobs: dry-run flag (preview without persisting); force flag (version-lock bypass).
- [ ] Port embeddings-aware list: `?semantic=<q>` param.
- [ ] Decide deploymentmeta attachment: keep inline summary on agent/server GET, or drop in favor of a separate deployments-for query.

**Finish signal**: `router/v0.go` imports only `resource`, `health`, `ping`, `version`, `embeddings`, `apply`.

### 5. Group 4 + 5 — Deployment service + platform adapters
**Status**: `DeploymentAdapter` interface + `noop` reference shipped (`4a6e1a6`). Native adapters NOT ported.

- [ ] Port `internal/registry/platforms/local/` — docker-compose YAML rendering, port allocation, agentgateway-in-front pattern for specs with remotes, restart policy, log-follow, compose down, volume purge (~1000 LOC).
- [ ] Port `internal/registry/platforms/kubernetes/` — Deployment + Service + ConfigMap templating via kagent CRDs, DNS-1123 name sanitization, label ownership, rollout-wait, pod log streaming (~1500 LOC).
- [ ] Port Provider adapter surface (List / Create / Get / Update / Delete Providers) per platform.
- [ ] Port deployment service: `DeployServer / DeployAgent` target-spec fetch + provider resolution; `LaunchDeployment` post-apply orchestration; `UndeployDeployment` cleanup; `GetDeploymentLogs / CancelDeployment` adapter callbacks; `Discovery` for out-of-band deployments.
- [ ] Decide per-platform deployment identity + principal storage (deferred post-refactor per plan).

**Finish signal**: `arctl apply -f examples/declarative/full-stack.yaml` converges to `Ready=True` via a native adapter.

### 6. Group 2 — Go client rewrite
**Status**: 1800 LOC of typed per-kind methods in `internal/client/client.go`.

- [ ] Replace typed methods with generic `Get / GetLatest / List / Apply / Delete / PatchStatus` speaking `*v1alpha1.Object`.
- [ ] Preserve: bearer token support, configurable `http.Client`, `404 → ErrNotFound` mapping, pagination cursor forwarding.
- [ ] Delete `pkg/models/{agent,manifest,server_response,skill,prompt,deployment,provider,metadata,apiversion}.go` once client + handlers no longer reference them.

**Finish signal**: `internal/client/client.go` is under 400 LOC of kind-agnostic methods; integration tests PUT a v1alpha1 object and GET it back.

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
