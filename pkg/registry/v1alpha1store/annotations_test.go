//go:build integration

package v1alpha1store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestStore_AnnotationsRoundTrip(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	annotations := map[string]string{
		"security.agentregistry.solo.io/osv-status":    "clean",
		"internal.agentregistry.solo.io/import-source": "builtin-seed",
	}
	res, err := store.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: "ann", Annotations: annotations},
		Spec:     v1alpha1.AgentSpec{Title: "Ann"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, res.Version)

	obj, err := store.Get(ctx, testNS, "ann", "1")
	require.NoError(t, err)
	require.Equal(t, "clean", obj.Metadata.Annotations["security.agentregistry.solo.io/osv-status"])
	require.Equal(t, "builtin-seed", obj.Metadata.Annotations["internal.agentregistry.solo.io/import-source"])
}

// TestStore_AnnotationsPreservedOnReapply verifies the new apply-branch
// semantics: a re-apply with identical spec but no annotations in the
// incoming object replaces annotations to match what the caller sent.
// Annotations are user-managed, not server-managed, in the new world.
func TestStore_AnnotationsReplacedOnReapplyWithEmpty(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: "preserve", Annotations: map[string]string{"owner": "team-a"}},
		Spec:     v1alpha1.AgentSpec{Title: "P"},
	})
	require.NoError(t, err)

	// Re-apply with identical spec but no annotations in the body.
	// New semantic: labels/annotations come from the object, so this
	// clears the annotations to match what was sent.
	res, err := store.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: "preserve"},
		Spec:     v1alpha1.AgentSpec{Title: "P"},
	})
	require.NoError(t, err)
	require.Equal(t, UpsertLabelsUpdated, res.Outcome,
		"empty-annotations re-apply on identical spec must update labels/annotations in place")

	obj, err := store.Get(ctx, testNS, "preserve", "1")
	require.NoError(t, err)
	require.Empty(t, obj.Metadata.Annotations)
}

func TestStore_AnnotationsClearedOnEmptyMap(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: "clear", Annotations: map[string]string{"owner": "team-a"}},
		Spec:     v1alpha1.AgentSpec{Title: "C"},
	})
	require.NoError(t, err)

	// Re-apply with explicit empty map — annotations should clear.
	res, err := store.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: "clear", Annotations: map[string]string{}},
		Spec:     v1alpha1.AgentSpec{Title: "C"},
	})
	require.NoError(t, err)
	require.Equal(t, UpsertLabelsUpdated, res.Outcome)

	obj, err := store.Get(ctx, testNS, "clear", "1")
	require.NoError(t, err)
	require.Empty(t, obj.Metadata.Annotations)
}
