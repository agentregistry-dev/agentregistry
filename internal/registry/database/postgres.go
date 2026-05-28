package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/legacymigrate"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/orchestrator"
)

// PostgreSQL is the root PostgreSQL-backed store. It owns the connection
// pool; per-kind v1alpha1 access happens via NewStores against
// db.Pool().
type PostgreSQL struct {
	pool  *pgxpool.Pool
	authz auth.Authorizer
}

// NewPostgreSQL opens a pool against connectionURI, runs the v1alpha1
// migrations against it (unless skipMigrations is true), and returns a
// *PostgreSQL ready for use by the generic v1alpha1 Store.
//
// skipMigrations short-circuits the startup migrator entirely. Used
// when migrations have been applied out-of-band by `arctl db migrate
// up`. The pool is still parsed, opened, and pinged so a
// misconfigured DB fails fast.
func NewPostgreSQL(ctx context.Context, connectionURI string, authz auth.Authorizer, skipMigrations bool) (*PostgreSQL, error) {
	config, err := pgxpool.ParseConfig(connectionURI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PostgreSQL config: %w", err)
	}

	// Stability-focused pool defaults.
	config.MaxConns = 30
	config.MinConns = 5
	config.MaxConnIdleTime = 30 * time.Minute
	config.MaxConnLifetime = 2 * time.Hour

	// Set search_path on every new connection so the v1alpha1 stores
	// can refer to tables unqualified — the schema is set by
	// migratepgx at migration-config time but app queries don't go
	// through migratepgx.
	schemaIdent := pgx.Identifier{database.OSSSchema}.Sanitize()
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schemaIdent)
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostgreSQL pool: %w", err)
	}

	if err = pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	if skipMigrations {
		// The gate can fire from AppOptions.SkipMigrations,
		// AGENT_REGISTRY_SKIP_MIGRATIONS, or the bare SKIP_MIGRATIONS
		// env — phrase neutrally so operators searching for any of
		// the three see the line.
		slog.Info("skipping startup migrations (SkipMigrations enabled) — schema must already be applied")
		return &PostgreSQL{pool: pool, authz: authz}, nil
	}

	if err := orchestrator.RunUp(ctx, connectionURI, []orchestrator.Source{legacymigrate.OSSSource()}); err != nil {
		return nil, fmt.Errorf("failed to run startup migrations: %w", err)
	}

	return &PostgreSQL{pool: pool, authz: authz}, nil
}

// Pool exposes the underlying pgxpool for callers that need direct
// pgx access.
func (db *PostgreSQL) Pool() *pgxpool.Pool {
	return db.pool
}

// Close releases the connection pool.
func (db *PostgreSQL) Close() error {
	db.pool.Close()
	return nil
}
