package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReconcileEmbeddingDimensions_MatchingDimensions(t *testing.T) {
	db := NewTestDB(t)
	pg, ok := db.(*PostgreSQL)
	if !ok {
		t.Skip("test requires PostgreSQL backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Default schema uses vector(1536). Reconciling with 1536 should be a no-op.
	err := pg.ReconcileEmbeddingDimensions(ctx, 1536)
	require.NoError(t, err)

	// Verify column is still 1536.
	var dim int
	err = pg.pool.QueryRow(ctx, `
		SELECT atttypmod
		FROM pg_attribute
		WHERE attrelid = 'servers'::regclass
		  AND attname = 'semantic_embedding'
		  AND NOT attisdropped
	`).Scan(&dim)
	require.NoError(t, err)
	require.Equal(t, 1536, dim)
}

func TestReconcileEmbeddingDimensions_ChangeDimensions(t *testing.T) {
	db := NewTestDB(t)
	pg, ok := db.(*PostgreSQL)
	if !ok {
		t.Skip("test requires PostgreSQL backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Change from default 1536 to 1024.
	err := pg.ReconcileEmbeddingDimensions(ctx, 1024)
	require.NoError(t, err)

	// Verify servers column is now 1024.
	var serversDim int
	err = pg.pool.QueryRow(ctx, `
		SELECT atttypmod
		FROM pg_attribute
		WHERE attrelid = 'servers'::regclass
		  AND attname = 'semantic_embedding'
		  AND NOT attisdropped
	`).Scan(&serversDim)
	require.NoError(t, err)
	require.Equal(t, 1024, serversDim)

	// Verify agents column is also 1024.
	var agentsDim int
	err = pg.pool.QueryRow(ctx, `
		SELECT atttypmod
		FROM pg_attribute
		WHERE attrelid = 'agents'::regclass
		  AND attname = 'semantic_embedding'
		  AND NOT attisdropped
	`).Scan(&agentsDim)
	require.NoError(t, err)
	require.Equal(t, 1024, agentsDim)

	// Verify HNSW indexes were recreated.
	for _, idx := range []string{
		"idx_servers_semantic_embedding_hnsw",
		"idx_agents_semantic_embedding_hnsw",
	} {
		var exists bool
		err = pg.pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE indexname = $1)", idx,
		).Scan(&exists)
		require.NoError(t, err)
		require.True(t, exists, "HNSW index %s should exist after reconciliation", idx)
	}
}

func TestReconcileEmbeddingDimensions_InvalidDimensions(t *testing.T) {
	db := NewTestDB(t)
	pg, ok := db.(*PostgreSQL)
	if !ok {
		t.Skip("test requires PostgreSQL backend")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := pg.ReconcileEmbeddingDimensions(ctx, 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be positive")

	err = pg.ReconcileEmbeddingDimensions(ctx, -1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be positive")
}
