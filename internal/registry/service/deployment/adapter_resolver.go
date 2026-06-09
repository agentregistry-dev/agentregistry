package deployment

import (
	"context"
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// AdapterResolver resolves Deployment runtime adapters for adjacent operations
// such as logs. The Deployment controller is the only built-in lifecycle path
// that may call adapter Apply/Remove.
type AdapterResolver struct {
	adapters map[string]types.DeploymentAdapter
	getter   v1alpha1.GetterFunc
}

// ResolverDependencies bundles the adapter resolver inputs.
type ResolverDependencies struct {
	// Adapters is the platform -> adapter map. AdapterResolver looks up by
	// Runtime.Spec.Type; unmapped platforms surface
	// UnsupportedDeploymentRuntimeError.
	Adapters map[string]types.DeploymentAdapter
	// Getter fetches typed Objects by ref. Logs uses it to resolve
	// Deployment.Spec.RuntimeRef.
	Getter v1alpha1.GetterFunc
}

// NewAdapterResolver constructs an adapter resolver from its dependencies.
// Adapters must be non-nil (an empty map is fine for tests that never
// dispatch). Getter may be nil only when callers do not resolve deployments.
func NewAdapterResolver(deps ResolverDependencies) *AdapterResolver {
	if deps.Adapters == nil {
		deps.Adapters = map[string]types.DeploymentAdapter{}
	}
	return &AdapterResolver{
		adapters: deps.Adapters,
		getter:   deps.Getter,
	}
}

// Logs streams logs from the deployed workload. Returns an
// UnsupportedDeploymentRuntimeError if no adapter matches the runtime.
func (r *AdapterResolver) Logs(ctx context.Context, deployment *v1alpha1.Deployment, in types.LogsInput) (<-chan types.LogLine, error) {
	if deployment == nil {
		return nil, fmt.Errorf("%w: deployment is required", pkgdb.ErrInvalidInput)
	}
	runtime, err := r.resolveRuntime(ctx, deployment)
	if err != nil {
		return nil, err
	}
	adapter, err := r.resolveAdapter(runtime.Spec.Type)
	if err != nil {
		return nil, err
	}
	in.Deployment = deployment
	return adapter.Logs(ctx, in)
}

func (r *AdapterResolver) resolveRuntime(ctx context.Context, deployment *v1alpha1.Deployment) (*v1alpha1.Runtime, error) {
	if r == nil || r.getter == nil {
		return nil, fmt.Errorf("%w: deployment adapter resolver getter is nil", pkgdb.ErrInvalidInput)
	}
	ref := deployment.Spec.RuntimeRef
	ref.Namespace = refNamespace(ref.Namespace, deployment.Metadata.NamespaceOrDefault())
	obj, err := r.getter(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("resolve runtimeRef %s/%s: %w", ref.Namespace, ref.Name, err)
	}
	runtime, ok := obj.(*v1alpha1.Runtime)
	if !ok || runtime == nil {
		return nil, fmt.Errorf("runtimeRef %s/%s did not resolve to a Runtime", ref.Namespace, ref.Name)
	}
	return runtime, nil
}

func (r *AdapterResolver) resolveAdapter(runtimeType string) (types.DeploymentAdapter, error) {
	normalized := strings.TrimSpace(runtimeType)
	if normalized == "" {
		return nil, fmt.Errorf("%w: runtime type is empty", pkgdb.ErrInvalidInput)
	}
	if r == nil {
		return nil, &UnsupportedDeploymentRuntimeError{Type: normalized}
	}
	adapter, ok := r.adapters[normalized]
	if !ok || adapter == nil {
		return nil, &UnsupportedDeploymentRuntimeError{Type: normalized}
	}
	return adapter, nil
}

func refNamespace(refNamespace, fallback string) string {
	if refNamespace != "" {
		return refNamespace
	}
	if fallback != "" {
		return fallback
	}
	return v1alpha1.DefaultNamespace
}
