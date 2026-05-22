package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestSourceIndexProjectionPolicyDefaultsAndOverrides(t *testing.T) {
	stores := map[string]*v1alpha1store.Store{
		v1alpha1.KindDeployment: nil,
		v1alpha1.KindRuntime:    nil,
	}

	sources := NewSourceIndex(stores)
	require.True(t, sources.kinds[v1alpha1.KindDeployment].IncludeTerminating)
	require.False(t, sources.kinds[v1alpha1.KindRuntime].IncludeTerminating)

	sources = NewSourceIndex(stores, SourceIndexOptions{
		ProjectionPolicies: map[string]v1alpha1.ProjectionPolicy{
			v1alpha1.KindRuntime: {IncludeTerminating: true},
		},
	})
	require.True(t, sources.kinds[v1alpha1.KindRuntime].IncludeTerminating)
	require.True(t, sources.kinds[v1alpha1.KindDeployment].IncludeTerminating)
}
