// Package database wraps golang-migrate/migrate v4 with the patterns
// the OSS migrator and the arctl db migrate CLI need: a per-source
// MigrationsTable factory and a dirty-state auto-recovery wrapper.
package database

import (
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
func NewMigrator(dsn string, migrationsFS fs.FS, dir, table string) (*migrate.Migrate, error) {
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
// failure, rolls back to the pre-Up version so the database returns
// to a clean state rather than the go-migrate-default "dirty"
// schema_migrations row.
//
// Returns the pre-Up version so callers running multiple sources can
// snapshot it and roll prior sources back via RollbackToVersion if a
// later source fails (the cross-track atomicity model from kagent's
// runner).
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
			logger.Info("migration failed; no prior version to recover to")
		} else {
			logger.Info("migration failed, attempting rollback", "target_version", preVersion)
			if rbErr := RollbackToVersion(mg, name, preVersion); rbErr != nil {
				logger.Error("rollback failed", "error", rbErr)
			} else {
				logger.Info("rollback complete", "version", preVersion)
			}
		}
		return preVersion, fmt.Errorf("run migrations for %s: %w", name, upErr)
	}
	return preVersion, nil
}

// RollbackToVersion rolls mg back to targetVersion, clearing any
// dirty-state marker left over from a partial-failure Up. Used both
// for auto-recovery inside RunUpWithRecovery and by callers
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
			return nil
		}
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

