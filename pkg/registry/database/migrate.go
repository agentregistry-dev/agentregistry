// Package database wraps golang-migrate/migrate v4 with the patterns
// the orchestrator and the arctl db migrate CLI need: a
// schema-parameterized *migrate.Migrate factory and a dirty-state
// auto-recovery wrapper.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
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
func NewMigrator(_ context.Context, dsn string, migrationsFS fs.FS, dir, schema string) (*migrate.Migrate, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
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

// RunUpWithRecovery applies all pending migrations for mg and, on
// failure, clears go-migrate's "dirty" schema_migrations bookkeeping
// by Force-ing back to the pre-Up version. This is BOOKKEEPING
// recovery only — it does NOT undo any DDL that may have committed
// before the migration failed mid-statement. The operator-actionable
// guarantee is that subsequent `Up` calls won't reject with
// "Dirty database version N. Fix and force version." — they'll
// re-attempt the failed migration, which our idempotent-DDL
// convention (CREATE ... IF NOT EXISTS, CREATE OR REPLACE, DROP ...
// IF EXISTS) makes safe.
//
// Returns the pre-Up version so callers running multiple sources can
// snapshot it and pass it to RollbackToVersion if a later source's
// Up fails. Cross-source rollback is best-effort: it succeeds when
// each prior source's `.down.sql` files are reversible; sources with
// up-only migrations (raise-exception downs) will fail to roll back
// and the caller is expected to surface the partial state.
//
// name appears in log lines and is used purely for operator-facing
// diagnostics — it has no semantic effect on the migrator.
func RunUpWithRecovery(mg *migrate.Migrate, name string) (preVersion uint, err error) {
	logger := slog.Default().With("component", "database.migrate", "source", name)

	preVersion, _, verr := mg.Version()
	if verr != nil && !errors.Is(verr, migrate.ErrNilVersion) {
		return 0, fmt.Errorf("get pre-migration version for %s: %w", name, verr)
	}

	upErr := mg.Up()
	if upErr == nil || errors.Is(upErr, migrate.ErrNoChange) {
		return preVersion, nil
	}
	recoverFromFailedUp(logger, mg, name, preVersion)
	return preVersion, fmt.Errorf("run migrations for %s: %w", name, upErr)
}

// recoverFromFailedUp clears go-migrate's dirty bookkeeping back to
// preVersion when there's a prior version to recover to; otherwise it
// logs an actionable note that the first migration left no row to
// recover from. Schema-level cleanup is not attempted — see
// RollbackToVersion's docstring.
func recoverFromFailedUp(logger *slog.Logger, mg *migrate.Migrate, name string, preVersion uint) {
	if preVersion == 0 {
		// No prior version means there's nothing to recover to; the
		// dirty row points at the migration that failed and an
		// operator-facing message is the most actionable surface.
		logger.Info("migration failed; no prior version to recover to — inspect schema for partial DDL before retry")
		return
	}
	logger.Info(fmt.Sprintf("migration failed, clearing dirty bookkeeping back to v%d", preVersion), "target_version", preVersion)
	if rbErr := RollbackToVersion(mg, name, preVersion); rbErr != nil {
		logger.Error("dirty-bookkeeping recovery failed", "error", rbErr)
		return
	}
	logger.Info(fmt.Sprintf("dirty bookkeeping cleared back to v%d; partial DDL from the failed migration may remain — inspect schema before retry", preVersion), "version", preVersion)
}

// RollbackToVersion returns mg's schema_migrations row to targetVersion.
// The function is rollback-only: callers must pass a targetVersion at
// or below the current applied version. When targetVersion >= current,
// it returns nil without touching the schema (the name promises
// rollback semantics, and mg.Migrate would happily walk forward).
//
// Two paths:
//
//   - Dirty: clear the dirty marker by forcing to targetVersion. This
//     is BOOKKEEPING ONLY — DDL committed by the partially-applied
//     migration may remain. The idempotent-DDL convention is what
//     makes a subsequent Up safe to retry.
//   - Clean: walk down to targetVersion via mg.Migrate / mg.Down,
//     which go-migrate routes through the source's known-version list
//     (gap-safe; runs the right .down.sql files in order).
//
// targetVersion == 0 means "empty schema" — mg.Down rolls back every
// applied migration in the source.
//
// Used by auto-recovery in RunUpWithRecovery and by cross-source
// coordination after a later source fails. Up-only sources (whose
// .down.sql files raise) succeed on the dirty path and fail on the
// clean path; the caller is expected to surface partial state.
func RollbackToVersion(mg *migrate.Migrate, name string, targetVersion uint) error {
	currentVersion, dirty, err := mg.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return nil
		}
		return fmt.Errorf("get version for %s: %w", name, err)
	}

	if dirty {
		forceTarget := int(targetVersion)
		if forceTarget == 0 {
			// go-migrate represents "no version applied" by removing
			// the row; Force(-1) is the supported signal for that.
			forceTarget = -1
		}
		if err := mg.Force(forceTarget); err != nil {
			return fmt.Errorf("clear dirty state for %s: %w", name, err)
		}
		return nil
	}

	if targetVersion >= currentVersion {
		// Already at or below target; the rollback-only contract makes
		// this a no-op rather than forwarding the schema via Migrate.
		return nil
	}
	if targetVersion == 0 {
		if err := mg.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("roll back to empty for %s: %w", name, err)
		}
		return nil
	}
	if err := mg.Migrate(targetVersion); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("roll back to v%d for %s: %w", targetVersion, name, err)
	}
	return nil
}
