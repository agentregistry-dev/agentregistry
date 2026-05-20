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

// BootstrapLegacyOSSMigrations bridges OSS deployments that ran the
// pre-engine-swap custom migrator (`schema_migrations` with columns
// version/name/applied_at and a +200 version offset) to the
// golang-migrate shape (`schema_migrations` with version/dirty,
// independent counter starting at 1).
//
// On fresh installs and on deployments already bridged the function
// no-ops. On a legacy DB it runs once in a single transaction:
//
//   1. Renames the legacy table to schema_migrations_v0_legacy
//      (audit trail preserved).
//   2. Creates the new go-migrate-shaped schema_migrations table.
//   3. Copies rows with `version BETWEEN 201 AND 499` (the OSS range
//      under the old offset) into the new table with `(version - 200)`
//      and dirty=false — orphan rows whose .up.sql is no longer in
//      the embedded FS are silently dropped (logged at INFO so an
//      operator can see what was skipped).
//
// Rows with `version >= 500` are intentionally left in
// schema_migrations_v0_legacy for downstream extension bootstraps to
// claim — independent tracks split is layered, not bundled.
//
// The function is safe to call from multiple sources concurrently:
// the RENAME serializes via Postgres's ACCESS EXCLUSIVE lock, and a
// no-op outcome is the steady state once any caller has bridged.
func BootstrapLegacyOSSMigrations(ctx context.Context, dsn string, migrationsFS fs.FS, dir string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database for bootstrap: %w", err)
	}
	defer func() { _ = db.Close() }()

	// Probe: does a legacy-shape schema_migrations table exist? The
	// `name` column was on the custom migrator but is absent from
	// go-migrate's schema, so its presence is the unambiguous
	// legacy-shape marker.
	var hasNameColumn bool
	probe := `
		SELECT EXISTS (
		    SELECT 1 FROM information_schema.columns
		    WHERE table_schema = 'public'
		      AND table_name = 'schema_migrations'
		      AND column_name = 'name'
		)`
	if err := db.QueryRowContext(ctx, probe).Scan(&hasNameColumn); err != nil {
		return fmt.Errorf("probe legacy schema_migrations: %w", err)
	}
	if !hasNameColumn {
		return nil
	}

	valid, err := loadValidUpVersions(migrationsFS, dir)
	if err != nil {
		return fmt.Errorf("scan migration sources: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bootstrap tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

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

	rows, err := tx.QueryContext(ctx,
		`SELECT version FROM schema_migrations_v0_legacy WHERE version BETWEEN 201 AND 499 ORDER BY version`)
	if err != nil {
		return fmt.Errorf("read legacy OSS rows: %w", err)
	}
	defer rows.Close()

	carried := 0
	var dropped []int
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return fmt.Errorf("scan legacy version: %w", err)
		}
		stripped := version - 200
		if !valid[stripped] {
			dropped = append(dropped, version)
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, dirty) VALUES ($1, false)`,
			stripped); err != nil {
			return fmt.Errorf("insert bridged row v%d: %w", stripped, err)
		}
		carried++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate legacy rows: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bootstrap tx: %w", err)
	}

	logger := slog.Default().With("component", "database.bootstrap")
	logger.Info("bridged legacy schema_migrations to go-migrate shape",
		"carried", carried,
		"dropped", len(dropped))
	if len(dropped) > 0 {
		logger.Info("orphan legacy rows dropped during bridge (no matching .up.sql in current embed)",
			"versions", dropped)
	}
	return nil
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
