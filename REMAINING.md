# What's Left — v1alpha1 Refactor Punch List

Companion to `REVIEW_GUIDE.md` (commit-grouped reading order) and
`DECISIONS.md` (design choices + the rationale behind each). `REBUILD_TRACKER.md` has the full per-subsystem inventory; this file is the 3-minute read.

Current branch: `refactor/v1alpha1-types` (~58 commits, **net −21k LOC**).

---

## What is now DONE

Completed on this branch; tracker entries deleted per its own "delete
once resolved" instruction.

| Tracker group | Outcome | Key commits |
|---|---|---|
| **Group 1** — HTTP handlers | Collapsed into `resource.Register[T]`; router imports only generic resource + health/ping/version + multi-doc apply + import. Legacy `/v0/{servers,agents,skills,prompts,providers,deployments}` handlers deleted. | `ec84636` `2f667eb` `8be4067` `a7ab3a4` `4be00e9` `2c2ff4e` |
| **Group 2** — Go client rewrite | Generic `Get/GetLatest/List/Apply/Delete` returning `*v1alpha1.RawObject`. Deployment + embeddings RPCs deleted. | `5aa8339` + cruft sweep |
| **Group 3** — Service-layer dissolution | `internal/registry/service/{agent,server,skill,prompt,provider,deployment-Registry}` gone. Only `V1Alpha1Coordinator` + `UnsupportedDeploymentPlatformError` remain under `service/deployment/`. | `20b25d4` `2cbf1c2` |
| **Group 4 + 5** — Deployment service + platform adapters | Local + Kubernetes adapters port to `types.DeploymentAdapter`; translate helpers shared via `platforms/utils`; `V1Alpha1Coordinator` drives Apply/Remove; legacy surface end-to-end delete. | `3ea8bc2` `c4bba08` `8d4c3ea` `7d02e9f` `4be00e9` `2c2ff4e` `7191848` |
| **Group 6** — Validators | `(Object).Validate / ResolveRefs / ValidateRegistries / ValidateUniqueRemoteURLs` on every kind; `registries.Dispatcher` owns NPM/PyPI/OCI/NuGet/MCPB. Legacy `internal/registry/validators/` deleted. | `81e732e` `4f2f212` `a6ea729` `020e2bf` `58401c7` |
| **Group 7** — Seed + importer | `pkg/importer/Importer` + OSV + Scorecard scanners; `POST /v0/import`; seed from disk via the new pipeline. Legacy `internal/registry/importer/` deleted (`048619f`). | `64ff130` `e2e6f8f` `0ec9297` `4e2afd7` `8d13ee4` `20abd73` `048619f` |
| **Group 9** — MCP protocol bridge | `NewServer(stores)` takes the v1alpha1 Store map. Tools return typed v1alpha1 envelopes; tool names preserved. Deploy/remove tools retired. | `bd9410c` |
| **Group 12** — Legacy DB interfaces | `pkg/registry/database` collapsed from ~268 → thin root contract (`Pool()` + `Close()`) plus sentinel errors. All postgres_*.go per-kind stores deleted. | `df5986c` + current checkout seam follow-up |
| **Group 13** — Legacy SQL migrations | `internal/registry/database/migrations/*.sql` (11 legacy files) + `migrations_vector/` deleted. Only `migrations_v1alpha1/` remains. | `df5986c` |
| **Group 8** — Embeddings / pgvector indexer | Restored on v1alpha1 in 5 commits after the cutover revealed the initial `2cbf1c2` deletion was an oversight, not a scope choice. `003_embeddings.sql` adds pgvector columns to Agent/MCPServer/Skill/Prompt. `Store.SetEmbedding` + `GetEmbeddingMetadata` + `SemanticList`. `internal/registry/embeddings/` Provider + Indexer with per-kind payload builders and checksum-based skip. `internal/registry/jobs/` async manager. `POST /v0/embeddings/index` + `GET /v0/embeddings/index/{jobId}` + `?semantic=<q>&semanticThreshold=<f>` on every list endpoint. `AGENT_REGISTRY_EMBEDDINGS_*` config restored. | `b112860` `34d72fb` `86a5482` `5be3de1` `c440525` |
| **Upstream apiv0.ServerJSON drop (platforms/utils)** | Per "everything should be v1alpha1" directive: `TranslateMCPServer` + helpers now operate on `v1alpha1.MCPServerSpec` directly. `platforms/` subtree imports zero upstream code. `go.mod` dep stays until the imperative CLI retires via declarative-CLI merge. | `b9f4d3f` |

---

## What's left

### Group 11 — legacy `pkg/models` / `internal/registry/kinds` cleanup
- [ ] Delete `pkg/models/{agent,manifest,server_response,skill,prompt,provider,deployment,apiversion}.go` and `internal/registry/kinds/` once the remaining workflow CLI surfaces are ported. `client_deprecated.go` is already gone in `709a23d`, and `pkg/types.ProviderPlatformAdapter` now speaks v1alpha1 resources; what remains is real usage in workflow CLI paths (`internal/cli/agent/*`, `internal/cli/mcp/manifest`, `internal/cli/scheme`) plus a few platform translation helpers.

### Group 8 — Embeddings indexer follow-ups
Core restored (see DONE table). These are incremental improvements:
- [ ] Subscribe to `v1alpha1_*_status` NOTIFY for auto-regen on spec change (currently indexing is manual via `POST /v0/embeddings/index`).
- [ ] SSE streaming variant of the index job (swap the pattern from `RegisterDeploymentLogs`).
- [ ] CLI `arctl embeddings index` subcommand — imperative CLI replacement work; ports with the declarative CLI cascade.
- [ ] Provider plug-in beyond OpenAI (Azure OpenAI / Ollama / local models). Provider interface exists; just need concrete impls.

### Group 14 — Enterprise extension points
- [ ] `v1alpha1.Scheme.Register(...)` for enterprise kinds at enterprise init.
- [ ] Enterprise Syncer writes `discovered_*` tables in the v1alpha1 shape.
- [ ] Enterprise platform adapters register via `AppOptions.DeploymentAdapters` (v1alpha1 interface) + `AppOptions.ProviderPlatforms`.
- [ ] Pin enterprise to pre-refactor OSS SHA until Phase 2 KRT rebase lands; then single enterprise port PR.

### Phase 2 rebase — **separate branch, lands last**
- [ ] Rebase `internal-refactor-phase-2` (KRT reconciler) onto post-refactor `main`.
- [ ] **Author fresh Phase 2 seam tables in `migrations_v1alpha1/`** — `reconcile_events` (audit trail + exponential-backoff state), `discovered_local`, `discovered_kubernetes`. The legacy Phase 2 migration `011_reconciler_schema.sql` anchored these on `deployments.id TEXT` (UUID row id); the v1alpha1 schema has no such column, so the rebase must redesign them with FKs against the composite identity `(namespace, name, version)` instead. Not present on this branch by design — the refactor branch owns the envelope + store; Phase 2 owns its own reconciler-specific tables.
- [ ] Replace synchronous `V1Alpha1Coordinator` path with NOTIFY-driven reconciliation.
- [ ] Condition-based status queries (`hasCondition(Conditions, "Ready", "True")`) instead of scalar status string.
- [ ] Status writes via `Store.PatchStatus`; finalizer drops via `Store.PatchFinalizers`.
- [ ] Update NOTIFY payload parsing — legacy payload was `{op, id=<uuid>}`; v1alpha1 payload is `{op, id=<namespace>/<name>/<version>}`. `TableWatcher.ChangeHandler` signature can stay but id parsing needs the split.
- [ ] SSE watch handler over Scheme-registered kinds.

### Nice-to-have dedup (non-blocking)
Spotted during review sweeps, deliberately deferred:
- [ ] `resource/handler.go listInput` + `namespacedListInput` — shared base via Go embedding. Huma interaction tests first.
- [ ] `pkg/api/v1alpha1/accessors.go` — 6 kinds × 7 accessor methods; could collapse via an embedded `typedBase` struct. Changes public type shape; scope carefully.
- [ ] `MarshalSpec / UnmarshalSpec` boilerplate on every kind — probably can't collapse without giving up typed Spec at compile time.

---

## What explicitly is NOT being done

- **Big-bang cutover.** Subsystem-at-a-time only (see `DECISIONS.md`).
- **Backwards-compat shims in the final state.** When a subsystem's
  port commit lands, its legacy code is gone. No parallel DTOs, no
  `// DEPRECATED` annotations. The temporary `client_deprecated.go`
  bridge was removed in `709a23d`; remaining legacy packages are live
  callers that still need a real port, not shims.
- **Trivy / image CVE scanner.** Legacy `container_scan.go` was Docker
  Hub popularity metadata, not a security scanner. Real Trivy is
  net-new work; scope separately.
- **Typed Provider config.** Still `map[string]any`. Revisit when a
  real need arises — deferred from platform adapter port (see
  `DECISIONS.md` §6).
- **CLI CRUD port.** Being replaced by a declarative CLI on a separate
  branch. Workflow commands (`arctl agent run`, `arctl mcp build`, etc.)
  stay.

---

## Verification gate for "refactor complete"

1. `make build && make test-unit` green. **✅ CURRENTLY PASSING.**
2. `arctl apply -f examples/declarative/full-stack.yaml` returns `applied` for every kind.
3. `arctl get agent <name> -o yaml` round-trips to valid `ar.dev/v1alpha1` including Status.
4. Reconciler writes `Type: "Ready", Status: "True"`; `status.observedGeneration == metadata.generation` after converge. **Gated on Phase 2.**
5. Reapply same YAML → `unchanged`; generation stable.
6. Edit one spec field, reapply → `configured`; generation +1; ObservedGeneration catches up.
7. Reverse lookup `GET /v0/agents?mcpServerRef=<name>/<version>` hits the GIN index.
8. `arctl delete -f full-stack.yaml` clean.
9. `cmd/tools/gen-openapi` regen produces reviewed `openapi.yaml` diff.
10. `cd ui && npm run build` after TS client regen.
11. `Validated`, `RefsResolved`, `Published`, `Ready` observable in `status.conditions` at the right lifecycle points.

Currently passing: **(1) fully.** Other gates blocked on the
declarative-CLI merge + Phase 2 KRT rebase — neither belongs to this
PR.
