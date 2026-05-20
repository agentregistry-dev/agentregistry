// Package database wraps golang-migrate/migrate v4 with the patterns
// the OSS migrator and the arctl db migrate CLI need: a per-source
// MigrationsTable factory and a dirty-state auto-recovery wrapper.
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

// NewMigrator constructs a *migrate.Migrate for the given source.
// Each registered source owns its own MigrationsTable (passed as table)
// so two sources can coexist in the same database without colliding.
//
// The caller owns mg.Close() — it tears down both the iofs source and
// the underlying *sql.DB. A single dedicated connection (not a pool)
// is used because go-migrate's advisory lock is session-level and must
// not be shared.
//
// NewMigrator takes a context for API symmetry with the surrounding
// startup code, but ctx is unused inside the function — sql.Open is
// lazy (never pings), and go-migrate's API is synchronous and doesn't
// accept a context. The legacy bootstrap is the only ctx-consuming
// hop in the OSS factory; see NewOSSMigrator.
func NewMigrator(_ context.Context, dsn string, migrationsFS fs.FS, dir, table string) (*migrate.Migrate, error) {
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
		MigrationsTable: table,
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

	if upErr := mg.Up(); upErr != nil {
		if errors.Is(upErr, migrate.ErrNoChange) {
			return preVersion, nil
		}
		if preVersion == 0 {
			// No prior version means there's nothing to recover to;
			// the dirty row points at the migration that failed and an
			// operator-facing message is the most actionable surface.
			logger.Info("migration failed; no prior version to recover to — inspect schema for partial DDL before retry")
		} else {
			logger.Info(fmt.Sprintf("migration failed, clearing dirty bookkeeping back to v%d", preVersion), "target_version", preVersion)
			if rbErr := RollbackToVersion(mg, name, preVersion); rbErr != nil {
				logger.Error("dirty-bookkeeping recovery failed", "error", rbErr)
			} else {
				logger.Info(fmt.Sprintf("dirty bookkeeping cleared back to v%d; partial DDL from the failed migration may remain — inspect schema before retry", preVersion), "version", preVersion)
			}
		}
		return preVersion, fmt.Errorf("run migrations for %s: %w", name, upErr)
	}
	return preVersion, nil
}

// RollbackToVersion clears go-migrate's dirty-state marker on mg and,
// when the source has reversible `.down.sql` files, steps the schema
// back to targetVersion via the standard `mg.Steps(-N)` path.
//
// Important behavioral note: when called from auto-recovery after a
// failed Up at version current=preVersion+1, `Force(current-1)` clears
// the dirty flag but `steps = (current-1) - preVersion = 0` so no
// `.down.sql` is invoked. The function returns nil ("nothing to roll
// back from the bookkeeping perspective") — it does NOT undo DDL that
// the failed migration committed before erroring out. Callers that
// expected full atomicity must rely on the migration's own
// idempotency for safe retry.
//
// Used both for auto-recovery inside RunUpWithRecovery and by callers
// coordinating cross-source rollback after a later source fails.
func RollbackToVersion(mg *migrate.Migrate, name string, targetVersion uint) error {
	currentVersion, dirty, err := mg.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return nil
		}
		return fmt.Errorf("get version after failure for %s: %w", name, err)
	}

	if dirty {
		// go-migrate flags a row dirty when its Up failed partway.
		// Force to current-1 so subsequent Steps(-N) can run; if the
		// very first migration failed, Force(-1) removes the row
		// entirely.
		cleanVersion := int(currentVersion) - 1
		forceTarget := cleanVersion
		if forceTarget < 1 {
			forceTarget = -1
		}
		if err := mg.Force(forceTarget); err != nil {
			return fmt.Errorf("clear dirty state for %s: %w", name, err)
		}
		if forceTarget < 0 {
			// First migration failed; the row is gone, nothing
			// further to step back through.
			return nil
		}
		// forceTarget >= 1 here, and forceTarget == cleanVersion on
		// this path (we only rewrote to -1 inside the < 1 branch), so
		// the signed→unsigned cast is well-defined.
		currentVersion = uint(cleanVersion)
	}

	steps := int(currentVersion) - int(targetVersion)
	if steps <= 0 {
		return nil
	}
	if err := mg.Steps(-steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("roll back %d step(s) for %s: %w", steps, name, err)
	}
	return nil
}

