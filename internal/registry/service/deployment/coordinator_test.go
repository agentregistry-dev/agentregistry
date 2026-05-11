//go:build integration

package deployment

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/noop"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// seedV1Alpha1Fixtures creates a MCPServer + Runtime + Deployment row set
// in a fresh pool so coordinator tests don't re-derive the fixture. Returns
// the store map + the Deployment metadata coordinates.
func seedV1Alpha1Fixtures(t *testing.T) (map[string]*v1alpha1store.Store, *v1alpha1.Deployment) {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool)
	ctx := context.Background()

	_, err := stores[v1alpha1.KindMCPServer].Upsert(ctx, &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather"},
		Spec: v1alpha1.MCPServerSpec{
			Description: "noop mcp server",
			Source: &v1alpha1.MCPServerSource{
				Package: &v1alpha1.MCPPackage{
					RegistryType: v1alpha1.RegistryTypeOCI,
					Identifier:   "ghcr.io/example/weather:1.0.0",
					Transport:    v1alpha1.MCPTransport{Type: "stdio"},
				},
			},
		},
	})
	require.NoError(t, err)

	_, err = stores[v1alpha1.KindRuntime].Upsert(ctx, &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "noop-runtime"},
		Spec:     v1alpha1.RuntimeSpec{Type: noop.RuntimeType},
	})
	require.NoError(t, err)

	_, err = stores[v1alpha1.KindDeployment].Upsert(ctx, &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-noop"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather"},
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "noop-runtime"},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	})
	require.NoError(t, err)

	deployment := &v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-noop"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather"},
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "noop-runtime"},
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
		Adapters: map[string]types.DeploymentAdapter{noop.RuntimeType: noop.New()},
		Getter:   internaldb.NewGetter(stores),
	})

	require.NoError(t, coord.Apply(ctx, deployment))

	raw, err := stores[v1alpha1.KindDeployment].Get(ctx, "default", "weather-noop", "")
	require.NoError(t, err)
	// RawObject.Status is opaque JSONB bytes; decode via the Status
	// storage codec to reach the typed Conditions field the coordinator
	// writes.
	var status v1alpha1.Status
	require.NoError(t, v1alpha1.UnmarshalStatusFromStorage(raw.Status, &status))
	require.NotNil(t, status.GetCondition("Ready"), "noop adapter should have written Ready condition")
	require.Contains(t, raw.Metadata.Annotations, "runtimes.agentregistry.solo.io/noop/applied-at")
}

func TestCoordinator_ApplyPreservesExistingAnnotations(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	err := stores[v1alpha1.KindDeployment].PatchAnnotations(ctx, "default", "weather-noop", "", func(annotations map[string]string) map[string]string {
		annotations["keep"] = "me"
		return annotations
	})
	require.NoError(t, err)

	coord := NewCoordinator(Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.RuntimeType: noop.New()},
		Getter:   internaldb.NewGetter(stores),
	})

	require.NoError(t, coord.Apply(ctx, deployment))

	raw, err := stores[v1alpha1.KindDeployment].Get(ctx, "default", "weather-noop", "")
	require.NoError(t, err)
	require.Equal(t, "me", raw.Metadata.Annotations["keep"])
	require.Contains(t, raw.Metadata.Annotations, "runtimes.agentregistry.solo.io/noop/applied-at")
}

func TestCoordinator_RemoveWritesRemovedCondition(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	coord := NewCoordinator(Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.RuntimeType: noop.New()},
		Getter:   internaldb.NewGetter(stores),
	})

	require.NoError(t, coord.Apply(ctx, deployment))
	require.NoError(t, coord.Remove(ctx, deployment))

	raw, err := stores[v1alpha1.KindDeployment].Get(ctx, "default", "weather-noop", "")
	require.NoError(t, err)
	var status v1alpha1.Status
	require.NoError(t, v1alpha1.UnmarshalStatusFromStorage(raw.Status, &status))
	ready := status.GetCondition("Ready")
	require.NotNil(t, ready)
	require.Equal(t, v1alpha1.ConditionFalse, ready.Status)
}

func TestCoordinator_UnsupportedRuntimeType(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	coord := NewCoordinator(Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{}, // empty — no adapter for "noop"
		Getter:   internaldb.NewGetter(stores),
	})

	err := coord.Apply(ctx, deployment)
	require.Error(t, err)
	var unsupported *UnsupportedDeploymentRuntimeError
	require.True(t, errors.As(err, &unsupported), "expected UnsupportedDeploymentRuntimeError, got %v", err)
	require.Equal(t, noop.RuntimeType, unsupported.Type)
}

func TestCoordinator_DanglingTargetRef(t *testing.T) {
	stores, deployment := seedV1Alpha1Fixtures(t)
	ctx := context.Background()

	// Point the deployment at a MCPServer that doesn't exist.
	deployment.Spec.TargetRef.Name = "does-not-exist"

	coord := NewCoordinator(Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.RuntimeType: noop.New()},
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
		Adapters: map[string]types.DeploymentAdapter{noop.RuntimeType: noop.New()},
		Getter:   internaldb.NewGetter(stores),
	})

	runtime := &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "noop-runtime"},
		Spec:     v1alpha1.RuntimeSpec{Type: noop.RuntimeType},
	}
	results, err := coord.Discover(ctx, runtime)
	require.NoError(t, err)
	require.Empty(t, results, "noop.Discover reports nothing")
}
