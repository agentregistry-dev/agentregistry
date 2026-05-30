# Agent Knowledge Pack

This is the repo-local starting point for agents picking up AgentRegistry work
from prior sessions. It covers the OSS repo at
`/Users/tacopaco/workspace/solo/code/agentregistry` and the enterprise repo at
`/Users/tacopaco/workspace/solo/code/agentregistry-enterprise`.

Use this document before writing new architecture summaries, PR review guides,
or handoffs. It is intentionally an index and decision map, not a replacement
for reading the live code, current branch, and current PR state.

## Session Manager Packs

Use the Session Manager skill whenever a task depends on prior conversation
context, especially for work owned by Scott/ilackarms.

If the Session Manager API is running, search the top-level packs first:

```bash
curl -sS 'http://127.0.0.1:4000/api/rag/search?q=<query>&project=agentregistry&k=10'
curl -sS 'http://127.0.0.1:4000/api/rag/search?q=<query>&project=agentregistry-enterprise&k=10'
```

If the API is not running, use the local CLI:

```bash
SESSION_MANAGER_REPO="${SESSION_MANAGER_REPO:-$HOME/workspace/fun/code/claude-codex-manager}"
npm --prefix "$SESSION_MANAGER_REPO" run -s sm -- recent --project agentregistry --since 30d --limit 25
npm --prefix "$SESSION_MANAGER_REPO" run -s sm -- resume --project-path /Users/tacopaco/workspace/solo/code/agentregistry --format prompt
npm --prefix "$SESSION_MANAGER_REPO" run -s sm -- resume --project-path /Users/tacopaco/workspace/solo/code/agentregistry-enterprise --format prompt
```

Top-level packs created for broad onboarding:

- `agentregistry-comprehensive-docs-context`: comprehensive OSS and linked worktree context; created 2026-05-30 with 207 sessions and indexed with 1536-dimensional `openai/text-embedding-3-small` embeddings.
- `agentregistry-enterprise-comprehensive-docs-context`: comprehensive enterprise context; created 2026-05-30 with 10 session references and indexed from 7 available conversations.

Focused packs to prefer when the task has a clear topic:

- `agentregistry-krt-followup-management`: KRT follow-up management, OSS PR #517, enterprise PR #727, and hardening follow-ups.
- `agentregistry-krt-controller-review-context`: H-086/H-090 review context, local review guide, source projection, kind registration, and enterprise companion context.
- `agentregistry-krt-implementation-start`: KRT implementation kickoff, controller execution, and deployment intent/event model.
- `agentregistry-krt-controller-architecture`: KRT architecture RFC context, old Phase 2 branch archaeology, v1alpha1 substrate, approval bridge, `reconcile_events`, and discovered-state decisions.
- `agentregistry-approval-reconciler-next`: approval staging, `ApproveBatch`, RBAC/AccessPolicy hooks, generation/observedGeneration, and reconciler split context.
- `agentregistry-version-tags-status-generation`: version/tag architecture, latest-tag semantics, generation/status decisions, and OSS/enterprise PR stack context.
- `agentregistry-tagged-versioning-takeover`: tagged-versioning takeover and approval-flow review context.
- `k8s-api-redesign`: v1alpha1 API envelope, store/handler migration, authz bypass fixes, and enterprise port review.
- `v1alpha1-incremental-port`: original incremental v1alpha1 port session.
- `krt-redesign`: older KRT/controller redesign sessions.
- `agentregistry-h054-enterprise-pr574-wrap`: enterprise PR #574 tag-identity wrap-up.
- `agentregistry-enterprise-baseline`: older enterprise baseline sessions, mainly deployment logs/OpenAPI pinning.
- `agentregistry-h040-version-tag-cleanup`: broad older catch-all pack. Useful for archaeology, but search focused packs first when possible.

## Current Workstream Map

### KRT Controller Lane

Important handoffs and sessions: H-086, H-090, H-091, H-099, and H-101.
Important branches/worktrees have included:

- OSS: `ilackarms/krt-controller`, PR #517, worktree
  `/Users/tacopaco/workspace/solo/code/agentregistry-krt-controller-foundations`.
- Enterprise: `ilackarms/krt-controller-kind-registry`, PR #727, worktree
  `/Users/tacopaco/workspace/solo/code/agentregistry-enterprise-krt-kind-registry`.

Carry-forward decisions:

- The old KRT branch is a reference implementation, not a branch to replay wholesale.
- The accepted controller shape is database-backed source state via `krt.StaticCollection`, derived deployment work via `krt.NewCollection`, dependency edges via `FetchOne(..., krt.FilterKey(...))`, and effectful work isolated to deriver/executor boundaries.
- `RecomputeTrigger` is an Istio KRT escape hatch, but was not needed in the current controller lane because retry/backoff state lives in durable executor/controller tables.
- A final skeptical KRT review found no remaining KRT-native blocker; do not force churn just to make code look more KRT-shaped.
- `make verify` is PR-readiness for this lane. API/delete-surface changes can regenerate `openapi.yaml`, `ui/lib/api/types.gen.ts`, enterprise `openapi/openapi-latest.yaml`, and `ui/src/Api/ARE/*`.
- Enterprise may need an extra pseudo-version repin after any OSS fixup commit.
- Local-only guide files such as `KRT_CONTROLLER_REVIEW_GUIDE.md` have interfered with rebase/verify before. Move or stash them temporarily if needed, restore afterward, and do not commit them unless explicitly requested.

### Approval And Auditability Lane

Important surfaces:

- OSS admission seam and apply replay: `pkg/registry/resource/{core.go,apply.go}`.
- Enterprise approval service and routes: `go/internal/registry/approval/{service.go,routes.go,store.go}`.
- Design doc: `design-docs/ENTERPRISE_NATIVE_CREATE_APPROVAL_FLOW_RFC.md`.
- Review guide artifact: `APPROVAL_PR_REVIEW_GUIDE.md`.

Carry-forward decisions:

- The OSS admission seam allows enterprise approval staging between validation/authz and production storage.
- Enterprise create approval should stage only content-artifact kinds: `Agent`, `MCPServer`, `RemoteMCPServer`, `Skill`, and `Prompt`.
- A production `Deployment` that references a pending artifact remains production intent; it should converge once the artifact is approved instead of being staged itself.
- `ApplyInterceptor`, `ResolverWrapper`, and `ExtraResourceRoutes` are temporary synchronous-handler bridges until KRT/reconciler-owned admission and staging exist.
- Durable auditability concerns are not a ClickHouse/KRT event-storage problem. Current KRT eventing is Postgres-backed through `control_plane_events` and `reconcile_events`; approval decision retention needs explicit decision fields such as `approved_by` and `approved_at`.
- If a reviewer asks whether feedback targets approval or KRT, map the wording to the code path before accepting the premise.

### RemoteMCPServer And v1alpha1 Lane

Important docs:

- `design-docs/V1ALPHA1_REMOTE_MCP_SERVER.md`
- `design-docs/V1ALPHA1_PLATFORM_ADAPTERS.md`
- `docs/auth/authz-matrix.md`

Carry-forward decisions:

- The supported surface is `RemoteMCPServer`, not `RemoteAgent`, unless the user explicitly reopens that feature.
- `RemoteMCPServer` is a top-level v1alpha1 kind and should be represented in OSS kind registration, CRUD bindings, importer/seed behavior, embeddings, generated OpenAPI/UI clients, and enterprise authz mappings where relevant.
- Enterprise merge-conflict recovery in API-shape work should start by checking whether the local branch is behind the real PR head before resolving conflicts.
- `make verify` is the safest path after API-shape conflicts because it regenerates OpenAPI and generated UI clients.
- Enterprise fixtures need the current `AgentSource` layout, including `spec.source.image` and `spec.source.repository`, while preserving fixture-specific metadata such as `repository.source: git`.

### Enterprise Runtime And Cloud Lane

Important enterprise rules:

- Do not add new env vars with the `AGENT_REGISTRY_` prefix. New env vars use short names like `CLICKHOUSE_ADDR`.
- Do not duplicate upstream logic that belongs in OSS.
- When an OSS change lands and enterprise depends on it, update the enterprise pseudo-version and regenerate generated artifacts.
- AgentCore list APIs are not correctness sources. Successful deploys should record runtime identity directly; cloud list APIs and `aws_agents` are cache/discovery surfaces until a reconciler owns both.
- ClickHouse is observability infrastructure, not the durable store for KRT controller audit history.

## Operating Rules For New Sessions

Before starting non-trivial AgentRegistry work:

1. Check the live repo and branch state in both repos when the task is cross-repo.
2. Search the relevant Session Manager pack before answering history, design, or "why did we" questions.
3. Prefer focused packs over the comprehensive pack once the topic is known.
4. Treat the comprehensive packs as onboarding, then verify against live code and PR heads.
5. Preserve unrelated local changes and local-only review artifacts.
6. Use isolated worktrees for risky PR review, rebase, or branch archaeology.
7. Run the verification that proves the claim. For API/generated-artifact work, that usually means `make verify` or the repo-specific generated checks, not just a focused unit test.

## Quick Commands

Inspect current packs:

```bash
curl -sS 'http://127.0.0.1:4000/api/rag/packs?project=agentregistry'
curl -sS 'http://127.0.0.1:4000/api/rag/packs?project=agentregistry-enterprise'
```

Search KRT history:

```bash
curl -sS 'http://127.0.0.1:4000/api/rag/search?q=KRT%20controller%20PR%20517%20PR%20727&project=agentregistry&k=10'
```

Search approval history:

```bash
curl -sS 'http://127.0.0.1:4000/api/rag/search?q=approval%20staging%20ApplyInterceptor%20ApproveBatch&project=agentregistry&k=10'
```

Local fallback:

```bash
npm --prefix "$HOME/workspace/fun/code/claude-codex-manager" run -s sm -- recall --query "agentregistry KRT approval RemoteMCPServer" --project agentregistry
npm --prefix "$HOME/workspace/fun/code/claude-codex-manager" run -s sm -- recent --project agentregistry-enterprise --since 90d --limit 40
```
