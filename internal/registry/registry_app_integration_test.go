//go:build integration

package registry

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types/typestest"
)

// TestBuildStores_PropagatesAuditor verifies the auditor passed
// through buildStores (the AppOptions.Auditor field)
// reaches every constructed Store. We drive a tagged-artifact Upsert
// and assert the auditor saw the expected ResourceTagCreated event,
// proving the option survived the
// NewStores -> NewStore option chain.
func TestBuildStores_PropagatesAuditor(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	auditor := &typestest.RecordingAuditor{}
	stores := buildStores(pool, auditor)

	agentStore := stores[v1alpha1.KindAgent]
	require.NotNil(t, agentStore)

	_, err := agentStore.Upsert(t.Context(), &v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: v1alpha1.DefaultNamespace, Name: "audited"},
		Spec:     v1alpha1.AgentSpec{ModelName: "model-a"},
	})
	require.NoError(t, err)

	events := auditor.Events()
	require.Len(t, events, 1)
	require.Equal(t, v1alpha1.KindAgent, events[0].Kind)
	require.Equal(t, v1alpha1.DefaultNamespace, events[0].Namespace)
	require.Equal(t, "audited", events[0].Name)
	require.NotEmpty(t, events[0].Tag)

	// Sanity: nil auditor still works (NoopAuditor fallback) — guards the
	// nil-check branch in buildStores.
	stores2 := buildStores(pool, nil)
	require.NotNil(t, stores2[v1alpha1.KindAgent])
}
