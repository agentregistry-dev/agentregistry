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
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// seedV1Alpha1Fixtures creates a MCPServer + Provider + Deployment row set
// in a fresh pool so coordinator tests don't re-derive the fixture. Returns
// the store map + the Deployment metadata coordinates.
func seedV1Alpha1Fixtures(t *testing.T) (map[string]*v1alpha1store.Store, *v1alpha1.Deployment) {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool)
	ctx := context.Background()

	mcpSpec, err := json.Marshal(v1alpha1.MCPServerSpec{
		Description: "noop mcp server",
		Remotes:     []v1alpha1.MCPTransport{{Type: "streamable-http", URL: "https://example.test/mcp"}},
	})
	require.NoError(t, err)
	_, err = stores[v1alpha1.KindMCPServer].Upsert(ctx, "default", "weather", "1.0.0", mcpSpec, v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	providerSpec, err := json.Marshal(v1alpha1.ProviderSpec{Platform: noop.Platform})
	require.NoError(t, err)
	_, err = stores[v1alpha1.KindProvider].Upsert(ctx, "default", "noop-provider", "1", providerSpec, v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	depSpec, err := json.Marshal(v1alpha1.DeploymentSpec{
		TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Version: "1.0.0"},
		ProviderRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindProvider, Name: "noop-provider", Version: "1"},
		DesiredState: v1alpha1.DesiredStateDeployed,
	})
	require.NoError(t, err)
	upsertRes, err := stores[v1alpha1.KindDeployment].Upsert(ctx, "default", "weather-noop", "1", depSpec, v1alpha1store.UpsertOpts{})
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

func TestCoordinator_ApplyWritesConditionsAndAnnotations(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	coord := NewCoordinator(Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   internaldb.NewGetter(stores),
	})

	require.NoError(t, coord.Apply(ctx, deployment))

	raw, err := stores[v1alpha1.KindDeployment].Get(ctx, "default", "weather-noop", "1")
	require.NoError(t, err)
	// RawObject.Status is opaque JSONB bytes; decode via the Status
	// storage codec to reach the typed Conditions / ObservedGeneration
	// fields the coordinator writes.
	var status v1alpha1.Status
	require.NoError(t, v1alpha1.UnmarshalStatusFromStorage(raw.Status, &status))
	require.NotNil(t, status.GetCondition("Ready"), "noop adapter should have written Ready condition")
	require.Contains(t, raw.Metadata.Annotations, "platforms.agentregistry.solo.io/noop/applied-at")
	require.Equal(t, deployment.Metadata.Generation, status.ObservedGeneration)
}

func TestCoordinator_ApplyPreservesExistingAnnotations(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	err := stores[v1alpha1.KindDeployment].PatchAnnotations(ctx, "default", "weather-noop", "1", func(annotations map[string]string) map[string]string {
		annotations["keep"] = "me"
		return annotations
	})
	require.NoError(t, err)

	coord := NewCoordinator(Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   internaldb.NewGetter(stores),
	})

	require.NoError(t, coord.Apply(ctx, deployment))

	raw, err := stores[v1alpha1.KindDeployment].Get(ctx, "default", "weather-noop", "1")
	require.NoError(t, err)
	require.Equal(t, "me", raw.Metadata.Annotations["keep"])
	require.Contains(t, raw.Metadata.Annotations, "platforms.agentregistry.solo.io/noop/applied-at")
}

func TestCoordinator_RemoveWritesRemovedCondition(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	coord := NewCoordinator(Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   internaldb.NewGetter(stores),
	})

	require.NoError(t, coord.Apply(ctx, deployment))
	require.NoError(t, coord.Remove(ctx, deployment))

	raw, err := stores[v1alpha1.KindDeployment].Get(ctx, "default", "weather-noop", "1")
	require.NoError(t, err)
	var status v1alpha1.Status
	require.NoError(t, v1alpha1.UnmarshalStatusFromStorage(raw.Status, &status))
	ready := status.GetCondition("Ready")
	require.NotNil(t, ready)
	require.Equal(t, v1alpha1.ConditionFalse, ready.Status)
}

func TestCoordinator_UnsupportedPlatform(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	coord := NewCoordinator(Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{}, // empty — no adapter for "noop"
		Getter:   internaldb.NewGetter(stores),
	})

	err := coord.Apply(ctx, deployment)
	require.Error(t, err)
	var unsupported *UnsupportedDeploymentPlatformError
	require.True(t, errors.As(err, &unsupported), "expected UnsupportedDeploymentPlatformError, got %v", err)
	require.Equal(t, noop.Platform, unsupported.Platform)
}

func TestCoordinator_DanglingTargetRef(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	// Point the deployment at a MCPServer that doesn't exist.
	deployment.Spec.TargetRef.Name = "does-not-exist"

	coord := NewCoordinator(Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   internaldb.NewGetter(stores),
	})

	err := coord.Apply(ctx, deployment)
	require.Error(t, err)
	require.ErrorIs(t, err, v1alpha1.ErrDanglingRef)
}

func TestCoordinator_Discover_ReturnsAdapterResults(t *testing.T) {
	stores, _ := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	coord := NewCoordinator(Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   internaldb.NewGetter(stores),
	})

	provider := &v1alpha1.Provider{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "noop-provider", Version: "1"},
		Spec:     v1alpha1.ProviderSpec{Platform: noop.Platform},
	}
	results, err := coord.Discover(ctx, provider)
	require.NoError(t, err)
	require.Empty(t, results, "noop.Discover reports nothing")
}
