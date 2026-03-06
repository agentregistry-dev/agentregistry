package database

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ReconcileEmbeddingDimensions ensures the vector column dimensions in the
// database match the configured EMBEDDINGS_DIMENSIONS value. When a mismatch
// is detected it drops the HNSW indexes, clears stale embeddings, alters the
// column type, and recreates the indexes inside a single transaction.
//
// This is safe because:
//   - Existing deployments with matching dimensions are a no-op.
//   - Clearing embeddings on dimension change is correct (old vectors are
//     incompatible with the new dimension).
//   - The reconciliation runs once on startup, not per-request.
// ReconcileEmbeddingDimensions reconciles embedding column dimensions using
// the database's connection pool.
func (db *PostgreSQL) ReconcileEmbeddingDimensions(ctx context.Context, dimensions int) error {
	return reconcileEmbeddingDimensions(ctx, db.pool, dimensions)
}

func reconcileEmbeddingDimensions(ctx context.Context, pool *pgxpool.Pool, dimensions int) error {
	if dimensions <= 0 {
		return fmt.Errorf("reconcile embeddings: dimensions must be positive (got %d)", dimensions)
	}

	// Check current vector column dimension from the servers table.
	var currentDim int
	err := pool.QueryRow(ctx, `
		SELECT atttypmod
		FROM pg_attribute
		WHERE attrelid = 'servers'::regclass
		  AND attname = 'semantic_embedding'
		  AND NOT attisdropped
	`).Scan(&currentDim)
	if err != nil {
		// If the column doesn't exist yet (e.g., embeddings never enabled),
		// skip reconciliation — migrations will handle creation.
		slog.Info("skipping embedding dimension reconciliation: could not read column info", "error", err)
		return nil
	}

	if currentDim == dimensions {
		slog.Info("embedding dimensions match database schema", "dimensions", dimensions)
		return nil
	}

	slog.Warn("embedding dimension mismatch detected, reconciling",
		"current", currentDim,
		"configured", dimensions,
	)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("reconcile embeddings: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Drop HNSW indexes (must be dropped before altering column type).
	for _, idx := range []string{
		"idx_servers_semantic_embedding_hnsw",
		"idx_agents_semantic_embedding_hnsw",
	} {
		if _, err := tx.Exec(ctx, fmt.Sprintf("DROP INDEX IF EXISTS %s", idx)); err != nil {
			return fmt.Errorf("reconcile embeddings: drop index %s: %w", idx, err)
		}
	}

	// Clear stale embeddings — old vectors are incompatible with new dimensions.
	for _, table := range []string{"servers", "agents"} {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
			UPDATE %s SET
				semantic_embedding = NULL,
				semantic_embedding_provider = NULL,
				semantic_embedding_model = NULL,
				semantic_embedding_dimensions = NULL,
				semantic_embedding_checksum = NULL,
				semantic_embedding_generated_at = NULL
		`, table)); err != nil {
			return fmt.Errorf("reconcile embeddings: clear %s embeddings: %w", table, err)
		}
	}

	// Alter column types to the new dimension.
	for _, table := range []string{"servers", "agents"} {
		sql := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN semantic_embedding TYPE vector(%d)", table, dimensions)
		if _, err := tx.Exec(ctx, sql); err != nil {
			return fmt.Errorf("reconcile embeddings: alter %s column: %w", table, err)
		}
	}

	// Recreate HNSW indexes with the new dimension.
	for _, spec := range []struct{ table, index string }{
		{"servers", "idx_servers_semantic_embedding_hnsw"},
		{"agents", "idx_agents_semantic_embedding_hnsw"},
	} {
		sql := fmt.Sprintf(
			"CREATE INDEX %s ON %s USING hnsw (semantic_embedding vector_cosine_ops)",
			spec.index, spec.table,
		)
		if _, err := tx.Exec(ctx, sql); err != nil {
			return fmt.Errorf("reconcile embeddings: create index %s: %w", spec.index, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reconcile embeddings: commit: %w", err)
	}

	slog.Info("embedding dimensions reconciled successfully",
		"old", currentDim,
		"new", dimensions,
	)
	return nil
}
