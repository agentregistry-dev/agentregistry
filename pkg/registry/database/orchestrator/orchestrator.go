// Package orchestrator drives `golang-migrate/migrate/v4` Up against
// one or more registered Sources behind a single advisory-lock-guarded
// startup path. Each Source owns its own Postgres schema and may
// supply a `LegacyRun` callback that runs once between the first
// migration (`mg.Steps(1)`) and the rest (`mg.Up()`).
//
// The orchestrator is the merge gate's contract: server startup
// (`internal/registry/database/postgres.go`) and the CLI's `arctl db
// migrate up` both invoke `RunUp` so the same legacy-bridging logic
// fires on every up path.
package orchestrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// Source describes one set of migrations to be applied as part of the
// orchestrator's startup sequence.
type Source struct {
	// Name is the operator-visible label and the advisory-lock key
	// derivation input. Must be a valid SQL identifier
	// (`^[a-z][a-z0-9_]*$`).
	Name string

	// Schema is the Postgres schema this source's tables live in
	// (e.g. "agentregistry"). `golang-migrate`'s pgx/v5 driver is
	// configured with `SchemaName: Schema`; the source's
	// `schema_migrations` table is created in that schema.
	Schema string

	// Files is the embedded filesystem holding NNN_name.up.sql /
	// NNN_name.down.sql pairs.
	Files fs.FS

	// Dir is the directory inside Files containing the migration
	// pairs.
	Dir string

	// LegacyRun, when non-nil, is invoked between `mg.Steps(1)` and
	// `mg.Up()` — but only when `public.schema_migrations` exists AND
	// the source's `schema_migrations` table was empty before
	// `Steps(1)`. Fresh installs and re-runs both skip cleanly.
	LegacyRun func(ctx context.Context, db *sql.DB) error
}

// RunUp opens a dedicated single-connection database handle per Source,
// acquires a per-source `pg_advisory_lock`, applies `Steps(1)`,
// invokes the legacy bridge if applicable, then applies `Up()`. After
// every Source succeeds, `public.schema_migrations` (the prior custom
// migrator's bookkeeping table, if it survives) is renamed to
// `public.schema_migrations_v0_legacy`.
//
// Concurrent invocations against the same database serialize through
// the advisory lock; the loser re-probes inside the lock and falls
// through to a no-op.
func RunUp(ctx context.Context, dsn string, sources []Source) error {
	for _, src := range sources {
		if err := runSource(ctx, dsn, src); err != nil {
			return fmt.Errorf("run source %s: %w", src.Name, err)
		}
	}
	return renameLegacyOnce(ctx, dsn)
}

// runSource executes the per-Source sequence:
//
//  1. Open `*sql.DB` with `MaxOpenConns = 1`.
//  2. `pg_advisory_lock(<hash(src.Name)>)` (session-level; released on
//     close).
//  3. Snapshot the row count of `<src.Schema>.schema_migrations`
//     (zero if the table doesn't yet exist).
//  4. Build `*migrate.Migrate` against `src.Schema`.
//  5. `mg.Steps(1)`.
//  6. If `src.LegacyRun != nil` AND `public.schema_migrations` exists
//     AND the pre-Steps row count was zero, invoke `src.LegacyRun`.
//  7. `mg.Up()`.
//  8. Close mg + db (releases advisory lock).
func runSource(ctx context.Context, dsn string, src Source) error {
	logger := slog.Default().With("component", "database.orchestrator", "source", src.Name)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()

	lockKey := advisoryLockKey(src.Name)
	if _, err := db.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}
	defer func() {
		if _, err := db.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
			logger.Warn("release advisory lock", "error", err)
		}
	}()

	preStepsCount, err := schemaMigrationsRowCount(ctx, db, src.Schema)
	if err != nil {
		return fmt.Errorf("snapshot schema_migrations row count: %w", err)
	}

	mg, err := database.NewMigrator(ctx, dsn, src.Files, src.Dir, src.Schema)
	if err != nil {
		return fmt.Errorf("construct migrator: %w", err)
	}
	defer func() {
		if srcErr, dbErr := mg.Close(); srcErr != nil || dbErr != nil {
			logger.Warn("close migrator", "source_error", srcErr, "database_error", dbErr)
		}
	}()

	// Steps(1) only fires on first apply (preStepsCount == 0). On
	// re-runs we already have a row and Steps(1) would either
	// advance to a non-existent v2 or fail; in both cases the work
	// it represents is already done.
	if preStepsCount == 0 {
		if err := mg.Steps(1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("apply first migration: %w", err)
		}
	}

	if src.LegacyRun != nil && preStepsCount == 0 {
		legacyExists, err := publicSchemaMigrationsExists(ctx, db)
		if err != nil {
			return fmt.Errorf("probe public.schema_migrations: %w", err)
		}
		if legacyExists {
			logger.Info("running legacy data bridge")
			if err := src.LegacyRun(ctx, db); err != nil {
				return fmt.Errorf("legacy bridge: %w", err)
			}
		}
	}

	if err := mg.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply remaining migrations: %w", err)
	}
	return nil
}

// renameLegacyOnce renames `public.schema_migrations` to
// `public.schema_migrations_v0_legacy` if the legacy table is still
// present. `ALTER TABLE IF EXISTS ... RENAME TO` is idempotent and
// Postgres serializes concurrent ALTER TABLE under a table-level lock,
// so multiple racing orchestrator invocations end with the same state.
func renameLegacyOnce(ctx context.Context, dsn string) error {
	logger := slog.Default().With("component", "database.orchestrator")
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database for legacy rename: %w", err)
	}
	defer func() { _ = db.Close() }()

	exists, err := publicSchemaMigrationsExists(ctx, db)
	if err != nil {
		return fmt.Errorf("probe public.schema_migrations: %w", err)
	}
	if !exists {
		return nil
	}
	if _, err := db.ExecContext(ctx,
		`ALTER TABLE IF EXISTS public.schema_migrations RENAME TO schema_migrations_v0_legacy`); err != nil {
		return fmt.Errorf("rename public.schema_migrations: %w", err)
	}
	logger.Info("renamed public.schema_migrations to public.schema_migrations_v0_legacy; legacy data tables in v1alpha1.* will be dropped in a future release")
	return nil
}

// schemaMigrationsRowCount returns the count of rows in
// `<schema>.schema_migrations`, or 0 if the table doesn't exist.
func schemaMigrationsRowCount(ctx context.Context, db *sql.DB, schema string) (int, error) {
	var oid sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT to_regclass($1)::text", schema+".schema_migrations").Scan(&oid); err != nil {
		return 0, fmt.Errorf("regclass probe: %w", err)
	}
	if !oid.Valid {
		return 0, nil
	}
	var count int
	q := fmt.Sprintf("SELECT count(*) FROM %s.schema_migrations", schema)
	if err := db.QueryRowContext(ctx, q).Scan(&count); err != nil {
		return 0, fmt.Errorf("count schema_migrations rows: %w", err)
	}
	return count, nil
}

// publicSchemaMigrationsExists reports whether the legacy
// `public.schema_migrations` table is present. The orchestrator gates
// `LegacyRun` and the post-loop rename on this signal.
func publicSchemaMigrationsExists(ctx context.Context, db *sql.DB) (bool, error) {
	var oid sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT to_regclass('public.schema_migrations')::text").Scan(&oid); err != nil {
		return false, err
	}
	return oid.Valid, nil
}

// advisoryLockKey derives a stable 63-bit int from the source name so
// concurrent pods serializing on the same source share a lock without
// hardcoding a global registry.
func advisoryLockKey(name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return int64(h.Sum64() & 0x7fffffffffffffff)
}
