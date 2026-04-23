//go:build integration

package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/noop"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// seedV1Alpha1Fixtures creates a MCPServer + Provider + Deployment row set
// in a fresh pool so coordinator tests don't re-derive the fixture. Returns
// the store map + the Deployment metadata coordinates.
func seedV1Alpha1Fixtures(t *testing.T) (map[string]*internaldb.Store, *v1alpha1.Deployment) {
	t.Helper()
	pool := internaldb.NewV1Alpha1TestPool(t)
	stores := internaldb.NewV1Alpha1Stores(pool)
	ctx := context.Background()

	mcpSpec, err := json.Marshal(v1alpha1.MCPServerSpec{
		Description: "noop mcp server",
		Remotes:     []v1alpha1.MCPTransport{{Type: "streamable-http", URL: "https://example.test/mcp"}},
	})
	require.NoError(t, err)
	_, err = stores[v1alpha1.KindMCPServer].Upsert(ctx, "default", "weather", "1.0.0", mcpSpec, nil, internaldb.UpsertOpts{})
	require.NoError(t, err)

	providerSpec, err := json.Marshal(v1alpha1.ProviderSpec{Platform: noop.Platform})
	require.NoError(t, err)
	_, err = stores[v1alpha1.KindProvider].Upsert(ctx, "default", "noop-provider", "1", providerSpec, nil, internaldb.UpsertOpts{})
	require.NoError(t, err)

	depSpec, err := json.Marshal(v1alpha1.DeploymentSpec{
		TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Version: "1.0.0"},
		ProviderRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindProvider, Name: "noop-provider", Version: "1"},
		DesiredState: v1alpha1.DesiredStateDeployed,
	})
	require.NoError(t, err)
	upsertRes, err := stores[v1alpha1.KindDeployment].Upsert(ctx, "default", "weather-noop", "1", depSpec, nil, internaldb.UpsertOpts{})
	require.NoError(t, err)

	deployment := &v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-noop", Version: "1", Generation: upsertRes.Generation},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Version: "1.0.0"},
			ProviderRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindProvider, Name: "noop-provider", Version: "1"},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}
	return stores, deployment
}

func TestV1Alpha1Coordinator_ApplyWritesConditionsAndFinalizer(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	coord := NewV1Alpha1Coordinator(V1Alpha1Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   internaldb.NewV1Alpha1Getter(stores),
	})

	require.NoError(t, coord.Apply(ctx, deployment))

	raw, err := stores[v1alpha1.KindDeployment].Get(ctx, "default", "weather-noop", "1")
	require.NoError(t, err)
	require.NotNil(t, raw.Status.GetCondition("Ready"), "noop adapter should have written Ready condition")
	require.Contains(t, raw.Metadata.Finalizers, noop.FinalizerName)
	require.Contains(t, raw.Metadata.Annotations, "platforms.agentregistry.solo.io/noop/applied-at")
	require.Equal(t, deployment.Metadata.Generation, raw.Status.ObservedGeneration)
}

func TestV1Alpha1Coordinator_ApplyPreservesExistingAnnotations(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	err := stores[v1alpha1.KindDeployment].PatchAnnotations(ctx, "default", "weather-noop", "1", func(annotations map[string]string) map[string]string {
		annotations["keep"] = "me"
		return annotations
	})
	require.NoError(t, err)

	coord := NewV1Alpha1Coordinator(V1Alpha1Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   internaldb.NewV1Alpha1Getter(stores),
	})

	require.NoError(t, coord.Apply(ctx, deployment))

	raw, err := stores[v1alpha1.KindDeployment].Get(ctx, "default", "weather-noop", "1")
	require.NoError(t, err)
	require.Equal(t, "me", raw.Metadata.Annotations["keep"])
	require.Contains(t, raw.Metadata.Annotations, "platforms.agentregistry.solo.io/noop/applied-at")
}

func TestV1Alpha1Coordinator_RemoveClearsFinalizer(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	coord := NewV1Alpha1Coordinator(V1Alpha1Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   internaldb.NewV1Alpha1Getter(stores),
	})

	require.NoError(t, coord.Apply(ctx, deployment))
	require.NoError(t, coord.Remove(ctx, deployment))

	raw, err := stores[v1alpha1.KindDeployment].Get(ctx, "default", "weather-noop", "1")
	require.NoError(t, err)
	require.NotContains(t, raw.Metadata.Finalizers, noop.FinalizerName, "Remove should drop adapter finalizer")
	ready := raw.Status.GetCondition("Ready")
	require.NotNil(t, ready)
	require.Equal(t, v1alpha1.ConditionFalse, ready.Status)
}

func TestV1Alpha1Coordinator_UnsupportedPlatform(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	coord := NewV1Alpha1Coordinator(V1Alpha1Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{}, // empty — no adapter for "noop"
		Getter:   internaldb.NewV1Alpha1Getter(stores),
	})

	err := coord.Apply(ctx, deployment)
	require.Error(t, err)
	var unsupported *UnsupportedDeploymentPlatformError
	require.True(t, errors.As(err, &unsupported), "expected UnsupportedDeploymentPlatformError, got %v", err)
	require.Equal(t, noop.Platform, unsupported.Platform)
}

func TestV1Alpha1Coordinator_DanglingTargetRef(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	// Point the deployment at a MCPServer that doesn't exist.
	deployment.Spec.TargetRef.Name = "does-not-exist"

	coord := NewV1Alpha1Coordinator(V1Alpha1Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   internaldb.NewV1Alpha1Getter(stores),
	})

	err := coord.Apply(ctx, deployment)
	require.Error(t, err)
	require.ErrorIs(t, err, v1alpha1.ErrDanglingRef)
}

func TestV1Alpha1Coordinator_Discover_ReturnsAdapterResults(t *testing.T) {
	stores, _ := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	coord := NewV1Alpha1Coordinator(V1Alpha1Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   internaldb.NewV1Alpha1Getter(stores),
	})

	provider := &v1alpha1.Provider{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "noop-provider", Version: "1"},
		Spec:     v1alpha1.ProviderSpec{Platform: noop.Platform},
	}
	results, err := coord.Discover(ctx, provider)
	require.NoError(t, err)
	require.Empty(t, results, "noop.Discover reports nothing")
}
