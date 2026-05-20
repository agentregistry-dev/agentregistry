package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver — required by sql.Open("pgx", ...)
)

// BootstrapAdvisoryLockKey is the pg_advisory_xact_lock key under
// which BootstrapLegacyOSSMigrations serializes. ASCII "arboot-o"
// packed into an int64 — stable, reproducible across processes.
//
// Downstream extensions running BootstrapLegacyTrack against the
// renamed `schema_migrations_v0_legacy` table MUST pass this same
// key as BootstrapLegacyTrackOptions.AdvisoryLockKey so all
// bootstraps in the binary serialize globally against the OSS
// rename. Concretely: in a rolling-deploy scenario with two server
// pods, pod 1 might be running the OSS bootstrap (renaming
// schema_migrations -> schema_migrations_v0_legacy) while pod 2 is
// starting its downstream bootstrap (reading from
// schema_migrations_v0_legacy). Without the shared lock, pod 2 sees
// the old name and bails out as "nothing to bridge"; with the
// shared lock, pod 2 waits for pod 1's COMMIT and observes the
// renamed table.
//
// Downstream consumers that choose a different lock key for their
// own reasons must instead poll for the renamed table's existence
// with retry — this is materially more complex and is the documented
// fallback, not the recommended path.
const BootstrapAdvisoryLockKey = int64(0x6172626f6f742d6f)

// identifierRE constrains the table-name strings BootstrapLegacyTrack
// embeds into DDL. Init-time callers pass hardcoded values, so the
// regex is defense-in-depth against typos and accidental injection if
// a downstream consumer ever sourced a name from config.
var identifierRE = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// BootstrapLegacyTrackOptions configures a single track's bridge from
// a legacy custom-migrator schema_migrations row format (version INT,
// name VARCHAR, applied_at TIMESTAMPTZ) to go-migrate's
// (version BIGINT, dirty BOOLEAN) shape.
//
// Each field is documented inline. Callers should treat the option
// struct as immutable across calls — the bootstrap reads it once.
type BootstrapLegacyTrackOptions struct {
	// LegacyTable names the table to read pre-bridge rows from.
	// For the OSS track this is "schema_migrations" (the original
	// custom-migrator table); for a downstream track that runs AFTER
	// OSS has bridged, this is whatever name OSS renamed it to
	// (typically "schema_migrations_v0_legacy").
	LegacyTable string

	// RenameLegacyTo, when non-empty, causes the bootstrap to
	// `ALTER TABLE LegacyTable RENAME TO RenameLegacyTo` inside the
	// bridging transaction. The OSS bootstrap sets this so the name
	// "schema_migrations" is freed for the new go-migrate-shaped
	// table; downstream bootstraps that read from the already-renamed
	// table leave this empty.
	RenameLegacyTo string

	// LegacyVersionRange is the inclusive [lo, hi] range of legacy
	// version numbers this track claims. Rows outside the range are
	// ignored (left in place for other tracks to claim). OSS owns
	// [201, 499]; a downstream extension would own a disjoint range
	// such as [501, 999].
	LegacyVersionRange [2]int

	// StripOffset is subtracted from each legacy version before the
	// row is written to NewTable. OSS strips 200, so legacy v201
	// becomes go-migrate v1. The stripped value must match the
	// NNN parsed from a NNN_*.up.sql file in MigrationsFS/Dir or
	// the row is skipped as an orphan.
	StripOffset int

	// NewTable names the destination table in go-migrate's shape.
	// OSS uses "schema_migrations"; downstream extensions use
	// "schema_migrations_<source-name>" (matching the per-instance
	// MigrationsTable each *migrate.Migrate is configured with).
	NewTable string

	// MigrationsFS and Dir describe the embedded migration set whose
	// NNN_*.up.sql filenames define the valid (stripped) versions
	// this bootstrap will carry forward. Orphan legacy rows whose
	// stripped version has no matching .up.sql in the embed are
	// skipped (logged for forensics).
	MigrationsFS fs.FS
	Dir          string

	// AdvisoryLockKey is the pg_advisory_xact_lock key used to
	// serialize concurrent bootstraps. Bootstraps that must serialize
	// against each other — e.g. the OSS bootstrap renaming the
	// legacy table while a downstream bootstrap waits to read from
	// the post-rename table — MUST share this key. The default OSS
	// key is exported as BootstrapAdvisoryLockKey; downstream
	// consumers running their own BootstrapLegacyTrack against the
	// renamed table should pass the same value.
	AdvisoryLockKey int64
}

// BootstrapLegacyTrack bridges a single legacy migration track to
// go-migrate's table shape. Idempotent and concurrent-safe — see
// BootstrapLegacyOSSMigrations's docstring for the operator-facing
// guarantees this provides.
//
// The algorithm:
//
//  1. Outside the lock, probe LegacyTable for the legacy `name`
//     column and NewTable for the go-migrate `dirty` column. If
//     LegacyTable is missing the legacy shape OR NewTable already
//     has the new shape, no-op — there's nothing to bridge.
//
//  2. Pre-fetch the legacy versions in LegacyVersionRange (pgx
//     stdlib doesn't allow interleaved cursor + DML on a tx-bound
//     conn, so this happens outside the bridging tx).
//
//  3. Open the bridging tx and take pg_advisory_xact_lock(AdvisoryLockKey).
//     Re-probe inside the lock so a concurrent winner's commit
//     short-circuits the loser.
//
//  4. Optionally `ALTER TABLE LegacyTable RENAME TO RenameLegacyTo`.
//
//  5. `CREATE TABLE NewTable (version BIGINT PRIMARY KEY, dirty BOOLEAN NOT NULL)`.
//
//  6. INSERT the highest legacy version (stripped of StripOffset)
//     whose stripped form has a matching .up.sql in MigrationsFS/Dir.
//     Other rows in LegacyVersionRange that lack a matching file are
//     skipped (logged).
//
//  7. COMMIT.
func BootstrapLegacyTrack(ctx context.Context, dsn string, opts BootstrapLegacyTrackOptions) error {
	if err := validateBootstrapOptions(opts); err != nil {
		return err
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database for bootstrap: %w", err)
	}
	defer func() { _ = db.Close() }()

	// Outside-lock fast path: skip everything if there's no work to
	// do. Re-probed inside the lock for race-safety, but the cheap
	// pre-flight keeps fresh installs and already-bridged DBs (the
	// common steady state) from acquiring the lock.
	bridge, err := probeBridgeNeeded(ctx, querier{db: db}, opts.LegacyTable, opts.NewTable)
	if err != nil {
		return err
	}
	if !bridge {
		return nil
	}

	valid, err := loadValidUpVersions(opts.MigrationsFS, opts.Dir)
	if err != nil {
		return fmt.Errorf("scan migration sources: %w", err)
	}

	legacyVersions, err := readLegacyVersions(ctx, db, opts.LegacyTable, opts.LegacyVersionRange[0], opts.LegacyVersionRange[1])
	if err != nil {
		return err
	}

	maxValid, dropped := highestCarryable(legacyVersions, valid, opts.StripOffset)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bootstrap tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`SELECT pg_advisory_xact_lock($1)`, opts.AdvisoryLockKey); err != nil {
		return fmt.Errorf("acquire bootstrap advisory lock: %w", err)
	}

	bridge, err = probeBridgeNeeded(ctx, querier{tx: tx}, opts.LegacyTable, opts.NewTable)
	if err != nil {
		return err
	}
	if !bridge {
		return nil
	}

	if opts.RenameLegacyTo != "" {
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf("ALTER TABLE %s RENAME TO %s", opts.LegacyTable, opts.RenameLegacyTo)); err != nil {
			return fmt.Errorf("rename legacy %s: %w", opts.LegacyTable, err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`CREATE TABLE %s (version BIGINT NOT NULL PRIMARY KEY, dirty BOOLEAN NOT NULL)`, opts.NewTable)); err != nil {
		return fmt.Errorf("create new %s: %w", opts.NewTable, err)
	}

	if maxValid > 0 {
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf("INSERT INTO %s (version, dirty) VALUES ($1, false)", opts.NewTable),
			maxValid); err != nil {
			return fmt.Errorf("insert bridged row v%d into %s: %w", maxValid, opts.NewTable, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bootstrap tx: %w", err)
	}

	logger := slog.Default().With(
		"component", "database.bootstrap",
		"legacy_table", opts.LegacyTable,
		"new_table", opts.NewTable,
	)
	logger.Info("bridged legacy track to go-migrate shape",
		"highest_carried", maxValid,
		"dropped", len(dropped))
	if len(dropped) > 0 {
		logger.Info("orphan legacy rows skipped during bridge (no matching .up.sql in current embed)",
			"versions", formatDroppedVersions(dropped))
	}
	return nil
}

// BootstrapLegacyOSSMigrations bridges OSS deployments that ran the
// pre-engine-swap custom migrator (`schema_migrations` with columns
// version/name/applied_at and a +200 version offset) to the
// golang-migrate shape (`schema_migrations` with version/dirty, where
// a single row records the highest applied version).
//
// On fresh installs and on deployments already bridged the function
// no-ops. On a legacy DB it runs once in a single transaction:
//
//  1. Renames the legacy table to schema_migrations_v0_legacy
//     (audit trail preserved).
//  2. Creates the new go-migrate-shaped schema_migrations table.
//  3. Inserts a single row whose version is the highest legacy OSS
//     version that (a) sits in [201, 499] AND (b) has a matching
//     NNN_*.up.sql in migrationsFS/dir, with the +200 offset
//     stripped. Orphan legacy rows (no matching .up.sql) are
//     skipped — the legacy table preserves them for forensics.
//
// Rows with `version >= 500` are intentionally left untouched in
// schema_migrations_v0_legacy for downstream extension bootstraps to
// claim — the independent-tracks split is layered, not bundled.
//
// Implementation delegates to BootstrapLegacyTrack with OSS-specific
// options. Downstream consumers bridging their own legacy range from
// the renamed schema_migrations_v0_legacy table should call
// BootstrapLegacyTrack directly with their own
// BootstrapLegacyTrackOptions (and pass BootstrapAdvisoryLockKey so
// their bootstrap serializes against any concurrent OSS bridge).
func BootstrapLegacyOSSMigrations(ctx context.Context, dsn string, migrationsFS fs.FS, dir string) error {
	return BootstrapLegacyTrack(ctx, dsn, BootstrapLegacyTrackOptions{
		LegacyTable:        "schema_migrations",
		RenameLegacyTo:     "schema_migrations_v0_legacy",
		LegacyVersionRange: [2]int{201, 499},
		StripOffset:        200,
		NewTable:           "schema_migrations",
		MigrationsFS:       migrationsFS,
		Dir:                dir,
		AdvisoryLockKey:    BootstrapAdvisoryLockKey,
	})
}

// validateBootstrapOptions guards the table-name strings that get
// embedded into DDL. Returns nil on a valid option struct.
func validateBootstrapOptions(opts BootstrapLegacyTrackOptions) error {
	for label, name := range map[string]string{
		"LegacyTable": opts.LegacyTable,
		"NewTable":    opts.NewTable,
	} {
		if !identifierRE.MatchString(name) {
			return fmt.Errorf("bootstrap option %s=%q must match %s", label, name, identifierRE.String())
		}
	}
	if opts.RenameLegacyTo != "" && !identifierRE.MatchString(opts.RenameLegacyTo) {
		return fmt.Errorf("bootstrap option RenameLegacyTo=%q must match %s", opts.RenameLegacyTo, identifierRE.String())
	}
	if opts.LegacyVersionRange[0] > opts.LegacyVersionRange[1] {
		return fmt.Errorf("bootstrap option LegacyVersionRange=[%d, %d] must be non-empty", opts.LegacyVersionRange[0], opts.LegacyVersionRange[1])
	}
	if opts.AdvisoryLockKey == 0 {
		return fmt.Errorf("bootstrap option AdvisoryLockKey must be non-zero (pass database.BootstrapAdvisoryLockKey to serialize against the OSS bridge)")
	}
	return nil
}

// querier is a tiny shim so the bridge-needed probe runs against
// either a *sql.DB (outside-lock fast path) or a *sql.Tx (inside-lock
// authoritative re-probe) without duplicating the SQL.
type querier struct {
	db *sql.DB
	tx *sql.Tx
}

func (q querier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if q.tx != nil {
		return q.tx.QueryRowContext(ctx, query, args...)
	}
	return q.db.QueryRowContext(ctx, query, args...)
}

// probeBridgeNeeded returns true when a bridge from legacyTable into
// newTable is required: legacyTable has the legacy `name` column AND
// newTable doesn't yet have the go-migrate `dirty` column. Either
// half being false short-circuits to "no work to do".
func probeBridgeNeeded(ctx context.Context, q querier, legacyTable, newTable string) (bool, error) {
	hasLegacy, err := probeColumn(ctx, q, legacyTable, "name")
	if err != nil {
		return false, fmt.Errorf("probe legacy table %s: %w", legacyTable, err)
	}
	if !hasLegacy {
		return false, nil
	}
	hasNew, err := probeColumn(ctx, q, newTable, "dirty")
	if err != nil {
		return false, fmt.Errorf("probe new table %s: %w", newTable, err)
	}
	return !hasNew, nil
}

const columnProbeSQL = `
	SELECT EXISTS (
	    SELECT 1 FROM information_schema.columns
	    WHERE table_schema = 'public'
	      AND table_name = $1
	      AND column_name = $2
	)`

func probeColumn(ctx context.Context, q querier, table, column string) (bool, error) {
	var has bool
	if err := q.QueryRowContext(ctx, columnProbeSQL, table, column).Scan(&has); err != nil {
		return false, err
	}
	return has, nil
}

// formatDroppedVersions caps the slice it returns at maxLogged
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

// readLegacyVersions reads every legacyTable.version in [lo, hi] into
// a slice. Done outside the bootstrap tx because pgx's stdlib bridge
// can't interleave a SELECT cursor and DML on the same tx-bound
// connection.
func readLegacyVersions(ctx context.Context, db *sql.DB, legacyTable string, lo, hi int) ([]int, error) {
	q := fmt.Sprintf(`SELECT version FROM %s WHERE version BETWEEN $1 AND $2 ORDER BY version`, legacyTable)
	rows, err := db.QueryContext(ctx, q, lo, hi)
	if err != nil {
		return nil, fmt.Errorf("read legacy rows from %s: %w", legacyTable, err)
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

// highestCarryable returns the highest (legacy_version - stripOffset)
// that has a matching .up.sql in the valid set, plus the orphans that
// were skipped (legacy versions whose stripped form is not in valid).
// Returns 0 for the carried version when no legacy row qualifies.
func highestCarryable(legacyVersions []int, valid map[int]bool, stripOffset int) (carried int, dropped []int) {
	for _, v := range legacyVersions {
		stripped := v - stripOffset
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
