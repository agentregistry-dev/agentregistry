package database

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgUndefinedTable is the SQLSTATE code PostgreSQL returns when
// `schema_migrations` doesn't exist yet — used to make read-only
// methods on EnsureTable=false sources behave as "no rows applied"
// on a clean DB instead of erroring before the OSS source has had a
// chance to create the table.
const pgUndefinedTable = "42P01"

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUndefinedTable
}

// Migration represents a database migration. DownSQL is the contents of the
// optional NNN_name.down.sql sibling; empty when the migration has no
// .down.sql file (i.e. it is not reversible).
type Migration struct {
	Version int
	Name    string
	SQL     string
	DownSQL string
}

// ErrNotReversible indicates a `down` or backward `goto` would cross a
// migration that has no `.down.sql` sibling.
var ErrNotReversible = errors.New("migration is not reversible")

// ErrOutOfRange indicates a version was passed to MigrateTo / Force that
// does not fall within this migrator's VersionOffset range.
var ErrOutOfRange = errors.New("version out of source range")

// MigratorConfig configures a migrator instance.
// This allows external libraries (e.g., Enterprise extensions) to provide
// their own migrations while sharing the same schema_migrations table.
type MigratorConfig struct {
	// MigrationFiles is the embedded filesystem containing migration files.
	// The filesystem should contain a directory (named by MigrationDir) with .sql files
	// named using the pattern "NNN_description.sql" (e.g., "001_initial_schema.sql").
	MigrationFiles embed.FS
	// MigrationDir is the directory within MigrationFiles to read migrations from.
	// Defaults to "migrations" when empty.
	MigrationDir string
	// VersionOffset is added to all migration versions to avoid conflicts.
	// Set to 0 for OSS migrations, 500+ for extensions.
	// This allows multiple migration sources to avoid collisions.
	VersionOffset int
	// EnsureTable creates the schema_migrations table if it doesn't exist.
	// Set to true for OSS (creates table), false for extensions (assumes it exists from OSS).
	EnsureTable bool
}

// Migrator handles database migrations.
// It supports configurable migration sources and version offsets to allow
// multiple migration sets (e.g., OSS + extensions) to coexist.
type Migrator struct {
	conn   *pgx.Conn
	config MigratorConfig
	logger *slog.Logger
}

// NewMigrator creates a new migrator instance with the given configuration.
func NewMigrator(conn *pgx.Conn, config MigratorConfig) *Migrator {
	return &Migrator{
		conn:   conn,
		config: config,
		logger: slog.Default().With("component", "database.migrate"),
	}
}

// ensureMigrationsTable creates the migrations tracking table if it doesn't exist
func (m *Migrator) ensureMigrationsTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)
	`
	_, err := m.conn.Exec(ctx, query)
	return err
}

// getAppliedMigrations returns a map of already applied migration versions.
// When schema_migrations doesn't exist yet (clean DB, EnsureTable=false
// source called before the table-owning source ran) the result is an
// empty map — equivalent semantics to "no rows applied", which is what
// a fresh DB conceptually is.
func (m *Migrator) getAppliedMigrations(ctx context.Context) (map[int]struct{}, error) {
	query := "SELECT version FROM schema_migrations ORDER BY version"
	rows, err := m.conn.Query(ctx, query)
	if err != nil {
		if isUndefinedTable(err) {
			return map[int]struct{}{}, nil
		}
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]struct{})
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("failed to scan migration version: %w", err)
		}
		applied[version] = struct{}{}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read migration rows: %w", err)
	}

	return applied, nil
}

// loadMigrations loads all migration files from the embedded filesystem.
// For each NNN_name.sql found, an optional NNN_name.down.sql sibling in the
// same directory is loaded into DownSQL; absence is fine and means the
// migration is up-only.
func (m *Migrator) loadMigrations() ([]Migration, error) {
	dir := cmp.Or(m.config.MigrationDir, "migrations")
	entries, err := m.config.MigrationFiles.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		// Skip .down.sql siblings in the main scan; they're loaded
		// alongside their up counterpart below.
		if strings.HasSuffix(entry.Name(), ".down.sql") {
			continue
		}

		// Parse version from filename (e.g., "001_initial_schema.sql" -> version 1)
		name := entry.Name()
		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			m.logger.Error("skipping migration file with invalid name format", "name", name)
			continue
		}

		version, err := strconv.Atoi(parts[0])
		if err != nil {
			m.logger.Error("skipping migration file with invalid version", "name", name)
			continue
		}

		// Apply version offset
		offsetVersion := version + m.config.VersionOffset

		// Read the migration SQL
		content, err := m.config.MigrationFiles.ReadFile(path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file %s: %w", name, err)
		}

		// Optional sibling: NNN_name.down.sql. Absent => up-only.
		// Any error other than "does not exist" is fatal so a real fs
		// problem (permission, IO) isn't silently misread as up-only.
		downName := strings.TrimSuffix(name, ".sql") + ".down.sql"
		var downContent []byte
		downBytes, derr := m.config.MigrationFiles.ReadFile(path.Join(dir, downName))
		switch {
		case derr == nil:
			downContent = downBytes
		case errors.Is(derr, fs.ErrNotExist):
			// up-only migration; nothing to load.
		default:
			return nil, fmt.Errorf("failed to read down sibling %s: %w", downName, derr)
		}

		// Generate migration name with offset if offset is applied
		var migrationName string
		if m.config.VersionOffset > 0 {
			// Add offset to name to avoid conflicts with OSS migrations
			migrationName = fmt.Sprintf("%d_%s", offsetVersion, parts[1])
		} else {
			migrationName = name
		}

		migrations = append(migrations, Migration{
			Version: offsetVersion,
			Name:    strings.TrimSuffix(migrationName, ".sql"),
			SQL:     string(content),
			DownSQL: string(downContent),
		})
	}

	// Sort migrations by version
	slices.SortFunc(migrations, func(a, b Migration) int {
		return cmp.Compare(a.Version, b.Version)
	})

	return migrations, nil
}

// Migrate runs all pending migrations.
func (m *Migrator) Migrate(ctx context.Context) error {
	// Ensure the migrations table exists if configured
	if m.config.EnsureTable {
		if err := m.ensureMigrationsTable(ctx); err != nil {
			return fmt.Errorf("failed to create migrations table: %w", err)
		}
	}

	// Get applied migrations
	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Load all migration files
	migrations, err := m.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Find pending migrations
	var pending []Migration
	for _, migration := range migrations {
		if _, ok := applied[migration.Version]; !ok {
			pending = append(pending, migration)
		}
	}

	if len(pending) == 0 {
		m.logger.Info("no pending migrations")
		return nil
	}

	m.logger.Info("applying pending migrations", "count", len(pending))

	// Apply each pending migration in a transaction
	for _, migration := range pending {
		if err := m.applyMigration(ctx, migration); err != nil {
			return fmt.Errorf("failed to apply migration %s (v%d): %w", migration.Name, migration.Version, err)
		}
		m.logger.Info("applied migration", "version", migration.Version, "name", migration.Name)
	}

	m.logger.Info("all migrations applied successfully")
	return nil
}

// applyMigration applies a single migration in a transaction
func (m *Migrator) applyMigration(ctx context.Context, migration Migration) error {
	tx, err := m.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		// Rollback is safe to be called after a transaction is committed, where it won't be rolled back (ErrTxClosed).
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			m.logger.Error("failed to rollback migration transaction", "error", err)
		}
	}()

	// Execute the migration SQL
	_, err = tx.Exec(ctx, migration.SQL)
	if err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	// Record the migration as applied
	_, err = tx.Exec(ctx,
		"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)",
		migration.Version, migration.Name)
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit(ctx)
}

// CurrentVersion returns the highest applied migration version within this
// source's VersionOffset range. Returns 0 when nothing in this source has
// been applied. Self-ensures schema_migrations when EnsureTable=true so
// read-only callers (e.g. `arctl db migrate version`) work against a
// fresh DB.
func (m *Migrator) CurrentVersion(ctx context.Context) (int, error) {
	if m.config.EnsureTable {
		if err := m.ensureMigrationsTable(ctx); err != nil {
			return 0, fmt.Errorf("failed to create migrations table: %w", err)
		}
	}
	low, high := m.sourceRange()
	// When sourceRange reports the empty-range sentinel (low, low-1),
	// `version BETWEEN low AND high` evaluates to false for every row
	// (PostgreSQL semantics: BETWEEN x AND y is false when x > y), so
	// MAX is NULL and COALESCE returns 0 — correct for an empty source.
	var current int
	err := m.conn.QueryRow(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM schema_migrations WHERE version BETWEEN $1 AND $2",
		low, high,
	).Scan(&current)
	if err != nil {
		// Same treatment as getAppliedMigrations: a missing table on a
		// fresh DB is semantically "no version applied" rather than an
		// error to surface, so EnsureTable=false read-only callers work
		// before the table-owning source has migrated.
		if isUndefinedTable(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to query current version: %w", err)
	}
	return current, nil
}

// Status returns (appliedVersions, pendingMigrations) for this source.
// appliedVersions contains every schema_migrations row whose version
// is in this source's [VersionOffset+1, max-filename-version] range,
// regardless of whether the matching .sql file is currently visible
// to loadMigrations (Skip-filtered versions still appear here when a
// row exists). pendingMigrations are migrations loaded from
// MigrationFiles that are not yet applied; Skip-filtered files are
// absent from this slice. The asymmetry is intentional: it lets
// `down`/`goto` surface orphan rows (the "no matching file" error)
// when a Skip-toggle leaves a previously-applied version unreachable.
// Self-ensures schema_migrations when EnsureTable=true so read-only
// callers (e.g. `arctl db migrate status`) work against a fresh DB.
func (m *Migrator) Status(ctx context.Context) (applied []int, pending []Migration, err error) {
	if m.config.EnsureTable {
		if err := m.ensureMigrationsTable(ctx); err != nil {
			return nil, nil, fmt.Errorf("failed to create migrations table: %w", err)
		}
	}
	migrations, err := m.loadMigrations()
	if err != nil {
		return nil, nil, err
	}
	appliedSet, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return nil, nil, err
	}
	low, high := m.sourceRange()
	for v := range appliedSet {
		if v >= low && v <= high {
			applied = append(applied, v)
		}
	}
	slices.Sort(applied)
	for _, mig := range migrations {
		if _, ok := appliedSet[mig.Version]; !ok {
			pending = append(pending, mig)
		}
	}
	return applied, pending, nil
}

// Down rolls back the n most-recent applied migrations within this source's
// VersionOffset range. Each rollback runs the migration's DownSQL and
// deletes the schema_migrations row in a single transaction. Errors with
// ErrNotReversible (wrapped with the migration name + version) when a
// crossed migration has no DownSQL.
//
// Rollbacks are NOT batched into one transaction: a failure mid-sequence
// leaves prior rollbacks committed. Operators investigating a partial
// rollback should run `arctl db migrate status` to see the resulting
// applied set and decide whether to retry, re-apply (`up`), or `force`
// a specific version after manual cleanup.
func (m *Migrator) Down(ctx context.Context, n int) error {
	if n <= 0 {
		return nil
	}
	applied, _, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if n > len(applied) {
		return fmt.Errorf("cannot roll back %d migration(s); only %d applied in this source", n, len(applied))
	}
	migrations, err := m.loadMigrations()
	if err != nil {
		return err
	}
	byVersion := make(map[int]Migration, len(migrations))
	for _, mig := range migrations {
		byVersion[mig.Version] = mig
	}
	// Roll back the n most-recent applied versions (descending order).
	for i := len(applied) - 1; i >= len(applied)-n; i-- {
		v := applied[i]
		mig, ok := byVersion[v]
		if !ok {
			return fmt.Errorf("applied migration v%d has no matching file (likely filtered out by the migrator's Skip predicate); cannot roll back", v)
		}
		if mig.DownSQL == "" {
			return fmt.Errorf("%w: migration %s (v%d) has no .down.sql sibling", ErrNotReversible, mig.Name, mig.Version)
		}
		if err := m.rollbackMigration(ctx, mig); err != nil {
			return fmt.Errorf("failed to roll back migration %s (v%d): %w", mig.Name, mig.Version, err)
		}
		m.logger.Info("rolled back migration", "version", mig.Version, "name", mig.Name)
	}
	return nil
}

// MigrateTo brings this source's portion of schema_migrations to exactly
// version. Forward when version > current (runs pending up to and
// including version); backward when version < current (chains Down calls).
// Errors with ErrOutOfRange when version is outside this source's
// VersionOffset range, or when version is in range but is not an actual
// migration file in this source — silently no-opping on an "in range but
// not a known version" target was misleading callers, so the contract
// requires version to be a real migration version.
func (m *Migrator) MigrateTo(ctx context.Context, version int) error {
	low, high := m.sourceRange()
	if version < low || version > high {
		return fmt.Errorf("%w: %d not in [%d, %d]", ErrOutOfRange, version, low, high)
	}
	if m.config.EnsureTable {
		if err := m.ensureMigrationsTable(ctx); err != nil {
			return fmt.Errorf("failed to create migrations table: %w", err)
		}
	}
	migrations, err := m.loadMigrations()
	if err != nil {
		return err
	}
	known := false
	for _, mig := range migrations {
		if mig.Version == version {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("%w: %d is in [%d, %d] but no migration file exists at that version", ErrOutOfRange, version, low, high)
	}
	current, err := m.CurrentVersion(ctx)
	if err != nil {
		return err
	}
	if version == current {
		return nil
	}
	if version > current {
		return m.migrateForwardTo(ctx, version)
	}
	// version < current: chain down.
	applied, _, err := m.Status(ctx)
	if err != nil {
		return err
	}
	steps := 0
	for _, v := range applied {
		if v > version {
			steps++
		}
	}
	return m.Down(ctx, steps)
}

// migrateForwardTo applies pending migrations up to and including target.
func (m *Migrator) migrateForwardTo(ctx context.Context, target int) error {
	applied, err := m.getAppliedMigrations(ctx)
	if err != nil {
		return err
	}
	migrations, err := m.loadMigrations()
	if err != nil {
		return err
	}
	for _, mig := range migrations {
		if mig.Version > target {
			break
		}
		if _, ok := applied[mig.Version]; ok {
			continue
		}
		if err := m.applyMigration(ctx, mig); err != nil {
			return fmt.Errorf("failed to apply migration %s (v%d): %w", mig.Name, mig.Version, err)
		}
		m.logger.Info("applied migration", "version", mig.Version, "name", mig.Name)
	}
	return nil
}

// Force inserts a schema_migrations row for version without running its
// SQL. Used to reconcile state after manual remediation. Version must be a
// known migration in this source's offset range; idempotent if the row
// already exists.
func (m *Migrator) Force(ctx context.Context, version int) error {
	low, high := m.sourceRange()
	if version < low || version > high {
		return fmt.Errorf("%w: %d not in [%d, %d]", ErrOutOfRange, version, low, high)
	}
	if m.config.EnsureTable {
		if err := m.ensureMigrationsTable(ctx); err != nil {
			return fmt.Errorf("failed to create migrations table: %w", err)
		}
	}
	migrations, err := m.loadMigrations()
	if err != nil {
		return err
	}
	for _, mig := range migrations {
		if mig.Version != version {
			continue
		}
		_, err := m.conn.Exec(ctx,
			"INSERT INTO schema_migrations (version, name) VALUES ($1, $2) ON CONFLICT (version) DO NOTHING",
			mig.Version, mig.Name,
		)
		if err != nil {
			return fmt.Errorf("failed to record forced migration: %w", err)
		}
		m.logger.Info("forced migration", "version", mig.Version, "name", mig.Name)
		return nil
	}
	return fmt.Errorf("no migration file found for version %d (may be filtered out by the migrator's Skip predicate)", version)
}

// sourceRange returns the inclusive [low, high] version bounds for
// this source. Wraps SourceRange so callers with a *Migrator handle
// don't have to construct a config to ask the question; load errors
// are logged here (the package-free SourceRange can't log).
func (m *Migrator) sourceRange() (int, int) {
	low, high, err := sourceRangeFromConfig(m.config)
	if err != nil {
		m.logger.Error("failed to load migrations while computing source range; treating as empty", "error", err)
	}
	return low, high
}

// SourceRange returns the inclusive [low, high] version bounds for the
// given MigratorConfig. low is VersionOffset + 1; high is VersionOffset
// + the highest pre-offset version in MigrationFiles (after Skip
// filtering). Returns (low, low-1) — a sentinel "empty range" that
// matches no version — when the source has no migration files; callers
// do `if v < low || v > high` range checks that uniformly reject
// everything against an empty range.
//
// CLI routing (which runs before a connection is opened) and the
// Migrator's own range checks both go through this function so the two
// paths can't drift apart.
func SourceRange(cfg MigratorConfig) (low, high int) {
	low, high, _ = sourceRangeFromConfig(cfg)
	return low, high
}

func sourceRangeFromConfig(cfg MigratorConfig) (low, high int, err error) {
	low = cfg.VersionOffset + 1
	migrations, err := loadMigrationsFromConfig(cfg)
	if err != nil {
		return low, low - 1, err
	}
	if len(migrations) == 0 {
		return low, low - 1, nil
	}
	return low, migrations[len(migrations)-1].Version, nil
}

// loadMigrationsFromConfig is the package-free body of loadMigrations
// (no *Migrator receiver, no logger). Used by SourceRange so the CLI
// can compute bounds without constructing a Migrator.
func loadMigrationsFromConfig(cfg MigratorConfig) ([]Migration, error) {
	m := &Migrator{config: cfg, logger: slog.Default()}
	return m.loadMigrations()
}

// rollbackMigration runs migration.DownSQL and deletes the schema_migrations
// row in a single transaction.
func (m *Migrator) rollbackMigration(ctx context.Context, migration Migration) error {
	tx, err := m.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			m.logger.Error("failed to rollback down transaction", "error", err)
		}
	}()

	if _, err = tx.Exec(ctx, migration.DownSQL); err != nil {
		return fmt.Errorf("failed to execute down SQL: %w", err)
	}
	if _, err = tx.Exec(ctx, "DELETE FROM schema_migrations WHERE version = $1", migration.Version); err != nil {
		return fmt.Errorf("failed to delete migration row: %w", err)
	}
	return tx.Commit(ctx)
}
