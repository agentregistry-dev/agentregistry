package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestSourceIndexIncludesTerminatingRowsForFinalizedKinds(t *testing.T) {
	stores := map[string]*v1alpha1store.Store{
		v1alpha1.KindDeployment: nil,
		v1alpha1.KindRuntime:    nil,
	}

	sources := NewSourceIndex(stores)
	require.False(t, sources.kinds[v1alpha1.KindDeployment].IncludeTerminating)
	require.False(t, sources.kinds[v1alpha1.KindRuntime].IncludeTerminating)

	sources = NewSourceIndex(stores, SourceIndexOptions{
		InitialFinalizers: map[string]func(v1alpha1.Object) []string{
			v1alpha1.KindRuntime: func(v1alpha1.Object) []string { return nil },
		},
	})
	require.True(t, sources.kinds[v1alpha1.KindRuntime].IncludeTerminating)
	require.False(t, sources.kinds[v1alpha1.KindDeployment].IncludeTerminating)
}

func TestSourceIndexSourcesStartUnsyncedUntilMarked(t *testing.T) {
	stores := map[string]*v1alpha1store.Store{
		v1alpha1.KindDeployment: nil,
	}
	sources := NewSourceIndex(stores)
	require.False(t, sources.Deployments.HasSynced())

	sources.markSynced()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.True(t, sources.Deployments.WaitUntilSynced(ctx.Done()))
	require.True(t, sources.Deployments.HasSynced())
}
