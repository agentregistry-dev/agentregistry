package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver — required by sql.Open("pgx", ...)
)

// bootstrapAdvisoryLockKey is the pg_advisory_xact_lock key under
// which the OSS legacy-bootstrap serializes. Computed once via a
// stable fnv32 over the descriptive name so the value is reproducible
// across processes without baking a magic int.
const bootstrapAdvisoryLockKey = int64(0x6172626f6f742d6f) // "arboot-o" — first 8 bytes ASCII, fits int64

// BootstrapLegacyOSSMigrations bridges OSS deployments that ran the
// pre-engine-swap custom migrator (`schema_migrations` with columns
// version/name/applied_at and a +200 version offset) to the
// golang-migrate shape (`schema_migrations` with version/dirty, where
// a single row records the highest applied version).
//
// On fresh installs and on deployments already bridged the function
// no-ops. On a legacy DB it runs once in a single transaction:
//
//   1. Renames the legacy table to schema_migrations_v0_legacy
//      (audit trail preserved).
//   2. Creates the new go-migrate-shaped schema_migrations table.
//   3. Inserts a single row whose version is the highest legacy OSS
//      version that (a) sits in [201, 499] AND (b) has a matching
//      NNN_*.up.sql in migrationsFS/dir, with the +200 offset
//      stripped. Orphan legacy rows (no matching .up.sql) are
//      skipped — the legacy table preserves them for forensics.
//
// Rows with `version >= 500` are intentionally left untouched in
// schema_migrations_v0_legacy for downstream extension bootstraps to
// claim — the independent-tracks split is layered, not bundled.
//
// The function is safe to call from multiple processes concurrently
// (rolling deploys, parallel `arctl db migrate up` + server startup):
// the bootstrap tx takes `pg_advisory_xact_lock` so racers serialize,
// and re-probes the legacy shape inside the lock so the loser sees
// the bridged state and no-ops cleanly rather than colliding on the
// `_v0_legacy` rename.
func BootstrapLegacyOSSMigrations(ctx context.Context, dsn string, migrationsFS fs.FS, dir string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database for bootstrap: %w", err)
	}
	defer func() { _ = db.Close() }()

	// Cheap pre-flight outside the lock: on fresh installs and
	// already-bridged DBs (the common steady state) we skip the
	// lock acquisition entirely.
	hasName, err := probeLegacyShape(ctx, db)
	if err != nil {
		return err
	}
	if !hasName {
		return nil
	}

	valid, err := loadValidUpVersions(migrationsFS, dir)
	if err != nil {
		return fmt.Errorf("scan migration sources: %w", err)
	}

	// Pre-fetch legacy OSS versions into memory before opening the
	// bootstrap tx; pgx's stdlib bridge doesn't allow interleaved
	// SELECT cursors and DML on the same tx-bound connection.
	legacyVersions, err := readLegacyOSSVersions(ctx, db)
	if err != nil {
		return err
	}

	maxValid, dropped := highestCarryable(legacyVersions, valid)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bootstrap tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize concurrent bootstraps. pg_advisory_xact_lock releases
	// automatically on COMMIT / ROLLBACK so a panic or context-
	// cancellation can't leak the lock.
	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock($1)`, bootstrapAdvisoryLockKey); err != nil {
		return fmt.Errorf("acquire bootstrap advisory lock: %w", err)
	}

	// Re-probe inside the lock. If a concurrent caller bridged while
	// we were waiting on the lock, the `name` column is gone — bail
	// out cleanly instead of colliding on the RENAME.
	hasName, err = probeLegacyShapeTx(ctx, tx)
	if err != nil {
		return err
	}
	if !hasName {
		return nil
	}

	if _, err := tx.ExecContext(ctx,
		`ALTER TABLE schema_migrations RENAME TO schema_migrations_v0_legacy`); err != nil {
		return fmt.Errorf("rename legacy schema_migrations: %w", err)
	}

	// Match go-migrate pgx/v5 driver's CREATE TABLE shape exactly so
	// it accepts the pre-created table without complaint.
	if _, err := tx.ExecContext(ctx,
		`CREATE TABLE schema_migrations (version BIGINT NOT NULL PRIMARY KEY, dirty BOOLEAN NOT NULL)`); err != nil {
		return fmt.Errorf("create new schema_migrations: %w", err)
	}

	if maxValid > 0 {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, dirty) VALUES ($1, false)`,
			maxValid); err != nil {
			return fmt.Errorf("insert bridged row v%d: %w", maxValid, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bootstrap tx: %w", err)
	}

	logger := slog.Default().With("component", "database.bootstrap")
	logger.Info("bridged legacy schema_migrations to go-migrate shape",
		"highest_carried", maxValid,
		"dropped", len(dropped))
	if len(dropped) > 0 {
		logger.Info("orphan legacy rows skipped during bridge (no matching .up.sql in current embed)",
			"versions", formatDroppedVersions(dropped))
	}
	return nil
}

// probeLegacyShape returns true when `public.schema_migrations` has
// the custom migrator's `name` column. Used outside the bootstrap tx
// as a cheap fast-path; the authoritative re-probe happens inside the
// tx under the advisory lock.
func probeLegacyShape(ctx context.Context, db *sql.DB) (bool, error) {
	var has bool
	if err := db.QueryRowContext(ctx, legacyShapeProbeSQL).Scan(&has); err != nil {
		return false, fmt.Errorf("probe legacy schema_migrations: %w", err)
	}
	return has, nil
}

// probeLegacyShapeTx is the same probe scoped to the bootstrap tx so
// it sees the catalog state as of that tx's snapshot — necessary to
// detect a concurrent bootstrap that committed while we waited on the
// advisory lock.
func probeLegacyShapeTx(ctx context.Context, tx *sql.Tx) (bool, error) {
	var has bool
	if err := tx.QueryRowContext(ctx, legacyShapeProbeSQL).Scan(&has); err != nil {
		return false, fmt.Errorf("re-probe legacy schema_migrations under lock: %w", err)
	}
	return has, nil
}

const legacyShapeProbeSQL = `
	SELECT EXISTS (
	    SELECT 1 FROM information_schema.columns
	    WHERE table_schema = 'public'
	      AND table_name = 'schema_migrations'
	      AND column_name = 'name'
	)`

// formatDroppedVersions caps the slice it returns at maxLoggedDroppedVersions
// entries so a pathological migration history doesn't produce a
// wall-of-text log line. Truncation is visible to the operator via the
// trailing "...and N more" element.
func formatDroppedVersions(dropped []int) []any {
	const maxLogged = 20
	if len(dropped) <= maxLogged {
		out := make([]any, len(dropped))
		for i, v := range dropped {
			out[i] = v
		}
		return out
	}
	out := make([]any, maxLogged+1)
	for i := range maxLogged {
		out[i] = dropped[i]
	}
	out[maxLogged] = fmt.Sprintf("...and %d more", len(dropped)-maxLogged)
	return out
}

// readLegacyOSSVersions reads every legacy schema_migrations.version
// in the OSS range [201, 499] into a slice. Done outside the bootstrap
// tx because pgx's stdlib bridge can't interleave a SELECT cursor and
// DML on the same tx-bound connection.
func readLegacyOSSVersions(ctx context.Context, db *sql.DB) ([]int, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT version FROM schema_migrations WHERE version BETWEEN 201 AND 499 ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("read legacy OSS rows: %w", err)
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan legacy version: %w", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate legacy rows: %w", err)
	}
	return out, nil
}

// highestCarryable returns the highest (legacy_version - 200) that
// has a matching .up.sql in the valid set, plus the orphans that
// were skipped (legacy versions whose stripped form is not in valid).
// Returns 0 for the carried version when no legacy row qualifies.
func highestCarryable(legacyVersions []int, valid map[int]bool) (carried int, dropped []int) {
	for _, v := range legacyVersions {
		stripped := v - 200
		if !valid[stripped] {
			dropped = append(dropped, v)
			continue
		}
		if stripped > carried {
			carried = stripped
		}
	}
	return carried, dropped
}

// loadValidUpVersions scans migrationsFS/dir for NNN_name.up.sql
// files and returns a set of the parsed pre-offset version numbers.
// Used by the bootstrap to filter out legacy rows whose forward SQL
// is no longer in the embedded FS (e.g. a migration that the upstream
// codebase later replaced with a no-op or removed entirely).
func loadValidUpVersions(migrationsFS fs.FS, dir string) (map[int]bool, error) {
	entries, err := fs.ReadDir(migrationsFS, dir)
	if err != nil {
		return nil, err
	}
	valid := make(map[int]bool, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			continue
		}
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		valid[v] = true
	}
	return valid, nil
}
