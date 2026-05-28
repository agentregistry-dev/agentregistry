// Package database wraps golang-migrate/migrate v4 with the
// schema-parameterized *migrate.Migrate factory the orchestrator and
// the arctl db migrate CLI consume. Dirty-state recovery is not
// attempted here — the orchestrator's idempotent-DDL convention and
// advisory lock are the production contract.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver — required by sql.Open("pgx", ...)
)

// OSSSchema is the Postgres schema name OSS tables live in. The
// `golang-migrate` pgx/v5 driver is configured with `SchemaName: OSSSchema`,
// so `schema_migrations` and every data table land in this schema.
//
// Exposed as a constant for now; future work makes it env-driven so
// operators can point the binary at an operator-configured schema.
const OSSSchema = "agentregistry"

// migrationsTable is the table name `golang-migrate` uses for its own
// bookkeeping. Lives in the source's `SchemaName`, so two sources with
// different `SchemaName` values share the table name without colliding.
const migrationsTable = "schema_migrations"

// NewMigrator constructs a *migrate.Migrate against `schema` for the
// embedded migration set at `migrationsFS`/`dir`. The migrator's
// `schema_migrations` bookkeeping table is created in `schema` (via
// `migratepgx.Config{SchemaName: schema}`).
//
// The caller owns `mg.Close()` — it tears down both the iofs source
// and the underlying *sql.DB. A single dedicated connection (not a
// pool) is used because go-migrate's advisory lock is session-level
// and must not be shared.
//
// `ctx` is accepted for API symmetry with the surrounding startup
// code; sql.Open is lazy (never pings) and go-migrate's API is
// synchronous and doesn't accept a context.
func NewMigrator(ctx context.Context, dsn string, migrationsFS fs.FS, dir, schema string) (*migrate.Migrate, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// migratepgx.WithInstance creates schema_migrations in `schema`
	// during construction, which fails on a fresh DB where the schema
	// doesn't yet exist. CREATE SCHEMA IF NOT EXISTS makes the factory
	// safe to call on both fresh and existing DBs.
	if _, err := db.ExecContext(ctx,
		"CREATE SCHEMA IF NOT EXISTS "+pgx.Identifier{schema}.Sanitize()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create schema %s: %w", schema, err)
	}

	src, err := iofs.New(migrationsFS, dir)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("load migration files from %s: %w", dir, err)
	}

	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{
		SchemaName:      schema,
		MigrationsTable: migrationsTable,
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("create migration driver: %w", err)
	}

	mg, err := migrate.NewWithInstance("iofs", src, "pgx", driver)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("construct migrator: %w", err)
	}
	return mg, nil
}
