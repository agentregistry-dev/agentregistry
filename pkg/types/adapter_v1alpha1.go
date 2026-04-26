package types

import (
	"context"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// DeploymentAdapter is the v1alpha1 runtime surface for deploying
// Agent or MCPServer targets onto a concrete platform (local
// docker-compose, Kubernetes, hosted cloud runtimes, etc.).
//
// One adapter per platform. Adapters are registered at app boot in a
// map keyed by Platform() string; the reconciler looks up by
// Provider.Spec.Platform when a Deployment apply arrives.
//
// Lifecycle contract (see design-docs/V1ALPHA1_PLATFORM_ADAPTERS.md):
//
//  1. apply handler validates + resolves refs + Upserts the Deployment
//     row; reconciler observes NOTIFY.
//  2. reconciler calls DeploymentAdapter.Apply with the resolved
//     Target + Provider objects.
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
	// Platform returns the discriminator string matching
	// Provider.Spec.Platform ("local", "kubernetes", "gcp", ...).
	Platform() string

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
	// Provider. Used by the enterprise Syncer (or an OSS equivalent)
	// to reconcile drift between the registry's Deployment rows and
	// external reality. Entries that correspond to managed
	// Deployments are correlated by labels/annotations; entries
	// without a managed owner surface as discovered-only.
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

	// Provider is the resolved ProviderRef.
	Provider *v1alpha1.Provider

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
	//   - "ProviderConfigured" — Provider.Config parsed and connectable
	//   - "Degraded"    — transient failure, will retry
	Conditions []v1alpha1.Condition

	// ProviderMetadata carries adapter-internal state to persist
	// into Deployment.Metadata.Annotations (keyed under
	// platforms.agentregistry.solo.io/<platform>/*). Callers marshal
	// to string values since Annotations is map[string]string.
	ProviderMetadata map[string]string
}

// RemoveInput carries the Deployment being torn down plus its resolved
// Provider (the Target has already been dereferenced and is not
// included; teardown operates on the recorded runtime state).
type RemoveInput struct {
	Deployment *v1alpha1.Deployment
	Provider   *v1alpha1.Provider
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
	Provider *v1alpha1.Provider
}

// DiscoveryResult describes one out-of-band workload the adapter
// observed under the Provider. The Syncer uses the Correlation field
// to decide whether this entry maps to an existing managed Deployment.
type DiscoveryResult struct {
	// TargetKind is the v1alpha1 Kind this workload looks like —
	// Agent or MCPServer. Empty if the adapter can't infer.
	TargetKind string
	// Namespace, Name, Version identify the workload in the
	// registry's naming scheme. Blank fields mean "unmanaged" —
	// workload exists on the platform but has no corresponding
	// Deployment row.
	Namespace string
	Name      string
	Version   string
	// ProviderMetadata mirrors what Apply writes so the caller can
	// correlate this discovery with an existing Deployment's
	// annotations.
	ProviderMetadata map[string]string
}

// -----------------------------------------------------------------------------
// Provider adapter.
// -----------------------------------------------------------------------------

// ProviderPlatformAdapter is the per-platform side-effect hook fired
// after a Provider PUT/DELETE on the v1alpha1 generic resource
// handler. The v1alpha1 store is the source of truth for the
// Provider row itself; the adapter exists purely to reconcile any
// per-platform sidecar state (e.g. enterprise's aws_connections /
// gcp_connections / kagent_connections rows) so downstream lookups
// — gateway credential resolution, platform-specific deploy paths —
// can read those tables consistently.
//
// One adapter per platform discriminator (provider.Spec.Platform).
// Enterprise builds register adapters via AppOptions.ProviderPlatforms;
// the registry app maps that into per-kind PostUpsert/PostDelete on
// KindProvider, dispatching by Spec.Platform.
//
// Hook errors propagate back to the API caller (500 on the per-kind
// PUT path; ApplyStatusFailed on the batch path) — the v1alpha1 row
// is already persisted, so a hook failure indicates degraded sidecar
// state.
type ProviderPlatformAdapter interface {
	// Platform returns the discriminator string that matches
	// provider.Spec.Platform ("aws", "gcp", "kagent", ...).
	Platform() string

	// ApplyProvider runs after the v1alpha1 store has persisted a
	// Provider on PUT or batch apply. Must be idempotent — re-apply
	// with rotated config must converge sidecar state, not error.
	ApplyProvider(ctx context.Context, provider *v1alpha1.Provider) error

	// RemoveProvider runs after the v1alpha1 store has soft-deleted a
	// Provider. providerID is the metadata.name (the v1alpha1 row's
	// stable identity). Must tolerate missing sidecar rows for
	// idempotency.
	RemoveProvider(ctx context.Context, providerID string) error
}
