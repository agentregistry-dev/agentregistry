# v1alpha1 Platform Adapters — Design Draft

> Status: **draft — not implemented yet**. Input from @shashankram,
> @yuval-k, @EItanya welcome in-line.
>
> Scope: defines the adapter contract over `*v1alpha1.Deployment` that
> replaces the legacy `types.DeploymentPlatformAdapter` (tied to
> `*models.Deployment`). Ports local + kubernetes adapters to the new
> shape.
>
> **Nothing here is code yet.** Goal: agree on interface + lifecycle
> shape so the ~3.3k LOC port can proceed without mid-flight redesigns.

---

## Goals

1. One adapter interface over `*v1alpha1.Deployment` types that covers
   local docker-compose, Kubernetes, and (future) cloud-managed
   runtimes.
2. Clean lifecycle contract around K8s-style soft-delete + finalizers.
3. Clear path for enterprise to register custom platforms without
   forking OSS.
4. Preserve existing business logic: docker-compose rendering,
   agentgateway config synthesis, kubernetes CRD templating, log
   streaming, cancellation, discovery.

## Non-goals (this PR)

- Rewriting any adapter's orchestration internals. The docker-compose
  / k8s rendering stays; only input/output types change.
- Solving the reconciler problem. This design assumes reconciler loop
  arrives later (KRT rebase, Group 2 in REBUILD_TRACKER) and just
  specifies what the adapter receives from whatever calls it.

---

## Proposed interface

```go
// pkg/types/adapter_v1alpha1.go
package types

import (
    "context"
    "github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// DeploymentPlatformAdapter is the runtime surface for a v1alpha1
// Deployment target. Implementations live in
// internal/registry/platforms/<platform>/.
type DeploymentPlatformAdapter interface {
    // Platform matches Provider.Spec.Platform ("local", "kubernetes", ...).
    // Adapters register at app boot keyed by this string.
    Platform() string

    // SupportedTargetKinds lists the v1alpha1 Kinds this adapter can
    // deploy. Typically [Agent, MCPServer]; an agent-only adapter
    // could return [Agent] and skip MCPServer deployments.
    SupportedTargetKinds() []string

    // Apply ensures the Deployment's observed state matches the
    // desired state (DesiredState="deployed" ⇒ run it;
    // DesiredState="undeployed" ⇒ tear down).
    // Idempotent: safe to call with the same input repeatedly.
    Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error)

    // Remove is called when the Deployment row has a non-nil
    // DeletionTimestamp. Adapter tears down any remaining runtime
    // resources, then returns; caller removes the adapter's finalizer
    // so PurgeFinalized can hard-delete the row. Idempotent.
    Remove(ctx context.Context, in RemoveInput) error

    // Logs streams runtime logs from the deployed workload. Returned
    // channel closes when streaming ends; caller cancels via ctx.
    Logs(ctx context.Context, in LogsInput) (<-chan LogLine, error)

    // Discover enumerates out-of-band workloads under a Provider.
    // Used by the enterprise Syncer to reconcile drift. Entries that
    // correspond to managed Deployments are correlated by labels;
    // entries without a managed owner surface as "discovered-only".
    Discover(ctx context.Context, in DiscoverInput) ([]DiscoveryResult, error)
}

// ApplyInput carries everything Apply needs without the adapter
// reaching into the Store directly — the reconciler/apply pipeline
// pre-resolves refs and hands in concrete objects.
type ApplyInput struct {
    Deployment *v1alpha1.Deployment
    Target     v1alpha1.Object      // resolved TargetRef (*v1alpha1.Agent or *v1alpha1.MCPServer)
    Provider   *v1alpha1.Provider
    // Resolver is still passed so agent→MCPServer ref resolution can
    // happen *inside* the adapter for agentgateway config synthesis.
    // The local adapter needs to resolve every AgentSpec.MCPServers
    // entry to build the gateway's upstream map; we don't want the
    // caller to pre-resolve them (that'd couple the caller to
    // per-adapter quirks like "some adapters don't need this").
    Resolver v1alpha1.ResolverFunc
}

type ApplyResult struct {
    // Conditions to merge into Deployment.Status via Store.PatchStatus.
    // Canonical types: "Ready", "ProviderConfigured", "Degraded".
    Conditions []v1alpha1.Condition

    // ProviderMetadata captures adapter-internal state the adapter
    // wants persisted (container IDs, k8s resource UIDs, etc.) so
    // subsequent Apply/Remove calls can find existing artifacts.
    // Written under status.providerMetadata (proposed — see open
    // question #3 below).
    ProviderMetadata map[string]any

    // Finalizers the adapter wants added to the Deployment. Standard
    // pattern: Apply adds "<platform>.agentregistry.solo.io/cleanup";
    // Remove drops it after tear-down completes.
    AddFinalizers    []string
    RemoveFinalizers []string
}

type RemoveInput struct {
    Deployment *v1alpha1.Deployment
    Provider   *v1alpha1.Provider
}

type LogsInput struct {
    Deployment *v1alpha1.Deployment
    Follow     bool
    TailLines  int
}

type LogLine struct {
    Timestamp time.Time
    Stream    string // "stdout" | "stderr"
    Line      string
}

type DiscoverInput struct {
    Provider *v1alpha1.Provider
}

type DiscoveryResult struct {
    // Namespace, Name, Version that this discovered workload maps to
    // in the registry. Empty Namespace ⇒ unmanaged (not tied to a
    // Deployment row).
    TargetKind string
    Namespace  string
    Name       string
    Version    string
    // ProviderMetadata mirrors what Apply would produce; Syncer uses
    // it to correlate with stored Deployment.Status.providerMetadata.
    ProviderMetadata map[string]any
}
```

---

## Lifecycle sequence

```
1. client PUTs /v0/namespaces/{ns}/deployments/{name}/{v}
   → handler Validates + ResolvesRefs + Store.Upsert
   → row lands with DesiredState="deployed", Generation=N

2. reconciler (TBD — Group 2 KRT rebase) observes the write via NOTIFY,
   resolves TargetRef + ProviderRef, picks the adapter by
   Provider.Spec.Platform, calls adapter.Apply.

3. adapter.Apply:
   - renders platform-specific artifacts (docker-compose, k8s Deployment)
   - issues platform commands (docker compose up, kubectl apply)
   - returns ApplyResult with Ready=True and AddFinalizers=[cleanup-local]

4. reconciler writes Status via Store.PatchStatus, adds finalizer
   via Store.PatchFinalizers.

5. user deletes: DELETE /v0/namespaces/{ns}/deployments/{name}/{v}
   → Store.Delete sets DeletionTimestamp
   → Store.Delete does NOT hard-delete because Finalizers is non-empty

6. reconciler observes DeletionTimestamp, calls adapter.Remove.

7. adapter.Remove tears down, reconciler removes the adapter's finalizer
   via Store.PatchFinalizers.

8. PurgeFinalized GC sweeps the row (Finalizers=[], DeletionTimestamp
   non-nil).
```

---

## Resolutions (2026-04-17)

User + drafter reviewed every open question; all 8 resolved below.
The section that follows preserves the trade-off discussion; the
decision on each is inlined.

1. **Sync vs async Apply → always async**. Every adapter returns
   immediately with a `Progressing` condition; adapters run their
   own internal watch loop to later write `Ready=True` via
   `Store.PatchStatus`. Local adapter spins a goroutine + state
   table alongside docker-compose so it matches Kubernetes semantics.
   Trades off simplicity for uniform reconciler behavior.

2. **ProviderMetadata home → Annotations**. `ObjectMeta.Annotations`
   lands as its own small PR (see Importer doc, resolved) before
   the adapter port so adapters can write their state there from
   day one. No temporary inline `Status.ProviderMetadata` needed.

3. **Cross-namespace refs → allowed on both target + provider**.
   ResourceRef already has optional Namespace. Authz layer gates
   reads across namespaces. Common case: platform-owned Providers
   in `platform` namespace, team-owned Agents in `team-*`,
   Deployments that combine the two.

4. **Finalizer ownership → adapter declares, reconciler plumbs**.
   Apply returns `AddFinalizers`; Remove returns `RemoveFinalizers`;
   reconciler calls `Store.PatchFinalizers` with the lists.
   Enterprise adapters use their own prefix; naming collision is
   their responsibility.

5. **Provider credentials → keep map[string]any, document + authz
   gate, defer SecretRef to a separate design**. Legacy already
   stores kubeconfig inline as a string; matching that behavior
   preserves existing deployments. Tracker item: a dedicated
   SecretRef design pass covering K8s Secrets, Vault, AWS SM, and
   registry-managed secret store backends. **Not a prerequisite for
   this port.**

6. **Typed Provider config → map[string]any, adapters unmarshal**.
   No server-side schema check on Provider.Spec.Config; adapters
   unmarshal at Apply time and surface a `ProviderConfigured=False`
   condition on parse failure. Preflight validation hook rejected
   as over-engineering for demo-mode.

7. **Adapter dispatch → registry `map[platform]Adapter`**.
   AppOptions carries the map; reconciler looks up by
   `Provider.Spec.Platform`. Matches the existing pattern.

8. **Discovery output → return-and-caller-writes**. Adapter.Discover
   returns `[]DiscoveryResult`; Syncer (or an OSS equivalent) owns
   the writes to `discovered_local` / `discovered_kubernetes`.
   Adapter stays side-effect-free outside its own Deployment's
   Status.

---

## Trade-offs considered (for reference)

### 1. Synchronous Apply vs reconciler loop

**Option A**: reconciler always calls Apply; Apply returns immediately
with a "Progressing" condition; a watch loop later reports
convergence. Matches K8s control-plane semantics.

**Option B**: Apply blocks until the workload is healthy (timeout
bounded). Simpler for local adapter (docker compose up runs to
completion). Awkward for k8s (rollouts take minutes).

Recommendation: **Option A for k8s, Option B for local** — let the
adapter choose. Add a `synchronous bool` field on the interface or
signal via documentation.

### 2. Where does ProviderMetadata live?

Options:
- **a)** Inline in `v1alpha1.Status.ProviderMetadata map[string]any`.
  User-visible on GET; bloats the status payload; no schema.
- **b)** Side table `deployment_provider_metadata (namespace, name,
  version, metadata JSONB)`. Hidden from user; adds one table + one
  query per apply.
- **c)** Annotations (if we add `ObjectMeta.Annotations`) —
  K8s-idiomatic for controller state.

Recommendation: **(c) once Annotations land** (see Importer draft for
annotations proposal); **(a) as temporary home until then**.

### 3. Cross-namespace references

Can a Deployment in namespace `team-a` reference:
- a Provider in `platform` (shared)?
- an Agent in `team-b` (another team)?

Recommendation: **Yes for both**, with authz enforcement. ResourceRef
already carries optional `Namespace`. Policy: read-only cross-
namespace ref OK; authz layer can deny.

### 4. Finalizer ownership

Who owns the adapter's finalizer?

Recommendation: **the adapter itself**. Apply declares
AddFinalizers=["<platform>.agentregistry.solo.io/cleanup"], Remove
declares RemoveFinalizers=[same]. The reconciler is a dumb courier:
calls Apply/Remove, forwards the declared adds/removes via
Store.PatchFinalizers. No central finalizer registry.

Enterprise-added platforms add their own finalizer tokens under their
own prefix; naming collision is their problem.

### 5. Provider authz

`Provider.Spec.Config map[string]any` can carry credentials today
(kubeconfig, cloud API keys). Should we:
- **a)** Bake a `Provider.Spec.Credentials *SecretRef` field pointing
  at a Kubernetes Secret or registry-managed secret store?
- **b)** Continue with `Config` being secret-containing; document
  strongly; rely on authz to gate who can read Provider rows?

Recommendation: **(b) for now, revisit with enterprise**. Secret
management is a separate system we don't want to invent here. Flag
clearly in the Provider spec doc.

### 6. Typed Provider config

Current: `ProviderSpec.Config map[string]any`. Reviewer
@shashankram flagged this as a future concern.

Proposed shape:
```go
type ProviderSpec struct {
    Platform string
    // Config is deliberately untyped at the generic layer. Adapters
    // receive *Provider and unmarshal Config into their platform-
    // specific struct — see LocalProviderConfig /
    // KubernetesProviderConfig below.
    Config map[string]any
}
```
Adapters own the typed structs:
```go
// internal/registry/platforms/local/types.go
type LocalProviderConfig struct {
    RuntimeDir       string `json:"runtimeDir"`
    AgentGatewayPort int    `json:"agentGatewayPort"`
}
```
Unmarshal at Apply time; validate + surface ProviderConfigured
condition with the error on failure.

Recommendation: **keep generic `map[string]any`**; adapters do typed
unmarshal. Trade: no schema validation at server apply; server will
happily store garbage Config. Mitigate by having each adapter register
a "preflight" validation hook that the apply handler calls after
`obj.Validate` when the target object is a Provider and the Platform
matches (optional plumbing — defer if not worth the complexity).

### 7. Reconciler → adapter coupling

Where does the dispatch `Provider.Spec.Platform → Adapter` happen?

Options:
- **a)** A registry struct that maps platform name to adapter;
  reconciler looks up by platform.
- **b)** Each adapter is a plain function bound at wiring time; no
  central registry.

Recommendation: **(a)**. Straightforward, matches AppOptions current
`DeploymentPlatforms map[string]Adapter` pattern (currently keyed by
model.Deployment; rename to the new shape).

### 8. Discovery interplay with the Syncer

The enterprise Syncer already writes to `discovered_local` /
`discovered_kubernetes` tables. Adapter.Discover returns entries;
does it write to those tables directly or return them for the caller
to write?

Recommendation: **return-and-caller-writes**. Adapter stays pure
(no direct Store writes outside its own Status.PatchStatus). The
Syncer (or a v1alpha1-aware variant) takes DiscoveryResult slices and
reconciles them into the discovered tables.

---

## Port sequence

1. **Define the new interface + helper types** in `pkg/types/adapter_v1alpha1.go`.
   Keep the legacy `types.DeploymentPlatformAdapter` interface alive
   alongside it (renamed to `types.LegacyDeploymentPlatformAdapter`)
   so legacy adapters continue to compile.

2. **Port the `local` adapter** (`internal/registry/platforms/local/`):
   - Add `deployment_adapter_local_v1alpha1.go` implementing the new
     interface. Reuse the docker-compose rendering helpers and
     agentgateway config synthesis functions verbatim; the only
     changes are at the input type boundary.
   - Integration test: `TestLocalAdapter_ApplyRemove` that spins a
     docker-compose workload in a temp runtime dir, verifies
     it's running, then Remove and verifies it's gone.

3. **Port the `kubernetes` adapter** similarly.
   - `deployment_adapter_kubernetes_v1alpha1.go`.
   - Integration test against a kind/fake cluster (envtest) —
     or skip if the legacy tests covered enough edge cases for us
     to trust the port.

4. **Wire in registry_app**. AppOptions gains:
   ```go
   V1Alpha1DeploymentPlatforms map[string]DeploymentPlatformAdapter
   ```
   Legacy `DeploymentPlatforms` stays until the reconciler port
   removes the last caller.

5. **Post-port cleanup**. Delete legacy adapters once nothing calls
   them (depends on reconciler port — Group 2).

---

## Preserved business logic (behaviors to port faithfully)

- [ ] Local: docker-compose YAML rendering (port alloc, env merge,
      service naming, DNS-1123 conformance)
- [ ] Local: agentgateway config synthesis for Agent targets —
      resolves `AgentSpec.MCPServers` refs, builds upstream map
- [ ] Local: restart policy, `docker compose logs --follow`,
      `docker compose down`
- [ ] Local: provider config — RuntimeDir, AgentGatewayPort
- [ ] Kubernetes: Deployment + Service + ConfigMap rendering via
      controller-runtime client
- [ ] Kubernetes: label ownership with deployment name + generation
- [ ] Kubernetes: rollout wait, pod status surfacing
- [ ] Kubernetes: pod exec for logs, namespaced cancellation
- [ ] Provider adapter interface: List/Create/Get/Update/Delete
      Providers (separate from Deployment adapter — keeps enterprise
      cloud-provider plugin surface)
- [ ] Discovery: Syncer-friendly output shape

---

## Estimated effort

- Interface + helper types: **half day**.
- Local adapter port (keeping orchestration logic): **1-2 days**.
- Kubernetes adapter port: **1-2 days**.
- Wiring + AppOptions + tests: **half day**.

Total: **3-5 days** of focused work, assuming no surprises in the
agentgateway-types package (which is 451 LOC of kagent/kmcp/k8s CRD
structures that might need their own vocabulary cleanup).

---

## What this unblocks

- Group 2 KRT reconciler port (Group 5 adapters are the prerequisite
  target of the reconciler's control loop).
- Group 9 MCP protocol bridge's `deploy_server` / `cancel_deployment`
  tools — they need the adapter path alive to actually deploy.
- `arctl agent run` / `arctl mcp run` (if the declarative-CLI
  replacement preserves them).
