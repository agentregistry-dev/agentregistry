package deployment

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// Coordinator is the v1alpha1-native orchestrator that glues the
// generic v1alpha1store.Store to the set of registered DeploymentAdapter
// implementations. It is the synchronous counterpart to the Phase 2 KRT
// reconciler — HTTP handlers call it directly after Store.Upsert to drive
// adapter.Apply / adapter.Remove and thread the results back into the
// Deployment row via PatchStatus + annotation merges.
//
// Responsibilities:
//  1. resolve TargetRef + RuntimeRef via the supplied GetterFunc.
//  2. dispatch to the adapter keyed by Runtime.Spec.Type.
//  3. merge returned conditions into Deployment.Status and adapter-owned
//     annotations into Deployment.Metadata.Annotations.
//  4. surface a structured error when no adapter is registered for a
//     runtime's platform.
//
// Coordinator is NOT responsible for Upserting the Deployment row — that
// happens upstream at the apply handler. Coordinator.Apply MUST be called
// only after the row is persisted so status writes land on a real row.
type Coordinator struct {
	stores   map[string]*v1alpha1store.Store
	adapters map[string]types.DeploymentAdapter
	getter   v1alpha1.GetterFunc
}

// Dependencies bundles the coordinator's inputs.
type Dependencies struct {
	// Stores is the per-Kind generic Store map — output of
	// internaldb.NewStores.
	Stores map[string]*v1alpha1store.Store
	// Adapters is the platform → adapter map. Coordinator looks up by
	// Runtime.Spec.Type; unmapped platforms surface
	// UnsupportedDeploymentRuntimeError.
	Adapters map[string]types.DeploymentAdapter
	// Getter fetches typed Objects by ref. Coordinator uses it for
	// TargetRef + RuntimeRef; adapters may use the same GetterFunc for
	// nested refs (e.g. AgentSpec.MCPServers).
	Getter v1alpha1.GetterFunc
}

// NewCoordinator constructs a coordinator from its dependencies.
// Stores and Adapters must be non-nil (empty maps are fine for tests that
// never dispatch); Getter may be nil if the caller knows no nested-ref
// resolution is needed.
func NewCoordinator(deps Dependencies) *Coordinator {
	if deps.Stores == nil {
		deps.Stores = map[string]*v1alpha1store.Store{}
	}
	if deps.Adapters == nil {
		deps.Adapters = map[string]types.DeploymentAdapter{}
	}
	return &Coordinator{
		stores:   deps.Stores,
		adapters: deps.Adapters,
		getter:   deps.Getter,
	}
}

// Apply drives a Deployment to its desired state on the backing platform.
// Preconditions: the Deployment row exists (Store.Upsert has run); the
// TargetRef + RuntimeRef resolve via the coordinator's Getter.
//
// Flow:
//  1. resolve target (Agent or MCPServer) and runtime via Getter.
//  2. pick the DeploymentAdapter keyed by Runtime.Spec.Type.
//  3. reject the apply if the adapter doesn't support the target Kind.
//  4. call adapter.Apply with the resolved inputs.
//  5. merge returned conditions into Deployment.Status via PatchStatus.
//  6. merge adapter-owned annotations onto Deployment.Metadata.Annotations.
//
// Conditions are merged, not replaced — SetCondition dedups by Type and
// preserves LastTransitionTime when Status is unchanged.
func (c *Coordinator) Apply(ctx context.Context, deployment *v1alpha1.Deployment) error {
	if deployment == nil {
		return fmt.Errorf("%w: deployment is required", pkgdb.ErrInvalidInput)
	}
	if c.getter == nil {
		return fmt.Errorf("apply: coordinator getter is nil")
	}

	target, err := c.resolveTarget(ctx, deployment)
	if err != nil {
		if errors.Is(err, v1alpha1.ErrDanglingRef) {
			return c.persistReferencePending(ctx, deployment, err)
		}
		return err
	}
	runtime, err := c.resolveRuntime(ctx, deployment)
	if err != nil {
		if errors.Is(err, v1alpha1.ErrDanglingRef) {
			return c.persistReferencePending(ctx, deployment, err)
		}
		return err
	}

	adapter, err := c.resolveAdapter(runtime.Spec.Type)
	if err != nil {
		return err
	}
	if !adapterSupportsKind(adapter, target.GetKind()) {
		return fmt.Errorf("%w: adapter %q does not support target kind %q",
			pkgdb.ErrInvalidInput, adapter.Type(), target.GetKind())
	}

	result, err := adapter.Apply(ctx, types.ApplyInput{
		Deployment: deployment,
		Target:     target,
		Runtime:    runtime,
		Getter:     c.getter,
	})
	if err != nil {
		return fmt.Errorf("adapter %q apply: %w", adapter.Type(), err)
	}

	return c.persistApplyResult(ctx, deployment, result)
}

func (c *Coordinator) persistReferencePending(ctx context.Context, deployment *v1alpha1.Deployment, cause error) error {
	message := "referenced resource is not available yet"
	if cause != nil {
		message = cause.Error()
	}
	return c.persistApplyResult(ctx, deployment, &types.ApplyResult{
		Conditions: []v1alpha1.Condition{{
			Type:               "Ready",
			Status:             v1alpha1.ConditionFalse,
			Reason:             "ReferencePending",
			Message:            message,
			ObservedGeneration: deployment.Metadata.Generation,
		}},
	})
}

// Remove tears down a Deployment's runtime resources via the adapter and
// merges the resulting Removed condition into the row's status. Called
// after the row's DeletionTimestamp is set (soft-delete path) or when
// the user flips DesiredState=undeployed.
//
// Idempotent — calling Remove twice is safe: status simply converges to
// Ready=False again. Row lifetime past this point belongs to the
// soft-delete + GC pass; the adapter contributes no finalizer tokens.
func (c *Coordinator) Remove(ctx context.Context, deployment *v1alpha1.Deployment) error {
	if deployment == nil {
		return fmt.Errorf("%w: deployment is required", pkgdb.ErrInvalidInput)
	}
	runtime, err := c.resolveRuntime(ctx, deployment)
	if err != nil {
		return err
	}
	adapter, err := c.resolveAdapter(runtime.Spec.Type)
	if err != nil {
		return err
	}

	result, err := adapter.Remove(ctx, types.RemoveInput{
		Deployment: deployment,
		Runtime:    runtime,
	})
	if err != nil {
		return fmt.Errorf("adapter %q remove: %w", adapter.Type(), err)
	}

	return c.persistRemoveResult(ctx, deployment, result)
}

// Logs streams logs from the deployed workload. Returns an
// UnsupportedDeploymentRuntimeError if no adapter matches the runtime.
func (c *Coordinator) Logs(ctx context.Context, deployment *v1alpha1.Deployment, in types.LogsInput) (<-chan types.LogLine, error) {
	if deployment == nil {
		return nil, fmt.Errorf("%w: deployment is required", pkgdb.ErrInvalidInput)
	}
	runtime, err := c.resolveRuntime(ctx, deployment)
	if err != nil {
		return nil, err
	}
	adapter, err := c.resolveAdapter(runtime.Spec.Type)
	if err != nil {
		return nil, err
	}
	in.Deployment = deployment
	return adapter.Logs(ctx, in)
}

// Discover enumerates out-of-band workloads for the supplied Runtime.
// Empty slice + nil error means the adapter found nothing; mismatched
// platforms surface UnsupportedDeploymentRuntimeError.
func (c *Coordinator) Discover(ctx context.Context, runtime *v1alpha1.Runtime) ([]types.DiscoveryResult, error) {
	if runtime == nil {
		return nil, fmt.Errorf("%w: runtime is required", pkgdb.ErrInvalidInput)
	}
	adapter, err := c.resolveAdapter(runtime.Spec.Type)
	if err != nil {
		return nil, err
	}
	return adapter.Discover(ctx, types.DiscoverInput{Runtime: runtime})
}

// resolveTarget fetches the Deployment.Spec.TargetRef. Blank ref namespaces
// inherit from the Deployment's namespace — same rule as v1alpha1 Object
// ResolveRefs so a deployment targeting `Agent alice` in the same
// namespace works without repeating the namespace literal.
func (c *Coordinator) resolveTarget(ctx context.Context, deployment *v1alpha1.Deployment) (v1alpha1.Object, error) {
	ref := deployment.Spec.TargetRef
	if ref.Namespace == "" {
		ref.Namespace = deployment.Metadata.Namespace
	}
	obj, err := c.getter(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve targetRef %s/%s@%s: %w", ref.Namespace, ref.Name, ref.Tag, err)
	}
	if obj == nil {
		return nil, fmt.Errorf("resolve targetRef %s/%s: nil object", ref.Namespace, ref.Name)
	}
	return obj, nil
}

// resolveRuntime fetches the Deployment.Spec.RuntimeRef with the same
// blank-namespace inheritance rule as resolveTarget, then type-asserts to
// *v1alpha1.Runtime.
func (c *Coordinator) resolveRuntime(ctx context.Context, deployment *v1alpha1.Deployment) (*v1alpha1.Runtime, error) {
	ref := deployment.Spec.RuntimeRef
	if ref.Namespace == "" {
		ref.Namespace = deployment.Metadata.Namespace
	}
	obj, err := c.getter(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve runtimeRef %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	runtime, ok := obj.(*v1alpha1.Runtime)
	if !ok || runtime == nil {
		return nil, fmt.Errorf("runtimeRef %s/%s did not resolve to a Runtime", ref.Namespace, ref.Name)
	}
	return runtime, nil
}

// resolveAdapter looks up the registered DeploymentAdapter for a platform
// string. Returns a sentinel UnsupportedDeploymentRuntimeError so callers
// (MCP tool surface, HTTP handler) can discriminate "no adapter" from
// transient plumbing errors.
func (c *Coordinator) resolveAdapter(runtimeType string) (types.DeploymentAdapter, error) {
	normalized := strings.TrimSpace(runtimeType)
	if normalized == "" {
		return nil, fmt.Errorf("%w: runtime type is empty", pkgdb.ErrInvalidInput)
	}
	adapter, ok := c.adapters[normalized]
	if !ok {
		return nil, &UnsupportedDeploymentRuntimeError{Type: normalized}
	}
	return adapter, nil
}

// persistApplyResult merges adapter-returned Conditions and
// RuntimeMetadata into the Deployment row in a single atomic patch —
// one observation of the adapter equals one row update, so operators
// never see partial state (status updated but annotations missing, etc).
//
// No finalizer plumbing today: deletion proceeds on the soft-delete +
// GC path. The orphan-reconciliation follow-up will reintroduce a
// dedicated mechanism using the retained `finalizers` DB column.
func (c *Coordinator) persistApplyResult(ctx context.Context, deployment *v1alpha1.Deployment, result *types.ApplyResult) error {
	if result == nil {
		return nil
	}
	store, err := c.deploymentStore()
	if err != nil {
		return err
	}
	patch := v1alpha1store.PatchOpts{}
	if len(result.Conditions) > 0 {
		patch.Status = v1alpha1.StatusPatcher(func(s *v1alpha1.Status) {
			for _, cond := range result.Conditions {
				s.SetCondition(cond)
			}
		})
	}
	if len(result.RuntimeMetadata) > 0 {
		patch.Annotations = func(annotations map[string]string) map[string]string {
			if annotations == nil {
				annotations = map[string]string{}
			}
			maps.Copy(annotations, result.RuntimeMetadata)
			return annotations
		}
	}
	if err := store.ApplyPatch(ctx, deployment.Metadata.Namespace, deployment.Metadata.Name, "", patch); err != nil {
		return fmt.Errorf("persist apply result: %w", err)
	}
	return nil
}

// persistRemoveResult merges adapter-returned Conditions for an
// already-removed deployment. The hook fires from the Delete
// PostDelete path, which now hard-deletes finalizer-free rows
// synchronously — so the row may already be gone by the time we get
// here, in which case there's nothing to patch and ErrNotFound is the
// expected (not failure) outcome. Adapters that want their teardown
// status reflected on the row should attach a finalizer at apply time,
// drain it after teardown, and let PurgeFinalized hard-delete on the
// next pass — the soft-delete branch leaves the row visible long
// enough for the patch to land.
func (c *Coordinator) persistRemoveResult(ctx context.Context, deployment *v1alpha1.Deployment, result *types.RemoveResult) error {
	if result == nil {
		return nil
	}
	store, err := c.deploymentStore()
	if err != nil {
		return err
	}
	patch := v1alpha1store.PatchOpts{}
	if len(result.Conditions) > 0 {
		patch.Status = v1alpha1.StatusPatcher(func(s *v1alpha1.Status) {
			for _, cond := range result.Conditions {
				s.SetCondition(cond)
			}
		})
	}
	if patch.Status == nil && patch.Annotations == nil {
		return nil
	}
	if err := store.ApplyPatch(ctx, deployment.Metadata.Namespace, deployment.Metadata.Name, "", patch); err != nil {
		if errors.Is(err, pkgdb.ErrNotFound) {
			// Row already hard-deleted (finalizer-free fast path) — no
			// place to record the Removed condition. Adapter teardown
			// already ran successfully; this is a clean exit.
			return nil
		}
		return fmt.Errorf("persist remove result: %w", err)
	}
	return nil
}

func (c *Coordinator) deploymentStore() (*v1alpha1store.Store, error) {
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
