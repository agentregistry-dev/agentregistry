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
//  3. Apply returns immediately with a Progressing condition and
//     AddFinalizers = ["<platform>.agentregistry.solo.io/cleanup"].
//     Adapter spawns its own watch loop to later PatchStatus with
//     Ready=True when the workload converges.
//  4. on Deployment delete, Store.Delete sets DeletionTimestamp; row
//     stays because of the adapter finalizer.
//  5. reconciler sees DeletionTimestamp, calls DeploymentAdapter.Remove;
//     adapter tears down + returns RemoveFinalizers with its token.
//  6. reconciler calls Store.PatchFinalizers; PurgeFinalized GC
//     hard-deletes the row.
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
	// Progressing=True) and any finalizers to add. The adapter's
	// async watch loop later refines the conditions via PatchStatus.
	Apply(ctx context.Context, in ApplyInput) (*ApplyResult, error)

	// Remove tears down runtime resources. Called when:
	//   - Deployment.Metadata.DeletionTimestamp != nil (soft-delete)
	//   - Deployment.Spec.DesiredState == "undeployed"
	// Idempotent: safe to call when nothing exists.
	// Returns the finalizer tokens to drop once cleanup is complete;
	// an adapter can return an empty RemoveFinalizers if its teardown
	// is still in-progress and the reconciler should retry.
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

// ApplyResult captures the status + finalizer deltas the reconciler
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

	// AddFinalizers lists finalizer tokens the adapter wants added
	// to the Deployment. Standard pattern:
	// "<platform>.agentregistry.solo.io/cleanup".
	AddFinalizers []string
}

// RemoveInput carries the Deployment being torn down plus its resolved
// Provider (the Target has already been dereferenced and is not
// included; teardown operates on the recorded runtime state).
type RemoveInput struct {
	Deployment *v1alpha1.Deployment
	Provider   *v1alpha1.Provider
}

// RemoveResult describes the outcome of a Remove call. If the adapter
// still has cleanup pending it returns an empty RemoveFinalizers slice
// (or omits it); the reconciler will retry on the next pass.
type RemoveResult struct {
	// Conditions to merge into Deployment.Status (typically
	// Progressing with Reason="Terminating", then Ready=False with
	// Reason="Removed" on completion).
	Conditions []v1alpha1.Condition

	// RemoveFinalizers lists adapter-owned finalizer tokens to drop.
	// Empty means teardown is still in-flight; keep trying.
	RemoveFinalizers []string
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
// Provider adapter (separate surface from Deployment adapter).
// -----------------------------------------------------------------------------

// ProviderAdapter exposes platform-specific provider CRUD. Enterprise
// uses this to plug cloud-provider-backed Providers (for example,
// managed GKE / EKS clusters where provider lifecycle involves
// infrastructure calls, not just row writes).
//
// OSS provides a trivial ProviderAdapter for local + kubernetes
// platforms whose Register/Get/List/Update/Delete are pure Store
// operations.
type ProviderAdapter interface {
	Platform() string

	// Validate checks Provider.Spec.Config can parse into the
	// adapter's typed internal shape. Called after Store.Upsert on
	// a Provider row so misconfig surfaces as a ProviderConfigured
	// condition. Idempotent; no side effects beyond parsing.
	Validate(ctx context.Context, provider *v1alpha1.Provider) error
}

// ProviderPlatformAdapter defines provider CRUD behavior for a provider
// platform type. Pre-dates the split between ProviderAdapter (declarative
// validation) and DeploymentAdapter (Apply/Remove/Logs/Discover);
// retained for enterprise builds whose provider-side surface is still
// imperative.
type ProviderPlatformAdapter interface {
	Platform() string
	ListProviders(ctx context.Context) ([]*v1alpha1.Provider, error)
	CreateProvider(ctx context.Context, provider *v1alpha1.Provider) (*v1alpha1.Provider, error)
	GetProvider(ctx context.Context, providerID string) (*v1alpha1.Provider, error)
	UpdateProvider(ctx context.Context, providerID string, provider *v1alpha1.Provider) (*v1alpha1.Provider, error)
	DeleteProvider(ctx context.Context, providerID string) error
}
