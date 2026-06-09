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

func seedAdapterResolverFixtures(t *testing.T) (map[string]*v1alpha1store.Store, *v1alpha1.Deployment, *v1alpha1.Runtime) {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool, v1alpha1store.TestSchemaRegistry())
	ctx := context.Background()

	runtime := &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "noop-runtime"},
		Spec:     v1alpha1.RuntimeSpec{Type: noop.RuntimeType},
	}
	_, err := stores[v1alpha1.KindRuntime].Upsert(ctx, runtime)
	require.NoError(t, err)

	deployment := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-noop"},
		Spec: v1alpha1.DeploymentSpec{
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: runtime.Metadata.Name},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}
	return stores, deployment, runtime
}

func TestAdapterResolver_LogsUsesRuntimeAdapter(t *testing.T) {
	stores, deployment, _ := seedAdapterResolverFixtures(t)
	resolver := NewAdapterResolver(ResolverDependencies{
		Adapters: map[string]types.DeploymentAdapter{noop.RuntimeType: noop.New()},
		Getter:   internaldb.NewGetter(stores),
	})

	ch, err := resolver.Logs(context.Background(), deployment, types.LogsInput{})
	require.NoError(t, err)
	_, ok := <-ch
	require.False(t, ok, "noop adapter returns a closed log channel")
}

func TestAdapterResolver_UnsupportedRuntimeType(t *testing.T) {
	stores, deployment, _ := seedAdapterResolverFixtures(t)
	resolver := NewAdapterResolver(ResolverDependencies{
		Adapters: map[string]types.DeploymentAdapter{},
		Getter:   internaldb.NewGetter(stores),
	})

	_, err := resolver.Logs(context.Background(), deployment, types.LogsInput{})
	require.Error(t, err)
	var unsupported *UnsupportedDeploymentRuntimeError
	require.True(t, errors.As(err, &unsupported), "expected UnsupportedDeploymentRuntimeError, got %v", err)
	require.Equal(t, noop.RuntimeType, unsupported.Type)
}
