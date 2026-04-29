//go:build integration

package v1alpha1store

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/semantic"
)

// zeroPadVec returns a fixed-dimension vector with the first few positions
// set to the supplied values. Keeps fixtures short without violating the
// schema's fixed 1536 dimension.
func zeroPadVec(values ...float32) []float32 {
	v := make([]float32, 1536)
	copy(v, values)
	return v
}

func TestVectorLiteral(t *testing.T) {
	out, err := VectorLiteral([]float32{0.1, -0.25, 1, 0})
	require.NoError(t, err)
	require.Equal(t, "[0.1,-0.25,1,0]", out)

	_, err = VectorLiteral(nil)
	require.Error(t, err)
}

func TestStore_SetEmbedding_RoundTrip(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, testNS, "foo", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Title: "embed"}), UpsertOpts{})
	require.NoError(t, err)

	emb := semantic.SemanticEmbedding{
		Vector:     zeroPadVec(0.1, 0.2, 0.3),
		Provider:   "openai",
		Model:      "text-embedding-3-small",
		Dimensions: 1536,
		Checksum:   "sha256:abc",
	}
	require.NoError(t, store.SetEmbedding(ctx, testNS, "foo", "v1", emb))

	meta, err := store.GetEmbeddingMetadata(ctx, testNS, "foo", "v1")
	require.NoError(t, err)
	require.NotNil(t, meta)
	require.Equal(t, "openai", meta.Provider)
	require.Equal(t, "text-embedding-3-small", meta.Model)
	require.Equal(t, 1536, meta.Dimensions)
	require.Equal(t, "sha256:abc", meta.Checksum)
	require.False(t, meta.GeneratedAt.IsZero())
}

func TestStore_GetEmbeddingMetadata_NilWhenMissing(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, testNS, "foo", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Title: "x"}), UpsertOpts{})
	require.NoError(t, err)

	// Row exists but no embedding yet.
	meta, err := store.GetEmbeddingMetadata(ctx, testNS, "foo", "v1")
	require.NoError(t, err)
	require.Nil(t, meta)
}

func TestStore_GetEmbeddingMetadata_ErrNotFound(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.GetEmbeddingMetadata(ctx, testNS, "nope", "v1")
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
}

func TestStore_SetEmbedding_ErrNotFound(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	err := store.SetEmbedding(ctx, testNS, "nope", "v1", semantic.SemanticEmbedding{
		Vector: zeroPadVec(1),
	})
	require.True(t, errors.Is(err, pkgdb.ErrNotFound))
}

func TestStore_SemanticList_RanksByDistance(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	// Three agents at orthogonal-ish positions on the unit axes.
	mkAgent := func(name string, vec []float32) {
		_, err := store.Upsert(ctx, testNS, name, "v1",
			mustSpec(t, v1alpha1.AgentSpec{Title: name}), UpsertOpts{})
		require.NoError(t, err)
		require.NoError(t, store.SetEmbedding(ctx, testNS, name, "v1",
			semantic.SemanticEmbedding{
				Vector:     vec,
				Provider:   "test",
				Model:      "fake",
				Dimensions: 1536,
			}))
	}
	mkAgent("near", zeroPadVec(1, 0, 0))
	mkAgent("farther", zeroPadVec(0, 1, 0))
	mkAgent("farthest", zeroPadVec(-1, 0, 0))

	results, err := store.SemanticList(ctx, SemanticListOpts{
		Query:     zeroPadVec(1, 0, 0),
		Limit:     10,
		Namespace: testNS,
	})
	require.NoError(t, err)
	require.Len(t, results, 3)

	// Results ranked ascending by cosine distance.
	require.Equal(t, "near", results[0].Object.Metadata.Name)
	require.Equal(t, "farther", results[1].Object.Metadata.Name)
	require.Equal(t, "farthest", results[2].Object.Metadata.Name)

	require.InDelta(t, 0.0, results[0].Score, 1e-4)
	require.InDelta(t, 1.0, results[1].Score, 1e-4)
	require.InDelta(t, 2.0, results[2].Score, 1e-4)
}

func TestStore_SemanticList_ThresholdFilter(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	mkAgent := func(name string, vec []float32) {
		_, err := store.Upsert(ctx, testNS, name, "v1",
			mustSpec(t, v1alpha1.AgentSpec{Title: name}), UpsertOpts{})
		require.NoError(t, err)
		require.NoError(t, store.SetEmbedding(ctx, testNS, name, "v1",
			semantic.SemanticEmbedding{Vector: vec, Provider: "test", Dimensions: 1536}))
	}
	mkAgent("exact", zeroPadVec(1, 0, 0))
	mkAgent("orthogonal", zeroPadVec(0, 1, 0))

	results, err := store.SemanticList(ctx, SemanticListOpts{
		Query:     zeroPadVec(1, 0, 0),
		Threshold: 0.5, // drops the orthogonal (distance 1.0)
		Namespace: testNS,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "exact", results[0].Object.Metadata.Name)
}

func TestStore_SemanticList_SkipsRowsWithoutEmbedding(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	// Two rows — only one has an embedding.
	_, err := store.Upsert(ctx, testNS, "with-emb", "v1",
		mustSpec(t, v1alpha1.AgentSpec{}), UpsertOpts{})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, testNS, "no-emb", "v1",
		mustSpec(t, v1alpha1.AgentSpec{}), UpsertOpts{})
	require.NoError(t, err)
	require.NoError(t, store.SetEmbedding(ctx, testNS, "with-emb", "v1",
		semantic.SemanticEmbedding{Vector: zeroPadVec(1, 0, 0), Dimensions: 1536}))

	results, err := store.SemanticList(ctx, SemanticListOpts{
		Query:     zeroPadVec(1, 0, 0),
		Namespace: testNS,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "with-emb", results[0].Object.Metadata.Name)
}

func TestStore_SemanticList_LatestOnly(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for _, v := range []string{"v1", "v2"} {
		_, err := store.Upsert(ctx, testNS, "foo", v,
			mustSpec(t, v1alpha1.AgentSpec{Title: v}), UpsertOpts{})
		require.NoError(t, err)
		require.NoError(t, store.SetEmbedding(ctx, testNS, "foo", v,
			semantic.SemanticEmbedding{Vector: zeroPadVec(1, 0, 0), Dimensions: 1536}))
	}

	// Both versions return by default.
	all, err := store.SemanticList(ctx, SemanticListOpts{
		Query:     zeroPadVec(1, 0, 0),
		Namespace: testNS,
	})
	require.NoError(t, err)
	require.Len(t, all, 2)

	// LatestOnly collapses to the semver winner (v2).
	latest, err := store.SemanticList(ctx, SemanticListOpts{
		Query:      zeroPadVec(1, 0, 0),
		Namespace:  testNS,
		LatestOnly: true,
	})
	require.NoError(t, err)
	require.Len(t, latest, 1)
	require.Equal(t, "v2", latest[0].Object.Metadata.Version)
}
