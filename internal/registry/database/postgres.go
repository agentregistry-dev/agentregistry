package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// PostgreSQL is the root PostgreSQL-backed store. It owns the connection
// pool; per-kind v1alpha1 access happens via NewStores against
// db.Pool().
type PostgreSQL struct {
	pool  *pgxpool.Pool
	authz auth.Authorizer
}

// NewPostgreSQL opens a pool against connectionURI, runs the v1alpha1
// migrations against it, and returns a *PostgreSQL ready for use by the
// generic v1alpha1 Store.
//
// embeddingsEnabled gates the pgvector migration. When false, the
// 003_embeddings.sql migration is skipped so a vanilla PostgreSQL
// install (no pgvector extension) succeeds — semantic search /
// indexing remains unavailable until the flag flips on, at which
// point the migration applies on next boot.
func NewPostgreSQL(ctx context.Context, connectionURI string, authz auth.Authorizer, embeddingsEnabled bool) (*PostgreSQL, error) {
	config, err := pgxpool.ParseConfig(connectionURI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PostgreSQL config: %w", err)
	}

	// Stability-focused pool defaults.
	config.MaxConns = 30
	config.MinConns = 5
	config.MaxConnIdleTime = 30 * time.Minute
	config.MaxConnLifetime = 2 * time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostgreSQL pool: %w", err)
	}

	if err = pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection for migrations: %w", err)
	}
	defer conn.Release()

	v1alpha1Migrator := database.NewMigrator(conn.Conn(), v1alpha1store.MigratorConfig(embeddingsEnabled))
	if err := v1alpha1Migrator.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("failed to run v1alpha1 migrations: %w", err)
	}

	return &PostgreSQL{pool: pool, authz: authz}, nil
}

// Pool exposes the underlying pgxpool for callers (v1alpha1 Stores,
// importer findings store, enterprise extensions) that need direct
// pgx access.
func (db *PostgreSQL) Pool() *pgxpool.Pool {
	return db.pool
}

// Close releases the connection pool.
func (db *PostgreSQL) Close() error {
	db.pool.Close()
	return nil
}
