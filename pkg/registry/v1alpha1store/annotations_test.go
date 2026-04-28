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
	_, err := store.Upsert(ctx, testNS, "ann", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Title: "Ann"}), UpsertOpts{Annotations: annotations})
	require.NoError(t, err)

	obj, err := store.Get(ctx, testNS, "ann", "v1")
	require.NoError(t, err)
	require.Equal(t, "clean", obj.Metadata.Annotations["security.agentregistry.solo.io/osv-status"])
	require.Equal(t, "builtin-seed", obj.Metadata.Annotations["internal.agentregistry.solo.io/import-source"])
}

func TestStore_AnnotationsPreservedOnNilUpsert(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	// First apply with annotations.
	_, err := store.Upsert(ctx, testNS, "preserve", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Title: "P"}), UpsertOpts{Annotations: map[string]string{"owner": "team-a"}})
	require.NoError(t, err)

	// Re-apply with nil Annotations in opts (e.g. a controller that
	// only updates spec). Annotations should survive.
	_, err = store.Upsert(ctx, testNS, "preserve", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Title: "P"}), UpsertOpts{}) // Annotations nil
	require.NoError(t, err)

	obj, err := store.Get(ctx, testNS, "preserve", "v1")
	require.NoError(t, err)
	require.Equal(t, "team-a", obj.Metadata.Annotations["owner"])
}

func TestStore_AnnotationsClearedOnEmptyMap(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	// Apply with annotations.
	_, err := store.Upsert(ctx, testNS, "clear", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Title: "C"}), UpsertOpts{Annotations: map[string]string{"owner": "team-a"}})
	require.NoError(t, err)

	// Re-apply with explicit empty map — annotations should clear.
	_, err = store.Upsert(ctx, testNS, "clear", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Title: "C"}), UpsertOpts{Annotations: map[string]string{}})
	require.NoError(t, err)

	obj, err := store.Get(ctx, testNS, "clear", "v1")
	require.NoError(t, err)
	require.Empty(t, obj.Metadata.Annotations)
}
