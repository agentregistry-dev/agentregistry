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

	upsertAgent(t, store, "foo", v1alpha1.AgentSpec{Title: "embed"}, nil)

	emb := semantic.SemanticEmbedding{
		Vector:     zeroPadVec(0.1, 0.2, 0.3),
		Provider:   "openai",
		Model:      "text-embedding-3-small",
		Dimensions: 1536,
		Checksum:   "sha256:abc",
	}
	require.NoError(t, store.SetEmbedding(ctx, testNS, "foo", "1", emb))

	meta, err := store.GetEmbeddingMetadata(ctx, testNS, "foo", "1")
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

	upsertAgent(t, store, "foo", v1alpha1.AgentSpec{Title: "x"}, nil)

	// Row exists but no embedding yet.
	meta, err := store.GetEmbeddingMetadata(ctx, testNS, "foo", "1")
	require.NoError(t, err)
	require.Nil(t, meta)
}

func TestStore_GetEmbeddingMetadata_ErrNotFound(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.GetEmbeddingMetadata(ctx, testNS, "nope", "1")
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
}

func TestStore_SetEmbedding_ErrNotFound(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	err := store.SetEmbedding(ctx, testNS, "nope", "1", semantic.SemanticEmbedding{
		Vector: zeroPadVec(1),
	})
	require.True(t, errors.Is(err, pkgdb.ErrNotFound))
}

func TestStore_SemanticList_RanksByDistance(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	mkAgent := func(name string, vec []float32) {
		upsertAgent(t, store, name, v1alpha1.AgentSpec{Title: name}, nil)
		require.NoError(t, store.SetEmbedding(ctx, testNS, name, "1",
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
		upsertAgent(t, store, name, v1alpha1.AgentSpec{Title: name}, nil)
		require.NoError(t, store.SetEmbedding(ctx, testNS, name, "1",
			semantic.SemanticEmbedding{Vector: vec, Provider: "test", Dimensions: 1536}))
	}
	mkAgent("exact", zeroPadVec(1, 0, 0))
	mkAgent("orthogonal", zeroPadVec(0, 1, 0))

	results, err := store.SemanticList(ctx, SemanticListOpts{
		Query:     zeroPadVec(1, 0, 0),
		Threshold: 0.5,
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
	upsertAgent(t, store, "with-emb", v1alpha1.AgentSpec{}, nil)
	upsertAgent(t, store, "no-emb", v1alpha1.AgentSpec{}, nil)
	require.NoError(t, store.SetEmbedding(ctx, testNS, "with-emb", "1",
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

	// Two versions of the same agent.
	upsertAgent(t, store, "foo", v1alpha1.AgentSpec{Title: "first"}, nil)
	upsertAgent(t, store, "foo", v1alpha1.AgentSpec{Title: "second"}, nil)

	for _, v := range []string{"1", "2"} {
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

	// LatestOnly collapses to MAX(version) — version=2.
	latest, err := store.SemanticList(ctx, SemanticListOpts{
		Query:      zeroPadVec(1, 0, 0),
		Namespace:  testNS,
		LatestOnly: true,
	})
	require.NoError(t, err)
	require.Len(t, latest, 1)
	require.Equal(t, "2", latest[0].Object.Metadata.Version)
}
