package deployment

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// V1Alpha1Coordinator is the v1alpha1-native orchestrator that glues the
// generic database.Store to the set of registered DeploymentAdapter
// implementations. It is the synchronous counterpart to the Phase 2 KRT
// reconciler — HTTP handlers call it directly after Store.Upsert to drive
// adapter.Apply / adapter.Remove and thread the results back into the
// Deployment row via PatchStatus + PatchFinalizers.
//
// Responsibilities:
//  1. resolve TargetRef + ProviderRef via the supplied GetterFunc.
//  2. dispatch to the adapter keyed by Provider.Spec.Platform.
//  3. merge returned conditions into Deployment.Status and returned
//     finalizer tokens into Deployment.Metadata.Finalizers.
//  4. surface a structured error when no adapter is registered for a
//     provider's platform.
//
// Coordinator is NOT responsible for Upserting the Deployment row — that
// happens upstream at the apply handler. Coordinator.Apply MUST be called
// only after the row is persisted so status writes land on a real row.
type V1Alpha1Coordinator struct {
	stores   map[string]*internaldb.Store
	adapters map[string]types.DeploymentAdapter
	getter   v1alpha1.GetterFunc
}

// V1Alpha1Dependencies bundles the coordinator's inputs.
type V1Alpha1Dependencies struct {
	// Stores is the per-Kind generic Store map — output of
	// internaldb.NewV1Alpha1Stores.
	Stores map[string]*internaldb.Store
	// Adapters is the platform → adapter map. Coordinator looks up by
	// Provider.Spec.Platform; unmapped platforms surface
	// UnsupportedDeploymentPlatformError.
	Adapters map[string]types.DeploymentAdapter
	// Getter fetches typed Objects by ref. Coordinator uses it for
	// TargetRef + ProviderRef; adapters may use the same GetterFunc for
	// nested refs (e.g. AgentSpec.MCPServers).
	Getter v1alpha1.GetterFunc
}

// NewV1Alpha1Coordinator constructs a coordinator from its dependencies.
// Stores and Adapters must be non-nil (empty maps are fine for tests that
// never dispatch); Getter may be nil if the caller knows no nested-ref
// resolution is needed.
func NewV1Alpha1Coordinator(deps V1Alpha1Dependencies) *V1Alpha1Coordinator {
	if deps.Stores == nil {
		deps.Stores = map[string]*internaldb.Store{}
	}
	if deps.Adapters == nil {
		deps.Adapters = map[string]types.DeploymentAdapter{}
	}
	return &V1Alpha1Coordinator{
		stores:   deps.Stores,
		adapters: deps.Adapters,
		getter:   deps.Getter,
	}
}

// Apply drives a Deployment to its desired state on the backing platform.
// Preconditions: the Deployment row exists (Store.Upsert has run); the
// TargetRef + ProviderRef resolve via the coordinator's Getter.
//
// Flow:
//  1. resolve target (Agent or MCPServer) and provider via Getter.
//  2. pick the DeploymentAdapter keyed by Provider.Spec.Platform.
//  3. reject the apply if the adapter doesn't support the target Kind.
//  4. call adapter.Apply with the resolved inputs.
//  5. merge returned conditions into Deployment.Status via PatchStatus.
//  6. add finalizer tokens via PatchFinalizers.
//
// Conditions are merged, not replaced — SetCondition dedups by Type and
// preserves LastTransitionTime when Status is unchanged.
func (c *V1Alpha1Coordinator) Apply(ctx context.Context, deployment *v1alpha1.Deployment) error {
	if deployment == nil {
		return fmt.Errorf("%w: deployment is required", pkgdb.ErrInvalidInput)
	}
	if c.getter == nil {
		return fmt.Errorf("apply: coordinator getter is nil")
	}

	target, err := c.resolveTarget(ctx, deployment)
	if err != nil {
		return err
	}
	provider, err := c.resolveProvider(ctx, deployment)
	if err != nil {
		return err
	}

	adapter, err := c.resolveAdapter(provider.Spec.Platform)
	if err != nil {
		return err
	}
	if !adapterSupportsKind(adapter, target.GetKind()) {
		return fmt.Errorf("%w: adapter %q does not support target kind %q",
			pkgdb.ErrInvalidInput, adapter.Platform(), target.GetKind())
	}

	result, err := adapter.Apply(ctx, types.ApplyInput{
		Deployment: deployment,
		Target:     target,
		Provider:   provider,
		Getter:     c.getter,
	})
	if err != nil {
		return fmt.Errorf("adapter %q apply: %w", adapter.Platform(), err)
	}

	return c.persistApplyResult(ctx, deployment, result)
}

// Remove tears down a Deployment's runtime resources via the adapter and
// drops the adapter-owned finalizer tokens from the row. Called after the
// row's DeletionTimestamp is set (soft-delete path) or when the user
// flips DesiredState=undeployed.
//
// Idempotent — calling Remove twice is safe: the adapter returns no
// finalizers to drop on the second pass and status simply converges to
// Ready=False again.
func (c *V1Alpha1Coordinator) Remove(ctx context.Context, deployment *v1alpha1.Deployment) error {
	if deployment == nil {
		return fmt.Errorf("%w: deployment is required", pkgdb.ErrInvalidInput)
	}
	provider, err := c.resolveProvider(ctx, deployment)
	if err != nil {
		return err
	}
	adapter, err := c.resolveAdapter(provider.Spec.Platform)
	if err != nil {
		return err
	}

	result, err := adapter.Remove(ctx, types.RemoveInput{
		Deployment: deployment,
		Provider:   provider,
	})
	if err != nil {
		return fmt.Errorf("adapter %q remove: %w", adapter.Platform(), err)
	}

	return c.persistRemoveResult(ctx, deployment, result)
}

// Logs streams logs from the deployed workload. Returns an
// UnsupportedDeploymentPlatformError if no adapter matches the provider.
func (c *V1Alpha1Coordinator) Logs(ctx context.Context, deployment *v1alpha1.Deployment, in types.LogsInput) (<-chan types.LogLine, error) {
	if deployment == nil {
		return nil, fmt.Errorf("%w: deployment is required", pkgdb.ErrInvalidInput)
	}
	provider, err := c.resolveProvider(ctx, deployment)
	if err != nil {
		return nil, err
	}
	adapter, err := c.resolveAdapter(provider.Spec.Platform)
	if err != nil {
		return nil, err
	}
	in.Deployment = deployment
	return adapter.Logs(ctx, in)
}

// Discover enumerates out-of-band workloads for the supplied Provider.
// Empty slice + nil error means the adapter found nothing; mismatched
// platforms surface UnsupportedDeploymentPlatformError.
func (c *V1Alpha1Coordinator) Discover(ctx context.Context, provider *v1alpha1.Provider) ([]types.DiscoveryResult, error) {
	if provider == nil {
		return nil, fmt.Errorf("%w: provider is required", pkgdb.ErrInvalidInput)
	}
	adapter, err := c.resolveAdapter(provider.Spec.Platform)
	if err != nil {
		return nil, err
	}
	return adapter.Discover(ctx, types.DiscoverInput{Provider: provider})
}

// resolveTarget fetches the Deployment.Spec.TargetRef. Blank ref namespaces
// inherit from the Deployment's namespace — same rule as v1alpha1 Object
// ResolveRefs so a deployment targeting `Agent alice` in the same
// namespace works without repeating the namespace literal.
func (c *V1Alpha1Coordinator) resolveTarget(ctx context.Context, deployment *v1alpha1.Deployment) (v1alpha1.Object, error) {
	ref := deployment.Spec.TargetRef
	if ref.Namespace == "" {
		ref.Namespace = deployment.Metadata.Namespace
	}
	obj, err := c.getter(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve targetRef %s/%s@%s: %w", ref.Namespace, ref.Name, ref.Version, err)
	}
	if obj == nil {
		return nil, fmt.Errorf("resolve targetRef %s/%s: nil object", ref.Namespace, ref.Name)
	}
	return obj, nil
}

// resolveProvider fetches the Deployment.Spec.ProviderRef with the same
// blank-namespace inheritance rule as resolveTarget, then type-asserts to
// *v1alpha1.Provider.
func (c *V1Alpha1Coordinator) resolveProvider(ctx context.Context, deployment *v1alpha1.Deployment) (*v1alpha1.Provider, error) {
	ref := deployment.Spec.ProviderRef
	if ref.Namespace == "" {
		ref.Namespace = deployment.Metadata.Namespace
	}
	obj, err := c.getter(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve providerRef %s/%s@%s: %w", ref.Namespace, ref.Name, ref.Version, err)
	}
	provider, ok := obj.(*v1alpha1.Provider)
	if !ok || provider == nil {
		return nil, fmt.Errorf("providerRef %s/%s did not resolve to a Provider", ref.Namespace, ref.Name)
	}
	return provider, nil
}

// resolveAdapter looks up the registered DeploymentAdapter for a platform
// string. Returns a sentinel UnsupportedDeploymentPlatformError so callers
// (MCP tool surface, HTTP handler) can discriminate "no adapter" from
// transient plumbing errors.
func (c *V1Alpha1Coordinator) resolveAdapter(platform string) (types.DeploymentAdapter, error) {
	normalized := strings.ToLower(strings.TrimSpace(platform))
	if normalized == "" {
		return nil, fmt.Errorf("%w: provider platform is empty", pkgdb.ErrInvalidInput)
	}
	adapter, ok := c.adapters[normalized]
	if !ok {
		return nil, &UnsupportedDeploymentPlatformError{Platform: normalized}
	}
	return adapter, nil
}

// persistApplyResult merges adapter-returned Conditions + AddFinalizers
// into the Deployment row. Status and Finalizers patches are independent
// writes; a failure on the finalizer patch still leaves the status patch
// applied so operators see the partial progress.
func (c *V1Alpha1Coordinator) persistApplyResult(ctx context.Context, deployment *v1alpha1.Deployment, result *types.ApplyResult) error {
	if result == nil {
		return nil
	}
	store, err := c.deploymentStore()
	if err != nil {
		return err
	}
	if len(result.Conditions) > 0 {
		if err := store.PatchStatus(ctx, deployment.Metadata.Namespace, deployment.Metadata.Name, deployment.Metadata.Version, func(s *v1alpha1.Status) {
			s.ObservedGeneration = deployment.Metadata.Generation
			for _, c := range result.Conditions {
				s.SetCondition(c)
			}
		}); err != nil {
			return fmt.Errorf("patch status: %w", err)
		}
	}
	if len(result.AddFinalizers) > 0 {
		if err := store.PatchFinalizers(ctx, deployment.Metadata.Namespace, deployment.Metadata.Name, deployment.Metadata.Version, func(finalizers []string) []string {
			for _, f := range result.AddFinalizers {
				if !slices.Contains(finalizers, f) {
					finalizers = append(finalizers, f)
				}
			}
			return finalizers
		}); err != nil {
			return fmt.Errorf("patch finalizers: %w", err)
		}
	}
	return nil
}

// persistRemoveResult merges adapter-returned Conditions + removes any
// finalizer tokens the adapter declared done.
func (c *V1Alpha1Coordinator) persistRemoveResult(ctx context.Context, deployment *v1alpha1.Deployment, result *types.RemoveResult) error {
	if result == nil {
		return nil
	}
	store, err := c.deploymentStore()
	if err != nil {
		return err
	}
	if len(result.Conditions) > 0 {
		if err := store.PatchStatus(ctx, deployment.Metadata.Namespace, deployment.Metadata.Name, deployment.Metadata.Version, func(s *v1alpha1.Status) {
			s.ObservedGeneration = deployment.Metadata.Generation
			for _, c := range result.Conditions {
				s.SetCondition(c)
			}
		}); err != nil {
			return fmt.Errorf("patch status: %w", err)
		}
	}
	if len(result.RemoveFinalizers) > 0 {
		if err := store.PatchFinalizers(ctx, deployment.Metadata.Namespace, deployment.Metadata.Name, deployment.Metadata.Version, func(finalizers []string) []string {
			out := finalizers[:0]
			for _, f := range finalizers {
				if !slices.Contains(result.RemoveFinalizers, f) {
					out = append(out, f)
				}
			}
			return out
		}); err != nil {
			return fmt.Errorf("patch finalizers: %w", err)
		}
	}
	return nil
}

func (c *V1Alpha1Coordinator) deploymentStore() (*internaldb.Store, error) {
	store, ok := c.stores[v1alpha1.KindDeployment]
	if !ok || store == nil {
		return nil, errors.New("coordinator: no Deployment store registered")
	}
	return store, nil
}

func adapterSupportsKind(adapter types.DeploymentAdapter, kind string) bool {
	if adapter == nil {
		return false
	}
	return slices.Contains(adapter.SupportedTargetKinds(), kind)
}
