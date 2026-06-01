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
//
// # Schema is a resolved value (extension authors)
//
// A source's schema is a database.Schema, not a string: it is resolved
// (validated + its quoted identifier precomputed) once when the Source
// is built, and that value is threaded everywhere the schema is needed —
// nothing re-derives it per operation. An extension registering its own
// source builds the value once at init:
//
//	orchestrator.Source{
//	    Name:      "ext",
//	    Schema:    database.MustNewSchema("agentregistry_ext"),
//	    Files:     extMigrations,
//	    Dir:       "migrations",
//	    LegacyRun: ext.RunBridge, // func(ctx, *sql.DB, database.Schema) error
//	}
//
// Use database.NewSchema(name) (returns an error) when the name is
// operator/runtime input; database.MustNewSchema(name) (panics) for a
// const or an init-time registration value. A LegacyRun callback
// receives the source's Schema so it can address its destination tables
// via schema.Qualify("table") without re-resolving it.
//
// For a query that spans schemas (e.g. an extension joining its table
// against an OSS table), build a database.SchemaRegistry at the
// composition root, register each source's schema, and inject it; then
// reg.Get("oss").Qualify("agents") yields a safe, schema-qualified
// reference. App queries that stay within one schema don't need the
// registry — they go through a v1alpha1store.Store, which already holds
// its resolved schema and qualifies for them.
//
// # LegacyRun ordering for Source authors
//
// Sources that supply a `LegacyRun` callback can rely on the following
// invariant: `LegacyRun` is invoked after `mg.Steps(1)` has applied
// the source's first migration and before `mg.Up()` runs the rest.
// So at LegacyRun time, the schema reflects migration 001's tables and
// indexes but not 002+. Sources whose `LegacyRun` copies into specific
// tables must either keep those tables in 001 or accept that the
// destination shape will not have advanced past 001.
//
// `LegacyRun` is gated on `public.schema_migrations` (the prior custom
// migrator's bookkeeping table) existing. The data-copy must be
// idempotent under re-invocation (the OSS source uses
// `INSERT ... ON CONFLICT DO NOTHING`) because after a successful run
// the orchestrator renames `public.schema_migrations` aside, which
// closes the gate naturally on subsequent runs. Gating on
// `public.schema_migrations` alone (rather than also on the source's
// own `schema_migrations` row count) is what makes the bridge survive
// a partial run that committed `Steps(1)` and then aborted before
// `LegacyRun` fired.
//
// After every source's per-source sequence completes, if at least one
// source's `LegacyRun` actually fired this run, the orchestrator renames
// `public.schema_migrations` to `public.schema_migrations_v0_legacy`.
// The rename is gated on the bridge so an external user of the
// `public.schema_migrations` table (e.g. an unrelated golang-migrate
// setup against the same database) is never silently touched.
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
	migratedb "github.com/golang-migrate/migrate/v4/database"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// Source describes one set of migrations to be applied as part of the
// orchestrator's startup sequence.
type Source struct {
	// Name is the operator-visible label (shown in logs, accepted by the
	// CLI's `--source` flag) and the input to the advisory-lock key hash.
	// It is never interpolated into SQL, so it only needs to be a stable,
	// unique-per-source token; keep it short and lowercase for readability.
	Name string

	// Schema is the Postgres schema this source's tables live in,
	// resolved once (its quoted identifier is precomputed). Build it with
	// database.NewSchema(name) — or database.MustNewSchema(name) at
	// init/registration where the name is a known-valid const. The
	// orchestrator threads this value everywhere it needs the schema
	// (migrator construction, the schema_migrations probes); nothing
	// re-derives it from a string. `golang-migrate`'s pgx/v5 driver is
	// configured with Schema.Name(); the source's `schema_migrations`
	// table is created in that schema.
	Schema database.Schema

	// Files is the embedded filesystem holding NNN_name.up.sql /
	// NNN_name.down.sql pairs.
	Files fs.FS

	// Dir is the directory inside Files containing the migration
	// pairs.
	Dir string

	// LegacyRun, when non-nil, is invoked between `mg.Steps(1)` and
	// `mg.Up()` whenever `public.schema_migrations` exists. It receives
	// the source's resolved Schema so the bridge can address the
	// destination tables (e.g. schema.Qualify("agents")) without
	// re-resolving it. The callback must be idempotent under
	// re-invocation: on a successful run the orchestrator renames
	// `public.schema_migrations` aside, which closes the gate naturally
	// on subsequent runs, but the gate also fires on the recovery path
	// after a partial run that committed `Steps(1)` and aborted before
	// this callback ran. Fresh installs (no `public.schema_migrations`
	// ever) skip cleanly.
	LegacyRun func(ctx context.Context, db *sql.DB, schema database.Schema) error
}

// orchestratorGlobalLockKey is the advisory-lock key held for the whole
// of RunUp so concurrent pods run the migrate sequence strictly one at a
// time. It is a fixed literal — never derived from a Source name — so no
// Source's per-source lock can ever collide with it.
const orchestratorGlobalLockKey int64 = 0x6172_6f72_6368_6573 // "arorches"

// RunUp serializes the entire migrate sequence behind a single global
// advisory lock, then for each Source opens a dedicated single-connection
// handle, acquires a per-source `pg_advisory_lock`, applies `Steps(1)`,
// invokes the legacy bridge if applicable, then `Up()`. After every
// Source succeeds, if at least one source's `LegacyRun` fired this run,
// `public.schema_migrations` (the prior custom migrator's bookkeeping
// table) is renamed to `public.schema_migrations_v0_legacy`. The
// bridge-gate keeps RunUp from touching an unrelated owner of that table.
//
// # Concurrency
//
// The global lock means only one pod is ever inside the source loop or
// the failure-restore path; losers block, then re-probe and no-op once
// they acquire it. This is what makes the cross-source restore safe —
// otherwise one pod restoring a source could clobber a version another
// pod legitimately advanced. The per-source locks (runSource /
// WithSourceLock) are retained to serialize RunUp against the CLI's
// `down`/`goto`/`force`; RunUp always takes the global lock first and the
// CLI never takes it, so there is no lock-ordering cycle.
//
// # Cross-source atomicity
//
// If any source fails, every source this run touched — the failing one
// and all earlier-applied ones — is restored to its pre-run version, so
// the database returns to the prior release's version combo instead of a
// cross-track state no release ships. This is a compensating rollback,
// not a transaction (each migrator owns its own connection), so it
// depends on two invariants: every incremental migration ships a real,
// idempotent `.down.sql`, and each migration file applies as one implicit
// transaction (so a failed file's DDL rolls back atomically, leaving only
// a dirty marker — never half-applied schema). The lint test enforces the
// latter for the common cases; see pkg/registry/v1alpha1store/migrations/README.md.
//
// Sources first-installed this run are reset to NilVersion if they carry
// a dirty marker (their idempotent floor re-applies on a re-run) and
// otherwise left at their freshly-applied version. A source found already
// dirty at entry is not migrated: RunUp restores the prior sources and
// surfaces it for operator `force`, since its true state is ambiguous.
func RunUp(ctx context.Context, dsn string, sources []Source) error {
	globalDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database for global lock: %w", err)
	}
	globalDB.SetMaxOpenConns(1)
	defer func() { _ = globalDB.Close() }()
	if _, err := globalDB.ExecContext(ctx, "SELECT pg_advisory_lock($1)", orchestratorGlobalLockKey); err != nil {
		return fmt.Errorf("acquire global orchestrator lock: %w", err)
	}
	defer func() {
		if _, err := globalDB.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", orchestratorGlobalLockKey); err != nil {
			slog.Default().Warn("release global orchestrator lock",
				"component", "database.orchestrator", "error", err)
		}
	}()

	bridged := false
	var applied []appliedSource
	for _, src := range sources {
		// Snapshot the version this source sits at before we touch it, so
		// a failure can restore it to exactly this point.
		pre, dirty, present, err := preRunVersion(ctx, dsn, src.Schema)
		if err != nil {
			return errors.Join(
				fmt.Errorf("snapshot pre-run version for source %s: %w", src.Name, err),
				restoreAll(ctx, dsn, applied))
		}
		if dirty {
			// The source entered this run dirty (a prior run left a marker
			// whose schema state is ambiguous). Don't guess a restore
			// target for it: roll the prior sources back and surface it for
			// `arctl db migrate force`.
			return errors.Join(
				fmt.Errorf("source %s entered dirty at v%d; resolve with `arctl db migrate force` before migrating", src.Name, pre),
				restoreAll(ctx, dsn, applied))
		}

		ran, runErr := runSource(ctx, dsn, src)
		if runErr != nil {
			// Restore the failing source and every prior applied source to
			// their pre-run versions. The failing source is appended last
			// so restoreAll reverses it first.
			touched := append(append([]appliedSource{}, applied...),
				appliedSource{src: src, preVersion: pre, present: present})
			if rErr := restoreAll(ctx, dsn, touched); rErr != nil {
				return errors.Join(
					fmt.Errorf("run source %s failed: %w", src.Name, runErr),
					fmt.Errorf("restoring sources to their pre-run versions also failed: %w", rErr))
			}
			return fmt.Errorf("run source %s failed; all sources restored to their pre-run versions: %w", src.Name, runErr)
		}

		applied = append(applied, appliedSource{src: src, preVersion: pre, present: present})
		if ran {
			bridged = true
		}
	}
	if !bridged {
		return nil
	}
	return renameLegacyOnce(ctx, dsn)
}

// appliedSource records a source RunUp touched, with the version it sat
// at beforehand — the target restoreSource returns it to on failure.
type appliedSource struct {
	src        Source
	preVersion uint
	present    bool // whether the source had a prior applied version at entry
}

// restoreAll restores the given sources to their pre-run versions in
// reverse of slice order (most-recently-touched first). Per-source
// errors are collected and joined rather than aborting on the first, so
// one un-restorable source doesn't strand the others.
func restoreAll(ctx context.Context, dsn string, sources []appliedSource) error {
	var errs []error
	for i := len(sources) - 1; i >= 0; i-- {
		if err := restoreSource(ctx, dsn, sources[i]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// restoreSource returns a single source to the version it sat at before
// RunUp touched it. It is the one rollback primitive for both the failing
// source (which may carry a dirty marker) and prior applied sources
// (clean), under the source's advisory lock.
//
//   - A dirty marker (only the failing source can carry one — go-migrate
//     commits the dirty flag before running a migration body that then
//     rolled back atomically) is force-cleared first so Migrate can run.
//   - A source first-installed this run (present == false) is reset to
//     NilVersion if it still carries a marker, and otherwise left at its
//     freshly-applied version. Its tables are left in place: the up-only
//     `001` floor cannot be reversed, and any migrations that committed
//     before the failure (002+) are left applied too — all of them are
//     idempotent, so a re-run replays from NilVersion and converges. For
//     a freshly-installed source the prior release's combo is "absent",
//     and the leftover tables are harmless to a prior binary that does
//     not know the source.
//   - An upgraded source (present == true) is migrated back down to its
//     entry version, running only the `.down.sql` files for the
//     incremental migrations — never the up-only floor (entry >= 1).
func restoreSource(ctx context.Context, dsn string, a appliedSource) error {
	logger := slog.Default().With("component", "database.orchestrator", "source", a.src.Name)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()

	lockKey := advisoryLockKey(a.src.Name)
	if _, err := db.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("acquire advisory lock for source %s: %w", a.src.Name, err)
	}
	defer func() {
		if _, err := db.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
			logger.Warn("release advisory lock", "error", err)
		}
	}()

	mg, err := database.NewMigrator(ctx, dsn, a.src.Files, a.src.Dir, a.src.Schema)
	if err != nil {
		return fmt.Errorf("construct migrator for source %s: %w", a.src.Name, err)
	}
	defer func() { _, _ = mg.Close() }()

	cur, dirty, verErr := mg.Version()
	switch {
	case errors.Is(verErr, migrate.ErrNilVersion):
		// No marker at all — nothing to restore.
		return nil
	case verErr != nil:
		return fmt.Errorf("read current version for source %s: %w", a.src.Name, verErr)
	}

	if dirty {
		// The failed migration body rolled back atomically (see the
		// single-transaction invariant in pkg/registry/v1alpha1store/migrations/README.md), so cur's
		// DDL is not present. Force the marker clean at cur so Migrate can
		// run; the down to the entry version reverses the migrations that
		// did commit this run. cur is a migration version (bounded by the
		// source's migration count), so the uint->int narrow is safe.
		if err := mg.Force(int(cur)); err != nil {
			return fmt.Errorf("clear dirty marker for source %s at v%d: %w", a.src.Name, cur, err)
		}
	}

	if !a.present {
		if dirty {
			if err := mg.Force(migratedb.NilVersion); err != nil {
				return fmt.Errorf("reset freshly-installed source %s to nil: %w", a.src.Name, err)
			}
			logger.Info("reset freshly-installed failing source to nil (floor DDL is idempotent on re-run)")
		}
		return nil
	}

	logger.Info("restoring source to its pre-run version", "to_version", a.preVersion)
	if err := mg.Migrate(a.preVersion); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate source %s down to v%d: %w", a.src.Name, a.preVersion, err)
	}
	return nil
}

// preRunVersion reports the version `<schema>.schema_migrations` records,
// whether that row is marked dirty, and whether any version is applied at
// all. A missing table or empty bookkeeping means the source is being
// first-installed (present == false).
func preRunVersion(ctx context.Context, dsn string, schema database.Schema) (version uint, dirty, present bool, err error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return 0, false, false, fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	var oid sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT to_regclass($1)::text", schema.Name()+".schema_migrations").Scan(&oid); err != nil {
		return 0, false, false, fmt.Errorf("probe %s.schema_migrations: %w", schema.Name(), err)
	}
	if !oid.Valid {
		return 0, false, false, nil
	}
	var (
		v uint64
		d bool
	)
	// go-migrate keeps a single bookkeeping row (version, dirty).
	switch err := db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT version, dirty FROM %s.schema_migrations LIMIT 1", schema.Quoted())).Scan(&v, &d); {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, false, nil
	case err != nil:
		return 0, false, false, fmt.Errorf("read %s.schema_migrations version: %w", schema.Name(), err)
	}
	return uint(v), d, true, nil
}

// runSource executes the per-Source sequence:
//
//  1. Open `*sql.DB` with `MaxOpenConns = 1`.
//  2. `pg_advisory_lock(<hash(src.Name)>)` (session-level; released on
//     close).
//  3. Snapshot the row count of `<src.Schema>.schema_migrations`
//     (zero if the table doesn't yet exist).
//  4. Build `*migrate.Migrate` against `src.Schema`.
//  5. `mg.Steps(1)` (skipped if the pre-snapshot was non-zero — the
//     row points at the already-applied first migration).
//  6. If `src.LegacyRun != nil` AND `public.schema_migrations` exists,
//     invoke `src.LegacyRun`. The pre-Steps row count is intentionally
//     not part of this gate so the bridge fires on the recovery path
//     after a partial run that committed `Steps(1)` and aborted.
//  7. `mg.Up()`.
//  8. Close mg + db (releases advisory lock).
//
// Returns whether `src.LegacyRun` actually fired this invocation —
// `RunUp` uses that signal to gate the legacy-table rename.
func runSource(ctx context.Context, dsn string, src Source) (legacyRan bool, err error) {
	logger := slog.Default().With("component", "database.orchestrator", "source", src.Name)

	// This *sql.DB holds the orchestrator's per-source advisory lock
	// for the duration of the run. database.NewMigrator below opens
	// its OWN *sql.DB for go-migrate's use — go-migrate has its own
	// internal lock against its `schema_migrations` table and must not
	// share a connection with the orchestrator-held lock.
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return false, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()

	lockKey := advisoryLockKey(src.Name)
	if _, err := db.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return false, fmt.Errorf("acquire advisory lock: %w", err)
	}
	defer func() {
		if _, err := db.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
			logger.Warn("release advisory lock", "error", err)
		}
	}()

	preStepsCount, err := schemaMigrationsRowCount(ctx, db, src.Schema)
	if err != nil {
		return false, fmt.Errorf("snapshot schema_migrations row count: %w", err)
	}

	mg, err := database.NewMigrator(ctx, dsn, src.Files, src.Dir, src.Schema)
	if err != nil {
		return false, fmt.Errorf("construct migrator: %w", err)
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
			return false, fmt.Errorf("apply first migration: %w", err)
		}
	}

	// LegacyRun is gated on the legacy table's existence ALONE, not
	// on preStepsCount. The data-copy is idempotent (INSERT ... ON
	// CONFLICT DO NOTHING) and renameLegacyOnce closes the gate on
	// subsequent runs by moving public.schema_migrations aside. The
	// alternative (gating on preStepsCount == 0) would permanently
	// skip the bridge if a prior invocation committed Steps(1) and
	// then aborted before LegacyRun fired.
	if src.LegacyRun != nil {
		legacyExists, err := publicSchemaMigrationsExists(ctx, db)
		if err != nil {
			return false, fmt.Errorf("probe public.schema_migrations: %w", err)
		}
		if legacyExists {
			logger.Info("running legacy data bridge")
			if err := src.LegacyRun(ctx, db, src.Schema); err != nil {
				return false, fmt.Errorf("legacy bridge: %w", err)
			}
			legacyRan = true
		}
	}

	if err := mg.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return legacyRan, fmt.Errorf("apply remaining migrations: %w", err)
	}
	return legacyRan, nil
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
	logger.Info("renamed public.schema_migrations to public.schema_migrations_v0_legacy; legacy data tables in v1alpha1.* are not removed by this rename and remain available for inspection")
	return nil
}

// schemaMigrationsRowCount returns the count of rows in
// `<schema>.schema_migrations`, or 0 if the table doesn't exist.
func schemaMigrationsRowCount(ctx context.Context, db *sql.DB, schema database.Schema) (int, error) {
	var oid sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT to_regclass($1)::text", schema.Name()+".schema_migrations").Scan(&oid); err != nil {
		return 0, fmt.Errorf("regclass probe: %w", err)
	}
	if !oid.Valid {
		return 0, nil
	}
	var count int
	q := fmt.Sprintf("SELECT count(*) FROM %s.schema_migrations", schema.Quoted())
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

// WithSourceLock opens a dedicated single-connection database handle,
// acquires the orchestrator's per-source `pg_advisory_lock`, runs fn,
// then releases the lock and closes the connection. Exposed so CLI
// per-source operations (down / goto / force) can serialize against
// orchestrator-driven `up` and against each other — without it, two
// CLI invocations would only share go-migrate's internal lock on
// schema_migrations, leaving the LegacyRun window unguarded.
//
// fn receives the underlying *sql.DB so callers that need to issue
// auxiliary queries (e.g. probing schema state) can share the locked
// session.
func WithSourceLock(ctx context.Context, dsn, sourceName string, fn func(db *sql.DB) error) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database for advisory lock: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()

	lockKey := advisoryLockKey(sourceName)
	if _, err := db.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("acquire advisory lock for source %s: %w", sourceName, err)
	}
	defer func() {
		if _, err := db.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
			slog.Default().Warn("release advisory lock",
				"component", "database.orchestrator",
				"source", sourceName,
				"error", err)
		}
	}()
	return fn(db)
}
