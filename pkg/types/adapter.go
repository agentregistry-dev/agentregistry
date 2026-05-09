package types

import (
	"context"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// DeploymentAdapter is the v1alpha1 runtime surface for deploying
// Agent or MCPServer targets onto a concrete runtime (local
// docker daemon, Kubernetes, hosted cloud runtimes, etc.).
//
// One adapter per runtime type. Adapters are registered at app boot in
// a map keyed by Type() string; the reconciler looks up by
// Runtime.Spec.Type when a Deployment apply arrives.
//
// Lifecycle contract (see design-docs/V1ALPHA1_RUNTIME_ADAPTERS.md):
//
//  1. apply handler validates + resolves refs + Upserts the Deployment
//     row; reconciler observes NOTIFY.
//  2. reconciler calls DeploymentAdapter.Apply with the resolved
//     Target + Runtime objects.
//  3. Apply returns immediately with a Progressing condition. Adapter
//     spawns its own watch loop to later PatchStatus with Ready=True
//     when the workload converges.
//  4. on Deployment delete, Store.Delete sets DeletionTimestamp; the
//     reconciler calls DeploymentAdapter.Remove for external-state
//     teardown. Row lifetime is owned by soft-delete + GC, not by
//     adapter-returned tokens — Remove is purely an external-state
//     hook.
//
// Apply is ALWAYS ASYNC. Apply returns quickly; convergence is tracked
// via the adapter's own watch loop writing status. The reconciler
// doesn't block on convergence.
type DeploymentAdapter interface {
	// Type returns the canonical CamelCase discriminator string
	// ("Local", "Kubernetes", "BedrockAgentCore", ...). Runtime.Validate
	// canonicalizes Spec.Type at admission, so the reconciler's adapter
	// lookup compares Type() against Spec.Type with exact-match equality.
	Type() string

	// SupportedTargetKinds lists the v1alpha1 Kinds this adapter can
	// deploy. Typically []string{KindAgent, KindMCPServer}. Used by
	// the reconciler to early-reject a Deployment whose TargetRef
	// points at a kind the adapter doesn't handle.
	SupportedTargetKinds() []string

	// Apply ensures the Deployment's runtime matches its desired
	// state. DesiredState == "deployed" or "" (default) ⇒ run.
	// DesiredState == "undeployed" ⇒ reconciler routes to Remove
	// directly; adapters can assume Apply is only called with a
	// run-intent.
	//
	// Idempotent. Safe to call repeatedly with the same input.
	// Returns the initial conditions to persist (typically
	// Progressing=True). The adapter's async watch loop later refines
	// the conditions via PatchStatus.
	Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error)

	// Remove tears down runtime resources. Called when:
	//   - Deployment.Metadata.DeletionTimestamp != nil (soft-delete)
	//   - Deployment.Spec.DesiredState == "undeployed"
	// Idempotent: safe to call when nothing exists. Row lifetime is
	// owned by the soft-delete + GC path; the adapter only handles
	// external-state teardown.
	Remove(ctx context.Context, in RemoveInput) (*RemoveResult, error)

	// Logs streams runtime logs from the deployed workload. The
	// returned channel closes when streaming ends; caller cancels via
	// ctx.
	Logs(ctx context.Context, in LogsInput) (<-chan LogLine, error)

	// Discover enumerates out-of-band workloads running under a
	// Runtime. Used by downstream syncers to reconcile drift between
	// the registry's Deployment rows and external reality. Entries
	// that correspond to managed Deployments are correlated by
	// labels/annotations; entries without a managed owner surface as
	// discovered-only.
	//
	// Adapters MUST NOT write directly to the discovered_* tables;
	// the caller persists the results.
	Discover(ctx context.Context, in DiscoverInput) ([]DiscoveryResult, error)
}

// ApplyInput carries everything Apply needs without the adapter
// reaching into the Store directly — the reconciler pre-resolves refs
// and hands in concrete objects.
type ApplyInput struct {
	// Deployment is the resource being applied.
	Deployment *v1alpha1.Deployment

	// Target is the resolved TargetRef — either *v1alpha1.Agent or
	// *v1alpha1.MCPServer. Adapters type-switch on it.
	Target v1alpha1.Object

	// Runtime is the resolved RuntimeRef.
	Runtime *v1alpha1.Runtime

	// Resolver is passed so adapters can check nested ref existence
	// mid-Apply (blank-namespace refs inherit from the referencing
	// object — same rules as v1alpha1.Object ResolveRefs).
	Resolver v1alpha1.ResolverFunc

	// Getter fetches the typed Object for a ResourceRef. Adapters use
	// this when they need the target's Spec (not just an existence
	// check) — for example, the local adapter walking
	// AgentSpec.MCPServers to build agentgateway upstream config.
	Getter v1alpha1.GetterFunc
}

// ApplyResult captures the status + annotation deltas the reconciler
// should persist after Apply.
type ApplyResult struct {
	// Conditions to merge into Deployment.Status via
	// Store.PatchStatus. Canonical types:
	//   - "Progressing" — workload is being created/updated
	//   - "Ready"       — workload is running + serving
	//   - "RuntimeConfigured" — Runtime.Config parsed and connectable
	//   - "Degraded"    — transient failure, will retry
	Conditions []v1alpha1.Condition

	// RuntimeMetadata carries adapter-internal state to persist
	// into Deployment.Metadata.Annotations (keyed under
	// runtimes.agentregistry.solo.io/<type>/*). Callers marshal
	// to string values since Annotations is map[string]string.
	RuntimeMetadata map[string]string
}

// RemoveInput carries the Deployment being torn down plus its resolved
// Runtime (the Target has already been dereferenced and is not
// included; teardown operates on the recorded runtime state).
type RemoveInput struct {
	Deployment *v1alpha1.Deployment
	Runtime    *v1alpha1.Runtime
}

// RemoveResult describes the outcome of a Remove call. The reconciler
// merges Conditions into Deployment.Status; idempotent re-Remove on a
// completed teardown is the expected pattern (no separate finalizer
// drain — soft-delete + GC handle the lifetime).
type RemoveResult struct {
	// Conditions to merge into Deployment.Status (typically
	// Progressing with Reason="Terminating", then Ready=False with
	// Reason="Removed" on completion).
	Conditions []v1alpha1.Condition
}

// LogsInput selects a log stream for the deployed workload.
type LogsInput struct {
	Deployment *v1alpha1.Deployment
	// Follow ⇒ stream indefinitely until ctx is cancelled. !Follow ⇒
	// return the available backlog and close.
	Follow bool
	// TailLines bounds the initial backlog; 0 means unbounded.
	TailLines int
}

// LogLine is a single emitted log record from the workload.
type LogLine struct {
	Timestamp time.Time
	Stream    string // "stdout" | "stderr" | platform-specific
	Line      string
}

// DiscoverInput scopes a Discover call.
type DiscoverInput struct {
	Runtime *v1alpha1.Runtime
}

// DiscoveryResult describes one out-of-band workload the adapter
// observed under the Runtime. The Syncer uses the Correlation field
// to decide whether this entry maps to an existing managed Deployment.
type DiscoveryResult struct {
	// TargetKind is the v1alpha1 Kind this workload looks like —
	// Agent or MCPServer. Empty if the adapter can't infer.
	TargetKind string
	// Namespace, Name, Tag identify the workload in the
	// registry's naming scheme. Blank fields mean "unmanaged" —
	// workload exists on the runtime but has no corresponding
	// Deployment row.
	Namespace string
	Name      string
	Tag       string
	// RuntimeMetadata mirrors what Apply writes so the caller can
	// correlate this discovery with an existing Deployment's
	// annotations.
	RuntimeMetadata map[string]string
}

// -----------------------------------------------------------------------------
// Runtime adapter.
// -----------------------------------------------------------------------------

// RuntimeAdapter is the per-type side-effect hook fired after a
// Runtime PUT/DELETE on the v1alpha1 generic resource handler. The
// v1alpha1 store is the source of truth for the Runtime row itself;
// the adapter exists purely to reconcile any per-type sidecar state
// (downstream connection tables, credential caches, etc.) so other
// lookups — gateway credential resolution, type-specific deploy paths
// — can read those tables consistently.
//
// One adapter per runtime type discriminator (runtime.Spec.Type).
// Downstream builds register adapters via AppOptions.RuntimeAdapters;
// the registry app maps that into per-kind PostUpsert/PostDelete on
// KindRuntime, dispatching by exact-match against Spec.Type
// (Runtime.Validate canonicalizes user-supplied case at admission).
//
// Hook errors propagate back to the API caller (500 on the per-kind
// PUT path; ApplyStatusFailed on the batch path) — the v1alpha1 row
// is already persisted, so a hook failure indicates degraded sidecar
// state.
type RuntimeAdapter interface {
	// Type returns the canonical CamelCase discriminator string
	// ("Local", "Kubernetes", "BedrockAgentCore", "GeminiAgentRuntime",
	// ...). Runtime.Validate canonicalizes Spec.Type at admission so
	// dispatch can compare with exact-match equality.
	Type() string

	// ApplyRuntime runs after the v1alpha1 store has persisted a
	// Runtime on PUT or batch apply. Must be idempotent — re-apply
	// with rotated config must converge sidecar state, not error.
	ApplyRuntime(ctx context.Context, runtime *v1alpha1.Runtime) error

	// RemoveRuntime runs after the v1alpha1 store has soft-deleted a
	// Runtime. runtimeID is the metadata.name (the v1alpha1 row's
	// stable identity). Must tolerate missing sidecar rows for
	// idempotency.
	RemoveRuntime(ctx context.Context, runtimeID string) error
}
