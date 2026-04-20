# v1alpha1 Port Tracker

> **Purpose**: every legacy subsystem that currently speaks `pkg/models` DTOs
> and per-kind database stores must be ported to speak `pkg/api/v1alpha1`
> types and the generic `database.Store`. This tracker enumerates each
> subsystem, its current state, the business logic that must survive the
> port, and the finish-line signal.
>
> **Approach**: no backwards compatibility. Each subsystem PR replaces old
> code with new code speaking v1alpha1 end-to-end. At PR merge, the old
> code for that subsystem is gone and the new code is serving it.
>
> **Delete an entry** once that subsystem's port PR has merged.
> **Delete this file** once every entry is resolved.

**Branch state**:
- Current: `refactor/v1alpha1-types` (commits `d54c8ef..a45a439`) — purely
  additive v1alpha1 foundation atop main. Legacy code 100% intact.
- Reference tag: `refactor/v1alpha1-types-cutover-reference` — the earlier
  nuclear-cutover attempt (15k+ LOC deleted). Preserved for cribbing; not
  a base to build on. Inspect with
  `git log refactor/v1alpha1-types-cutover-reference --oneline`.

**Generic primitives already in place** (use these as port targets):
- `pkg/api/v1alpha1` — typed envelopes (Agent, MCPServer, Skill, Prompt,
  Provider, Deployment), Object interface, Scheme, Conditions, ResourceRef.
- `internal/registry/database.Store` — generic per-table CRUD over v1alpha1
  rows with Upsert (generation + semver is_latest), PatchStatus
  (disjoint from spec), List, FindReferrers, GetLatest.
- `internal/registry/database/migrations_v1alpha1/001_v1alpha1_schema.sql`
  — v1alpha1 tables under a dedicated PostgreSQL schema (`v1alpha1.*`).
  Coexists with `public.*` legacy tables.
- `internal/registry/api/handlers/v0/resource` — generic `Register[T]`
  wiring GET/GET-latest/LIST/PUT/DELETE endpoints for one kind.

---

## 1. HTTP handlers → generic resource handler

**Current state (legacy)**:
- `internal/registry/api/handlers/v0/agents/handlers.go` + `deployment_handlers.go`
- `internal/registry/api/handlers/v0/servers/handlers.go` + `edit.go` +
  `deployment_handlers.go`
- `internal/registry/api/handlers/v0/skills/handlers.go`
- `internal/registry/api/handlers/v0/prompts/handlers.go`
- `internal/registry/api/handlers/v0/providers/handlers.go`
- `internal/registry/api/handlers/v0/deployments/handlers.go`
- `internal/registry/api/handlers/v0/deploymentmeta/meta.go` (helper)
- `internal/registry/api/handlers/v0/apply/handler.go` (multi-doc apply)
- `internal/registry/api/router/v0.go` (wires them all)

**Port plan**:
- Replace per-kind Register funcs at `/v0/{agents,servers,skills,prompts,providers,deployments}`
  with `resource.Register[*v1alpha1.X]` wired against `database.Store`.
- Rewrite `apply/handler.go` to use `v1alpha1.Scheme.DecodeMulti` + per-kind
  Store dispatch.
- Rewrite deployment-specific endpoints (watch SSE, cancel, edit) to accept
  v1alpha1 envelopes.

**Business logic to preserve**:
- [ ] Apply handler: multi-doc YAML / JSON decode + per-doc status report.
- [ ] Apply handler: dry-run flag (returns diff without persisting).
- [ ] Apply handler: force flag (bypasses version-lock check).
- [ ] Servers handler: README attach + fetch on publish
      (`SetServerReadme` business logic).
- [ ] Server edit endpoint semantics (PUT vs PATCH distinction — plan
      resolved to apply=publish, so this may go away entirely).
- [ ] Deployments handler: SSE watch endpoint (hard seam for reconciler;
      already status-only NOTIFY in v1alpha1 schema).
- [ ] Deployments handler: cancel + logs endpoints (callbacks to platform
      adapter).
- [ ] Embeddings-aware list: semantic search via `?semantic=<q>`.
- [ ] Deploymentmeta attachment: inline deployment summary on agent/server
      GET responses (design decision pending — could drop in favor of
      a separate deployments-for query).

**Finish-line signal**: `router/v0.go` no longer imports any handler
package other than `resource`, `health`, `ping`, `version`. All prior
per-kind handler packages deleted.

---

## 2. Go client → generic client

**Current state (legacy)**:
- `internal/client/client.go` — 1800+ LOC of typed per-kind methods
  (`GetAgent`, `CreateAgent`, `GetSkill`, `GetServerReadme`, `DeployAgent`,
  `CancelDeployment`, etc.). Speaks `pkg/models` types on the wire.
- `internal/client/client_test.go`, `client_integration_test.go`.

**Port plan**:
- Replace with generic `Get / GetLatest / List / Apply / Delete`
  plus `PatchStatus` if any client-side use case exists (doubtful; status
  is server-managed).
- Reference implementation available in the cutover tag:
  `git show refactor/v1alpha1-types-cutover-reference:internal/client/client.go`.

**Business logic to preserve**:
- [ ] Bearer token support.
- [ ] Configurable http.Client (timeout, TLS).
- [ ] 404 → `ErrNotFound` mapping so callers can type-switch.
- [ ] Pagination cursor forwarding on List.

**Finish-line signal**: `internal/client/client.go` is <400 LOC of
kind-agnostic methods; integration tests PUT a v1alpha1 object and GET
it back.

---

## 3. Service-layer thin wrappers → validation functions

**Current state (legacy)**:
- `internal/registry/service/agent/service.go` + tests
- `internal/registry/service/server/service.go` + tests
- `internal/registry/service/skill/service.go` + tests
- `internal/registry/service/prompt/service.go` + tests
- `internal/registry/service/provider/service.go` + tests + `adapters.go`
- `internal/registry/service/testing/fake_registry.go`
- `internal/registry/service/registry_service_test.go`
- `internal/registry/service/test_helpers_internal_test.go`

**Port plan**:
Each per-kind service has two kinds of code in it:
1. **Pass-through to the per-kind DB Store** — deleted with the Store.
2. **Business logic** — validation (name regex, URL allowlist, duplicate-URL
   detection), version-lock, embedding-regen triggers, reference resolution
   (agent→skills, agent→prompts).

Extract (2) into:
- `pkg/api/v1alpha1/<kind>.go` gets a `Validate() error` method with the
  validation rules currently in `internal/registry/validators/`.
- `pkg/api/v1alpha1/agent.go` gets a method like
  `(AgentSpec).ResolveRefs(ctx, store) error` that checks MCPServer/Skill/
  Prompt refs exist.
- Apply handler calls `obj.Validate()` then `store.Upsert()`; embedding
  regen fires on the status-change NOTIFY stream.

**Business logic to preserve**:
- [x] Agent / Server / Skill: duplicate remote URL detection. Ported as
      `(Object).ValidateUniqueRemoteURLs` + `database.NewV1Alpha1UniqueRemoteURLsChecker`
      backed by `Store.FindReferrers`; cross-namespace, per-Kind.
- [ ] Agent.ResolveAgentManifestSkills / ResolveAgentManifestPrompts
      parity — folded into Group 1; audited during handler collapse.
- [ ] Server.UpdateServer: idempotent patch-status pattern.
- [ ] Provider.RegisterProvider: platform-adapter dispatch.
- [ ] Provider.PlatformAdapters() registration API (enterprise extension).
- [x] Version-lock semantics — kept as helper
      (`v1alpha1.CompareVersions`). Legacy never rejected old-version
      applies; it only decided which row was `isLatest`. `Store.Upsert`
      + `pickLatestVersion` already preserve that semantic — no extra
      enforcement on the apply path.

**Finish-line signal**: `internal/registry/service/{agent,server,skill,prompt,provider}/`
and `testing/`, `set/`, `registry_service_test.go`,
`test_helpers_internal_test.go` all deleted; their validation is on
v1alpha1 Spec types; their version-lock is either in `Store.Upsert` or a
`pkg/api/v1alpha1` helper.

---

## 4. Deployment service → reconciler-facing coordinator

**Current state**:
- `internal/registry/service/deployment/service.go` + tests
- `internal/registry/service/deployment/records.go` + tests
- `internal/registry/service/deployment/discovery.go` + tests
- `internal/registry/service/deployment/discovered_id_test.go`

This service is NOT a thin wrapper — it orchestrates:
- Fetch resource (Agent or MCPServer) for a deployment request
- Dispatch to platform adapter (local/kubernetes)
- Persist deployment record + lifecycle state
- Discovery: enumerate deployments that already exist out-of-band

**Port plan**:
- Keep as a service (not a thin wrapper).
- Accept `*v1alpha1.Deployment` in, translate to platform-adapter calls.
- Read target via `store.Get(templateRef.Kind, templateRef.Name, templateRef.Version)`.
- Write status via `store.PatchStatus` — NOT via a separate Update method.
- Discovery still enumerates but writes to `discovered_*` tables (or
  mirrors them into v1alpha1 envelopes — decision needed at port time).

**Business logic to preserve**:
- [ ] DeployServer / DeployAgent: fetch target spec, resolve provider,
      call platform adapter, record result.
- [ ] LaunchDeployment: post-apply orchestration (adapter.Deploy).
- [ ] UndeployDeployment: adapter-specific cleanup (docker compose down
      / kubectl delete).
- [ ] GetDeploymentLogs / CancelDeployment: adapter callbacks.
- [ ] Discovery: reconcile out-of-band deployments detected by Syncer
      (enterprise surface).
- [ ] Idempotency: applying the same Deployment shouldn't re-run the
      platform adapter if nothing changed.

**Finish-line signal**: `deploymentsvc.New(...)` takes stores + adapter
map, all methods accept/return v1alpha1 types. Integration test:
apply a Deployment CR → local adapter spawns docker-compose → logs
streamable → cancel tears down.

---

## 5. Platform adapters (local + kubernetes) — **SURVEY DONE, READY TO PORT**

**Current state**:
- `internal/registry/platforms/local/deployment_adapter_local.go` (367 LOC)
  + `..._platform.go` (665 LOC) + tests — satisfies legacy
  `registrytypes.DeploymentPlatformAdapter` with Deploy / Undeploy /
  GetLogs / Cancel / CleanupStale / Discover methods that speak
  `*models.Deployment`.
- `internal/registry/platforms/kubernetes/deployment_adapter_kubernetes.go`
  + `..._platform.go` + tests — same shape, kagent + kmcp CRDs.
- `internal/registry/platforms/types/types.go` +
  `agentgateway_types.go` (~450 LOC of gateway config shapes). These
  are the adapter-internal DSL; they stay on legacy types during the
  port.
- `internal/registry/platforms/utils/deployment_adapter_utils.go`
  (~720 LOC) — `BuildPlatformMCPServer`, `ResolveAgent`,
  `TranslateMCPServer`, env/arg processing, `GenerateInternalNameForDeployment`.
  Six call sites into agent/server services via this file.
- `pkg/types/adapter_v1alpha1.go` — **NEW** interface already shipped
  (commit `4a6e1a6`) with Apply / Remove / Logs / Discover + typed
  input/output structs returning `[]v1alpha1.Condition`.
- `internal/registry/platforms/noop/adapter.go` — reference impl of
  the new interface; 106 LOC.

**Port plan — sub-commits (each a PR)**:

**5.a — Local adapter port (first)**. Add `Apply` / `Remove` / `Logs` /
`Discover` / `SupportedTargetKinds` methods to `localDeploymentAdapter`
*alongside* the legacy `Deploy` / `Undeploy` / etc. so it satisfies
both interfaces. New translation helpers live in a new file
(`local/v1alpha1_translate.go`) that converts `v1alpha1.MCPServerSpec`
→ `platformtypes.MCPServer` and `v1alpha1.AgentSpec` → `platformtypes.Agent`,
reading nested refs via `ApplyInput.Resolver` instead of `serverService`
/ `agentService`. Returns `[]v1alpha1.Condition` with `Progressing=True`
on success; async convergence watch deferred to Phase 2 KRT. Tests
exercise round-trip against docker-compose shell-out seams (fakeable
via `runLocalComposeUp` global).

**5.b — Kubernetes adapter port**. Same pattern as 5.a. kagent CRDs
are controller-runtime objects; translation produces the same
`*v1alpha2.Agent` / `*kmcpv1alpha1.MCPServer` / `*corev1.ConfigMap`
graph, just fed from v1alpha1 specs. `sanitizeKubernetesName`,
`kubernetesDeploymentManagedLabels`, `deploymentNamespace` stay.

**5.c — Platform/utils v1alpha1 helpers**. New `utils/resolve_v1alpha1.go`
with `ResolveAgentV1Alpha1` + `BuildPlatformMCPServerV1Alpha1` calling
through `ResolverFunc` instead of the legacy services. Keeps legacy
funcs alive until deployment service ports (5.d).

**5.d — Deployment service port (Group 4 rolls in)**. Rewrite
`internal/registry/service/deployment/` to speak `*v1alpha1.Deployment`
on its public surface. Registry interface collapses — `LaunchDeployment`,
`ApplyAgentDeployment`, `ApplyServerDeployment`, `UndeployDeployment`
all accept/return v1alpha1 types. Calls adapters via the new interface
(`types.DeploymentAdapter`). Status writes via `Store.PatchStatus`
with `[]v1alpha1.Condition` merged. Transaction-locked idempotent
upsert + drift detection preserved.

**5.e — Deployment HTTP handlers (B1.f)**. Port
`internal/registry/api/handlers/v0/deployments/handlers.go` to speak
v1alpha1 on the wire. Six routes: list / get / create / delete /
logs / cancel. No SSE watch endpoint currently (research confirmed —
that's a Phase 2 KRT addition, not an existing seam). Delete
`apitypes.DeploymentRequest` + `models.Deployment` wire types.

**5.f — Legacy adapter + platform/utils + service deletion cascade**.
After 5.a–e, drop legacy Deploy/Undeploy/GetLogs/Cancel/Discover
methods from both adapters; delete `platforms/utils/deployment_adapter_utils.go`
legacy functions; delete `pkg/registry/database.DeploymentStore`
legacy interface + postgres_deployments.go; delete
`internal/registry/service/{agent,server,provider}/` (last
consumers gone); delete `pkg/models/{agent,manifest,server_response,
skill,prompt,deployment,provider}.go`.

**Sequencing reason**: each sub-commit leaves both stacks runnable.
Adapter ports (5.a/5.b) ship first because the v1alpha1 interface
exists + noop works — adapters can satisfy both interfaces during
transition. Service port (5.d) lands after adapters so it has a new
interface to call. Handlers (5.e) last because they own the external
wire contract and break UI if rushed.

**Adapter-Interface coexistence pattern**: one struct satisfies both
`registrytypes.DeploymentPlatformAdapter` (legacy) and
`pkg/types.DeploymentAdapter` (new) until 5.f. Compile-time
assertions:
```go
var _ registrytypes.DeploymentPlatformAdapter = (*localDeploymentAdapter)(nil)
var _ types.DeploymentAdapter               = (*localDeploymentAdapter)(nil)
```

**What the survey uncovered (clarifications to upstream plan)**:
- **No SSE watch endpoint exists** in current code. `REBUILD_TRACKER.md` earlier noted
  "SSE watch endpoint" as a hard seam — actual state: no such handler.
  Phase 2 KRT owns the future watch surface; no byte-identical wire
  contract to preserve here. Simplifies 5.e.
- **Deployment service is heavy orchestrator** (not a thin wrapper):
  `LaunchDeployment`, `ApplyAgentDeployment`/`ApplyServerDeployment`
  do transaction-locked idempotent upsert with drift detection +
  cleanup + adapter dispatch. Preserve that behavior through the
  port; it's load-bearing.
- **Discovery (adapter-side)**: only kubernetes has a real Discover;
  local returns empty slice. New `DiscoveryResult` shape allows
  either.

**Business logic to preserve**:
- [ ] Local: docker-compose YAML rendering with proper port allocation,
      env merge, volume mounts.
- [ ] Local: agentgateway-in-front-of-agent pattern when spec has
      MCPServer refs.
- [ ] Local: restart policy, `docker compose logs --follow`,
      `docker compose down`.
- [ ] Kubernetes: Deployment + Service + ConfigMap templating using
      controller-runtime client.
- [ ] Kubernetes: label ownership with deployment name + generation.
- [ ] Kubernetes: wait for rollout ready, surface pod status.
- [ ] Kubernetes: log streaming via pod exec, namespaced cancellation.
- [ ] Name-sanitization rules (DNS-1123 on k8s side, docker compose
      service naming on local side).
- [ ] Port allocation that doesn't collide with existing local deployments.
- [ ] Provider adapter interface (List/Create/Get/Update/Delete Providers)
      so enterprise can plug in cloud providers.

**Finish-line signal**: integration test applies a Deployment CR targeting
a real MCPServer; platform adapter spawns; `kubectl get pods` or
`docker ps` shows running workload; status.conditions[?].Type=="Ready"
flips True; cancellation tears it down cleanly.

---

## 6. Validators — **STRUCTURAL PORTED** (commit 81e732e, 2026-04-17)

Per-kind `Validate()` + `ResolveRefs()` now live on every v1alpha1 typed
envelope and are called by the generic resource handler's PUT path.
Config.Resolver plugs in an optional cross-kind existence checker.

**Ported**: name/namespace/version format, URL (https-only), repository
source (git), label/finalizer shape, per-kind spec structural checks,
ResourceRef kind allowlists (MCPServers must be MCPServer, TargetRef
must be Agent/MCPServer, ProviderRef must be Provider), namespace
inheritance on blank ref namespaces. 28 unit tests + 3 integration
tests green.

**Still deferred**: HTTP-based external-registry validation
(NPM/PyPI/NuGet/OCI/MCPB identifier probing), duplicate-URL detection
across packages+remotes, `_meta.publisherProvided` size limit, namespace
↔ domain mapping, auto-write of `Validated` condition on apply. These
come back in follow-up PRs — legacy code for them still lives in
`internal/registry/validators/registries/` and gets restored into
`pkg/api/v1alpha1/validation/registries/` when needed.

**Legacy files still present** (owned by per-kind services not yet
ported — Group 3):

- `internal/registry/validators/validators.go` — `ValidateAgentJSON`,
  `ValidateServerJSON`, `ValidateSkillJSON`, `ValidatePromptJSON` (~600 LOC).
- `internal/registry/validators/utils.go` — URL check, slug, semver helpers.
- `internal/registry/validators/constants.go`, `package.go`.
- `internal/registry/validators/registries/` — per-registry package
  validators: `npm.go`, `oci.go`, `pypi.go`, `nuget.go`, `mcpb.go` + tests.
- `pkg/validators/names.go` — public name-format validator.

**Port plan**:
- Move kind-level validation into `(AgentSpec).Validate() error` on
  v1alpha1 typed specs.
- `validators/registries/` stays as a subpackage — the per-registry
  package-existence checks are independent of our type model. Move to
  `pkg/api/v1alpha1/validation/registries/` (or similar).
- `pkg/validators/names.go` becomes `pkg/api/v1alpha1/names.go` or
  inlines into ObjectMeta validation.

**Business logic to preserve**:
- [ ] Name regex: `^[a-z0-9][-a-z0-9./]*[a-z0-9]$` or similar, length 1..N.
- [ ] Version: opaque string OR valid semver, max length.
- [ ] URL: https-only unless localhost; no userinfo; no fragment.
- [ ] Icon src URL validation (http(s), no fragment).
- [ ] Per-Package registryType allowlist (oci, npm, pypi, nuget, mcpb,
      pascal's-helpdesk, etc.).
- [ ] OCI: registry allowlist (Docker Hub variants, GHCR, Quay, public
      GCR, public ECR), HEAD request with manifest v2 + v1 fallback,
      rate-limit detection.
- [ ] NPM: identifier exists + version in registry metadata.
- [ ] PyPI: identifier exists + version in JSON API.
- [ ] NuGet: V3 API package registration check.
- [ ] MCPB: archive checksum validation against registry metadata.
- [ ] Agent.Spec.McpServers / Skills / Prompts each ref must resolve to
      an existing v1alpha1 row (currently done in service/agent).
- [ ] Deployment.Spec.TargetRef must resolve; ProviderRef must resolve.
- [ ] Duplicate-URL detection across packages + remotes of a single spec.

**Finish-line signal**: apply handler calls `obj.Spec.Validate(ctx, store)`
before persist; failures return 400 with structured error listing every
violation; `Validated` condition written to status; integration test
covers every validator's happy + unhappy path.

---

## 7. Seed + importer pipelines

**Status**:
- Seed loader ported (`internal/registry/seed/builtin_v1alpha1.go`, ee07520).
- v1alpha1 importer + Scanner interface + FindingsStore landed
  (64ff130 + e2e6f8f). Core pipeline (decode → validate → enrich →
  upsert → findings-write) covered by 13 integration tests.
- **Native scanner ports still pending** (see below).

**Landed**:
- `pkg/importer/scanner.go` — Scanner interface + EnrichmentPrefix
  vocabulary + PromotedToLabels list.
- `pkg/importer/findings_store.go` — atomic replace-per-source writer
  over `v1alpha1.enrichment_findings`.
- `pkg/importer/importer.go` — `Importer.Import(ctx, Options) []ImportResult`
  with dry-run, namespace defaulting, WhichScans filtering.
- `internal/registry/database/migrations_v1alpha1/002_enrichment_findings.sql`.

**Legacy still in tree (to delete once port targets land)**:
- `internal/registry/importer/importer.go` (1535 LOC)
- `internal/registry/importer/container_scan.go` — Trivy shell-out
- `internal/registry/importer/dependency_health.go` — endpoint probes
- `internal/registry/importer/osv_scan.go` — OSV API client
- `internal/registry/importer/scorecard_lib.go` — OpenSSF scorecard

**Native scanner ports**:
- [x] `pkg/importer/scanners/osv/osv.go` — OSV.dev batch query.
      `git mv osv_scan.go` (commit `0ec9297`) preserves blame
      lineage back to the original `#18 Enrich server data with import`
      commit; Scanner wrap + test seams follow in `d2ba661` / `69eaf58`;
      14 unit tests in `4ad509c`.
- [x] `pkg/importer/scanners/scorecard/scorecard.go` — OpenSSF
      Scorecard wrap. `git mv scorecard_lib.go` (commit `4e2afd7`,
      99% similarity) preserves blame; Scanner wrap follows in
      `3f65d84`; 12 unit tests in `a44cad1`.
- [ ] `pkg/importer/scanners/container/` — NOT a direct port.
      Legacy `container_scan.go` is a Docker Hub metadata fetcher
      (pull count, last-updated), not Trivy. A real image CVE
      scanner is net-new work; scope separately if needed.
- [ ] `pkg/importer/scanners/dephealth/` — Legacy
      `dependency_health.go` does deps.dev license/ecosystem
      rollups, not CVE scanning. Non-security. Port only if the UI
      needs the metadata; currently not wired.

**Wiring**:
- [ ] `internal/registry/app.Bootstrap` constructs the Importer with
      the OSS scanner set and registers it on the server.
- [ ] HTTP endpoint `POST /v0/import` (or reuse `arctl import` against
      the apply endpoint, depending on CLI direction).
- [ ] Enterprise builds register proprietary scanners through
      `importer.Config.Scanners`.

**Finish-line signal**: `arctl import --from <dir> --enrich`
populates rows + writes findings; UI drill-down reads
`v1alpha1.enrichment_findings`; label filters by osv-status +
scorecard-bucket work end-to-end.

---

## 8. Embeddings / pgvector indexer

**Current state**:
- `internal/registry/embeddings/provider.go` — OpenAI provider interface.
- `internal/registry/embeddings/openai.go` — OpenAI-backed impl.
- `internal/registry/embeddings/helpers.go` — text-assembly utilities.
- `internal/registry/service/indexer.go` — the indexer.
- `internal/registry/service/indexer_test.go`.
- `internal/registry/api/handlers/v0/embeddings/handlers.go` + `sse.go` + tests.
- `internal/registry/jobs/manager.go` + `types.go` — async job tracker.
- pgvector migration still applies `semantic_embedding` columns to
  public.* tables (legacy). v1alpha1 schema does NOT yet have those
  columns.

**Port plan**:
- Add `semantic_embedding` + `semantic_embedding_provider` + `..._model` +
  `..._dimensions` columns to every v1alpha1.* table (via an additive
  migration 002_v1alpha1_embeddings.sql).
- Indexer reads v1alpha1 rows via Store, generates embedding, writes
  via `store.SetEmbedding(name, version, vec)` (new method).
- SSE index endpoint remains but targets v1alpha1 rows.
- Subscribe to per-table status-change NOTIFY for incremental reindex
  on spec change (decision per plan).

**Business logic to preserve**:
- [ ] Text assembly per-kind: which fields contribute to the embedding
      input (title, description, content for Prompt, etc.). This differs
      per kind.
- [ ] Embedding regen on spec change (generation bump triggers it).
- [ ] Manual reindex of all rows via POST /v0/embeddings/index.
- [ ] Progress streaming via SSE with job state machine.
- [ ] Semantic search: cosine distance query at `?semantic=` param in
      list endpoints.
- [ ] Threshold filter at `?semanticThreshold=` param.
- [ ] Provider abstraction so Azure OpenAI / Ollama / local models can
      plug in.

**Finish-line signal**: `arctl embeddings index --watch` surfaces progress;
`GET /v0/agents?semantic=payments` returns relevance-ordered matches.

---

## 9. MCP protocol bridge

**Current state**:
- `internal/mcp/registryserver/server.go` — exposes registry as MCP tools
  for Claude/other MCP clients.
- `internal/mcp/registryserver/deployments_test.go`
- `internal/mcp/registryserver/server_integration_test.go`
- `internal/mcp/registryserver/server_tools_test.go`

MCP tools include: `list_servers`, `get_server`, `deploy_server`,
`cancel_deployment`, `list_deployments`, etc.

**Port plan**:
- Tools now call `database.Store` directly, returning v1alpha1 objects
  JSON-marshaled for the MCP tool response.
- Deployment-orchestration tools call the ported deployment service.

**Business logic to preserve**:
- [ ] Tool signatures + descriptions (they're user-facing in Claude).
- [ ] Auth threaded through: session controls what the MCP caller can
      read/write.
- [ ] Streaming responses (if any tool uses them).

**Finish-line signal**: Claude Code connected via MCP lists/gets/deploys
against the registry successfully.

---

## 10. CLI (`arctl`) — **SKIP**

Per your note, the legacy CLI is being replaced by a different engineer on
a separate branch with a declarative-only model (`arctl apply -f` being
the primary verb). We do NOT port the per-kind commands here.

**What to do at port time**: when other-engineer's CLI branch lands, the
whole `internal/cli/` + `pkg/cli/` + `cmd/cli` tree goes away and is
replaced by their generic implementation.

**Preserved content** (restore when needed):
- `internal/cli/agent/frameworks/adk/python/templates/` (already
  restored in the cutover-reference tag under
  `d04d03d`; retrieve with
  `git checkout refactor/v1alpha1-types-cutover-reference -- internal/cli/agent/frameworks/adk/python/templates`).
- `internal/cli/mcp/frameworks/{golang,python}/templates/` (same).
- `internal/cli/skill/templates/hello-world/` (same).

---

## 11. pkg/models DTOs

**Current state**:
- `pkg/models/agent.go` — `AgentJSON`, `AgentResponse` wrappers.
- `pkg/models/manifest.go` — `AgentManifest`, `SkillRef`, `PromptRef`,
  `McpServerType`, `RegistryRef`.
- `pkg/models/server_response.go` — `ServerJSON` with MCP registry
  upstream shape.
- `pkg/models/skill.go` — `SkillJSON`, etc.
- `pkg/models/prompt.go` — `PromptJSON`.
- `pkg/models/provider.go` — `Provider`, `CreateProviderInput`, ...
- `pkg/models/deployment.go` + test — `Deployment`, `DeploymentActionResult`,
  `DeploymentFilter`.

**Port plan**:
- These stay alive until every subsystem that imports them has been
  ported. Retire module-by-module as imports disappear.
- When the last consumer switches to v1alpha1, delete the file.

**Finish-line signal**: `grep -r pkg/models internal pkg cmd` returns no
non-test hits → delete the package.

---

## 12. Legacy database interfaces + per-kind postgres stores

**Current state**:
- `pkg/registry/database/database.go` — `AgentStore`, `ServerStore`,
  `SkillStore`, `PromptStore`, `ProviderStore`, `DeploymentStore`,
  `Scope`, `Transactor`, filter types.
- `internal/registry/database/postgres_agents.go` (and 5 siblings) —
  implementations.
- `internal/registry/database/postgres.go` — `PostgreSQL` root struct
  with `Servers()`/`Agents()`/etc.
- `internal/registry/database/postgres_test.go`.

**Port plan**:
- Each subsystem port above flips its DB access from
  `scope.Agents().GetAgent(...)` to `store.Get(ctx, "v1alpha1.agents", name, version)`.
- Once no caller references `scope.Agents()` etc., delete
  `postgres_agents.go`.
- Once all six per-kind postgres_*.go files are gone, delete the Scope
  plumbing and the Store interfaces in `pkg/registry/database/`.
- `PostgreSQL.rootScope` and the transaction-scoping code go away too;
  the generic Store handles transactions internally per-call.

**Finish-line signal**: `pkg/registry/database/database.go` is only
sentinel errors + migrator config; `internal/registry/database/` has
`postgres.go` (pool + migrate), `migrate.go`, `store_v1alpha1*.go`,
`testutil.go` — nothing else.

---

## 13. Legacy SQL migrations (public.*)

**Current state**:
- `internal/registry/database/migrations/001_initial_schema.sql` through
  `010_drop_platform_validity_constraints.sql`.
- All create/modify tables in the `public` schema.

**Port plan**:
- Keep running during the port so legacy code works.
- At final cutover (every subsystem ported, legacy code deleted): replace
  the migrations/ directory with a single migration that drops the old
  tables and promotes `v1alpha1.*` → `public.*` (either by rename or by
  keeping the schema prefix and updating Store callers).

**Finish-line signal**: `migrations/` contains only the v1alpha1 schema
+ any additive v1alpha1 migrations that came after (e.g. embeddings).

---

## 14. OSS/enterprise extension points

**Current state**: `pkg/types.AppOptions` exposes
- `DatabaseFactory DatabaseFactory` — wraps/extends the base store.
- `ProviderPlatforms map[string]ProviderPlatformAdapter`
- `DeploymentPlatforms map[string]DeploymentPlatformAdapter`
- `ExtraRoutes func(api huma.API, pathPrefix string)`
- `HTTPServerFactory`, `OnHTTPServerCreated`, `UIHandler`, `AuthnProvider`,
  `AuthzProvider`.

**Port plan**:
- `DatabaseFactory` signature stays (it wraps the base store; enterprise
  uses it for migrations + authz). Either (a) the legacy `database.Store`
  interface stays in `pkg/registry/database` with the old methods and
  enterprise adapts, or (b) we redefine `database.Store` as our new
  generic Store type and enterprise updates. Decide during port.
- `ProviderPlatformAdapter` and `DeploymentPlatformAdapter` interfaces
  get new method signatures on `*v1alpha1.*` types (covered in Group 5).
- Enterprise kind registration via `v1alpha1.Scheme.Register` (already
  shipped).
- `ExtraRoutes` unchanged — huma-level hook.

**Business logic to preserve**:
- [ ] Enterprise must be able to plug in additional kinds, register
      additional platform adapters, and wrap the database from outside
      the OSS module.
- [ ] Enterprise's Syncer writes to `discovered_local` /
      `discovered_kubernetes` tables — needs those tables preserved
      through the migration.

**Finish-line signal**: enterprise builds against the v1alpha1 OSS
main branch with zero code changes other than updated adapter
signatures + kind registration calls.

---

## Suggested port order

Cheapest / most independent first, so we rack up wins and unblock
later work:

1. **Validators** (Group 6) — self-contained, no runtime dependencies
   on any other port. Makes apply-path richer.
2. **Service layer thin wrappers** (Group 3) — dissolves a whole
   directory of pass-through code; pulls validation into v1alpha1 where
   it belongs. Prerequisites Groups 1-5 to finish but Group 3 can
   start in parallel with Group 6.
3. **HTTP handlers** (Group 1) + **Go client** (Group 2) — end-user
   surface lands on v1alpha1. Big visible milestone; enables UI team.
4. **Deployment service + platform adapters** (Groups 4 & 5) — restores
   deployment functionality. Higher risk, larger behavioral surface.
5. **Seed + importer** (Group 7) — populates content; non-blocking.
6. **Embeddings** (Group 8) — semantic search; non-blocking.
7. **MCP bridge** (Group 9) — independent integration.
8. **pkg/models cleanup** (Group 11) — opportunistic, delete files as
   their consumers go.
9. **Legacy DB layer cleanup** (Group 12) — opportunistic, same.
10. **Legacy migrations → v1alpha1 rename** (Group 13) — final step.
11. **Enterprise coordination** (Group 14) — parallel throughout;
    sync after each OSS port PR.

Group 10 (CLI) is owned elsewhere and merges when it's ready; not
blocking our port work.
