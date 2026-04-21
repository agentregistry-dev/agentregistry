package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// PostgreSQL is the root PostgreSQL-backed store. It owns the connection
// pool; per-kind v1alpha1 access happens via NewV1Alpha1Stores against
// db.Pool().
type PostgreSQL struct {
	pool  *pgxpool.Pool
	authz auth.Authorizer
}

// NewPostgreSQL opens a pool against connectionURI, runs the v1alpha1
// migrations against it, and returns a *PostgreSQL ready for use by the
// generic v1alpha1 Store. The legacy public.* tables are no longer
// migrated — every domain the server speaks in production lives under
// the v1alpha1 schema.
func NewPostgreSQL(ctx context.Context, connectionURI string, authz auth.Authorizer, _ bool) (*PostgreSQL, error) {
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

	v1alpha1Migrator := database.NewMigrator(conn.Conn(), V1Alpha1MigratorConfig())
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
