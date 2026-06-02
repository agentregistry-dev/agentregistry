//go:build integration

package orchestrator_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/legacymigrate"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/orchestrator"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

const adminURI = "postgres://agentregistry:agentregistry@localhost:5432/postgres?sslmode=disable"

// newDB creates a fresh per-test Postgres database. Skips when
// localhost:5432 is unavailable.
func newDB(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminConn, err := pgx.Connect(ctx, adminURI)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()

	var randomBytes [8]byte
	_, err = rand.Read(randomBytes[:])
	require.NoError(t, err)
	dbName := fmt.Sprintf("test_orch_%d", binary.BigEndian.Uint64(randomBytes[:]))

	if _, err := adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName)); err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42P04" {
			t.Fatalf("CREATE DATABASE: %v", err)
		}
	}

	t.Cleanup(func() {
		cleanupCtx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		c, cerr := pgx.Connect(cleanupCtx, adminURI)
		if cerr != nil {
			return
		}
		defer func() { _ = c.Close(cleanupCtx) }()
		_, _ = c.Exec(cleanupCtx,
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			dbName)
		_, _ = c.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	})

	return fmt.Sprintf("postgres://agentregistry:agentregistry@localhost:5432/%s?sslmode=disable", dbName)
}

// TestRunUp_FreshInstall: no public.schema_migrations, no legacy data
// — orchestrator applies the migration and produces a single row in
// agentregistry.schema_migrations.
func TestRunUp_FreshInstall(t *testing.T) {
	dsn := newDB(t)
	ctx := context.Background()

	require.NoError(t, orchestrator.RunUp(ctx, dsn, []orchestrator.Source{legacymigrate.OSSSource()}))

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var rows int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM agentregistry.schema_migrations").Scan(&rows))
	require.Equal(t, 1, rows, "schema_migrations should have one row after fresh install")

	// LegacyRun must not have fired: agentregistry.agents is empty
	// (no rows were copied from a non-existent v1alpha1.agents).
	var agentCount int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM agentregistry.agents").Scan(&agentCount))
	require.Equal(t, 0, agentCount)
}

// TestRunUp_Idempotent: a second RunUp against an up-to-date database
// returns nil and doesn't add migration rows or re-fire LegacyRun.
func TestRunUp_Idempotent(t *testing.T) {
	dsn := newDB(t)
	ctx := context.Background()
	src := legacymigrate.OSSSource()

	require.NoError(t, orchestrator.RunUp(ctx, dsn, []orchestrator.Source{src}))
	require.NoError(t, orchestrator.RunUp(ctx, dsn, []orchestrator.Source{src}))

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var rows int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM agentregistry.schema_migrations").Scan(&rows))
	require.Equal(t, 1, rows, "second RunUp must not add migration rows")
}

// TestRunUp_LegacyBridge: seed a pre-engine-swap production state and
// confirm LegacyRun copies data, rows land in agentregistry.*, the
// rename fires, and the v1alpha1.* tables retain the original rows.
func TestRunUp_LegacyBridge(t *testing.T) {
	dsn := newDB(t)
	ctx := context.Background()

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	seedLegacyState(t, ctx, db)

	require.NoError(t, orchestrator.RunUp(ctx, dsn, []orchestrator.Source{legacymigrate.OSSSource()}))

	// Public's schema_migrations was renamed to v0_legacy.
	var hasLegacyRename, hasOriginal bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT to_regclass('public.schema_migrations_v0_legacy') IS NOT NULL").Scan(&hasLegacyRename))
	require.True(t, hasLegacyRename, "public.schema_migrations_v0_legacy must exist post-bridge")
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT to_regclass('public.schema_migrations') IS NOT NULL").Scan(&hasOriginal))
	require.False(t, hasOriginal, "public.schema_migrations must be gone (renamed)")

	// LegacyRun copied each row from v1alpha1.* to agentregistry.*.
	// Some tables (runtimes) carry a default seed from 001, so an
	// exact-count match doesn't hold; instead assert agentregistry's
	// count is >= legacy and the named sample row is present.
	legacyRows := map[string]string{
		"agents":      "sample-agent",
		"mcp_servers": "sample-mcp",
		"skills":      "sample-skill",
		"prompts":     "sample-prompt",
		"runtimes":    "sample-rt",
		"deployments": "sample-dep",
	}
	for table, sampleName := range legacyRows {
		var newCount, oldCount int
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM agentregistry.%s", table)).Scan(&newCount))
		require.NoError(t, db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM v1alpha1.%s", table)).Scan(&oldCount))
		require.GreaterOrEqual(t, newCount, oldCount, "agentregistry.%s should have at least v1alpha1.%s's rows", table, table)

		var present bool
		require.NoError(t, db.QueryRowContext(ctx,
			fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM agentregistry.%s WHERE name = $1)", table), sampleName,
		).Scan(&present))
		require.True(t, present, "sample row %q must be present in agentregistry.%s after bridge", sampleName, table)
	}

	// Spot-check payload identity for one row: agents.uid / spec / content_hash.
	var newUID, oldUID string
	var newSpec, oldSpec []byte
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT uid::text, spec FROM agentregistry.agents WHERE name = 'sample-agent'").Scan(&newUID, &newSpec))
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT uid::text, spec FROM v1alpha1.agents WHERE name = 'sample-agent'").Scan(&oldUID, &oldSpec))
	require.Equal(t, oldUID, newUID, "uid must match byte-for-byte")
	require.Equal(t, string(oldSpec), string(newSpec), "spec JSON must match byte-for-byte")
}

// TestRunUp_LegacyBridgeIdempotent: invoke RunUp twice against a
// seeded-legacy DB; the second invocation is a no-op because the
// rename in RunUp #1 moved public.schema_migrations aside, closing
// the LegacyRun gate on subsequent invocations.
func TestRunUp_LegacyBridgeIdempotent(t *testing.T) {
	dsn := newDB(t)
	ctx := context.Background()

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	seedLegacyState(t, ctx, db)

	src := legacymigrate.OSSSource()
	require.NoError(t, orchestrator.RunUp(ctx, dsn, []orchestrator.Source{src}))
	require.NoError(t, orchestrator.RunUp(ctx, dsn, []orchestrator.Source{src}))

	// Counts unchanged after second RunUp.
	var rows int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM agentregistry.schema_migrations").Scan(&rows))
	require.Equal(t, 1, rows)
	var agents int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM agentregistry.agents").Scan(&agents))
	require.Equal(t, 1, agents)
}

// TestRunUp_LegacyBridgeAfterPartialRun simulates a partial prior
// invocation that committed Steps(1) (creating all destination tables
// AND incrementing agentregistry.schema_migrations) but died before
// LegacyRun fired. The bridge gate must still fire LegacyRun on the
// next invocation as long as public.schema_migrations remains intact.
// This is the failure mode the gate fix prevents.
func TestRunUp_LegacyBridgeAfterPartialRun(t *testing.T) {
	dsn := newDB(t)
	ctx := context.Background()

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	seedLegacyState(t, ctx, db)

	// Reach into the migrator and apply Steps(1) directly, without
	// the orchestrator's bridge logic. This leaves the system in the
	// exact post-Steps(1)-pre-LegacyRun state that would result from
	// a crash between those two points: agentregistry.* tables exist,
	// agentregistry.schema_migrations has the v1 row, public.schema_migrations
	// is still intact, but no data has been copied.
	mg, err := database.NewMigrator(ctx, dsn, v1alpha1store.MigrationFiles, v1alpha1store.MigrationsDir, database.MustNewSchema(database.OSSSchema))
	require.NoError(t, err)
	require.NoError(t, mg.Steps(1))
	_, dbErr := mg.Close()
	require.NoError(t, dbErr)

	require.NoError(t, orchestrator.RunUp(ctx, dsn, []orchestrator.Source{legacymigrate.OSSSource()}))

	// The bridge must have fired despite preStepsCount being 1,
	// because public.schema_migrations was still present.
	var agentCount int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM agentregistry.agents").Scan(&agentCount))
	require.GreaterOrEqual(t, agentCount, 1, "LegacyRun must fire when public.schema_migrations is present even if Steps(1) already committed")

	// public.schema_migrations should have been renamed after the bridge.
	var renameExists bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT to_regclass('public.schema_migrations_v0_legacy') IS NOT NULL").Scan(&renameExists))
	require.True(t, renameExists, "public.schema_migrations_v0_legacy must exist post-bridge")
}

// TestRunUp_MultiPodRace: launch 5 concurrent RunUp goroutines against
// the same database; the advisory lock serializes them and the final
// state is exactly one applied migration.
func TestRunUp_MultiPodRace(t *testing.T) {
	dsn := newDB(t)
	ctx := context.Background()
	src := legacymigrate.OSSSource()

	const n = 5
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			errCh <- orchestrator.RunUp(ctx, dsn, []orchestrator.Source{src})
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err, "concurrent RunUp should not error")
	}

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var rows int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM agentregistry.schema_migrations").Scan(&rows))
	require.Equal(t, 1, rows, "exactly one migration row regardless of concurrent runners")
}

// seedLegacyState plants a pre-engine-swap production state in the DB:
// the prior migrator's public.schema_migrations + v1alpha1.* tables
// with sample rows.
func seedLegacyState(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	stmts := []string{
		`CREATE TABLE public.schema_migrations (
			version INTEGER PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)`,
		`INSERT INTO public.schema_migrations(version, name) VALUES
			(201, '001_v1alpha1_schema'),
			(202, '002_enrichment_findings'),
			(203, '003_embeddings'),
			(204, '004_notify_payload_discrete'),
			(206, '006_enrichment_findings_tag'),
			(207, '007_drop_enrichment_findings'),
			(208, '008_drop_semantic_embeddings')`,
		`CREATE SCHEMA v1alpha1`,
		`CREATE TABLE v1alpha1.agents (
			namespace VARCHAR(255) NOT NULL, name VARCHAR(255) NOT NULL, tag VARCHAR(255) NOT NULL,
			uid uuid DEFAULT gen_random_uuid() NOT NULL, generation BIGINT DEFAULT 1 NOT NULL,
			labels jsonb DEFAULT '{}'::jsonb NOT NULL, annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
			spec jsonb NOT NULL, content_hash CHARACTER(64) NOT NULL,
			status jsonb DEFAULT '{}'::jsonb NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() NOT NULL,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() NOT NULL,
			deletion_timestamp TIMESTAMP WITH TIME ZONE,
			PRIMARY KEY (namespace, name, tag)
		)`,
		`CREATE TABLE v1alpha1.mcp_servers (LIKE v1alpha1.agents INCLUDING ALL)`,
		`CREATE TABLE v1alpha1.skills (LIKE v1alpha1.agents INCLUDING ALL)`,
		`CREATE TABLE v1alpha1.prompts (LIKE v1alpha1.agents INCLUDING ALL)`,
		`CREATE TABLE v1alpha1.runtimes (
			namespace VARCHAR(255) NOT NULL, name VARCHAR(255) NOT NULL,
			uid uuid DEFAULT gen_random_uuid() NOT NULL, generation BIGINT DEFAULT 1 NOT NULL,
			labels jsonb DEFAULT '{}'::jsonb NOT NULL, annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
			spec jsonb NOT NULL, status jsonb DEFAULT '{}'::jsonb NOT NULL,
			deletion_timestamp TIMESTAMP WITH TIME ZONE,
			finalizers jsonb DEFAULT '[]'::jsonb NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() NOT NULL,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() NOT NULL,
			PRIMARY KEY (namespace, name)
		)`,
		`CREATE TABLE v1alpha1.deployments (LIKE v1alpha1.runtimes INCLUDING ALL)`,
		`INSERT INTO v1alpha1.agents (namespace, name, tag, spec, content_hash) VALUES
			('default', 'sample-agent', 'v1', '{"k":"v"}'::jsonb, '0000000000000000000000000000000000000000000000000000000000000000')`,
		`INSERT INTO v1alpha1.mcp_servers (namespace, name, tag, spec, content_hash) VALUES
			('default', 'sample-mcp', 'v1', '{"k":"v"}'::jsonb, '0000000000000000000000000000000000000000000000000000000000000000')`,
		`INSERT INTO v1alpha1.skills (namespace, name, tag, spec, content_hash) VALUES
			('default', 'sample-skill', 'v1', '{"k":"v"}'::jsonb, '0000000000000000000000000000000000000000000000000000000000000000')`,
		`INSERT INTO v1alpha1.prompts (namespace, name, tag, spec, content_hash) VALUES
			('default', 'sample-prompt', 'v1', '{"k":"v"}'::jsonb, '0000000000000000000000000000000000000000000000000000000000000000')`,
		`INSERT INTO v1alpha1.runtimes (namespace, name, spec) VALUES
			('default', 'sample-rt', '{"k":"v"}'::jsonb)`,
		`INSERT INTO v1alpha1.deployments (namespace, name, spec) VALUES
			('default', 'sample-dep', '{"k":"v"}'::jsonb)`,
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("seed %q: %v", firstLine(q), err)
		}
	}
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}
	return s
}

// TestRunUp_RollsBackPriorSourcesOnLaterFailure exercises cross-source
// atomicity: an already-upgraded earlier source is reversed to its
// pre-run version when a later source fails, while its up-only floor is
// left intact. track_a is brought to v1, then a run that would take it
// to v2 fails on track_b — track_a must end back at v1 with 002's table
// dropped.
func TestRunUp_RollsBackPriorSourcesOnLaterFailure(t *testing.T) {
	dsn := newDB(t)
	ctx := context.Background()

	upOnlyDown := []byte("DO $$ BEGIN RAISE EXCEPTION 'up-only'; END $$;")
	a001 := fstest.MapFS{
		"m/001_init.up.sql":   {Data: []byte("CREATE TABLE IF NOT EXISTS a_t (id int);")},
		"m/001_init.down.sql": {Data: upOnlyDown},
	}
	aFull := fstest.MapFS{
		"m/001_init.up.sql":   {Data: []byte("CREATE TABLE IF NOT EXISTS a_t (id int);")},
		"m/001_init.down.sql": {Data: upOnlyDown},
		"m/002_add.up.sql":    {Data: []byte("CREATE TABLE IF NOT EXISTS a_t2 (id int);")},
		"m/002_add.down.sql":  {Data: []byte("DROP TABLE IF EXISTS a_t2;")},
	}
	bFail := fstest.MapFS{
		"m/001_init.up.sql":   {Data: []byte("SELECT 1/0;")}, // fails at apply time
		"m/001_init.down.sql": {Data: upOnlyDown},
	}

	srcA001 := orchestrator.Source{Name: "track_a", Schema: database.MustNewSchema("track_a"), Files: a001, Dir: "m"}
	srcAFull := orchestrator.Source{Name: "track_a", Schema: database.MustNewSchema("track_a"), Files: aFull, Dir: "m"}
	srcB := orchestrator.Source{Name: "track_b", Schema: database.MustNewSchema("track_b"), Files: bFail, Dir: "m"}

	// Bring track_a to v1 — an existing source that the next run upgrades.
	require.NoError(t, orchestrator.RunUp(ctx, dsn, []orchestrator.Source{srcA001}))

	// track_a v1->v2, then track_b fails: track_a must roll back to v1.
	err := orchestrator.RunUp(ctx, dsn, []orchestrator.Source{srcAFull, srcB})
	require.Error(t, err)
	require.Contains(t, err.Error(), "restored to their pre-run versions")

	db, e := sql.Open("pgx", dsn)
	require.NoError(t, e)
	defer func() { _ = db.Close() }()

	var version int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT version FROM track_a.schema_migrations LIMIT 1").Scan(&version))
	require.Equal(t, 1, version, "track_a must be rolled back to its pre-run version")

	var hasA2 bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT to_regclass('track_a.a_t2') IS NOT NULL").Scan(&hasA2))
	require.False(t, hasA2, "002's table must be dropped by the rollback down")

	var hasA1 bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT to_regclass('track_a.a_t') IS NOT NULL").Scan(&hasA1))
	require.True(t, hasA1, "the up-only 001 floor must remain (not reversed)")

	// The failing source (freshly-installed, floor failed) must be reset
	// so a re-run isn't blocked by a leftover dirty marker.
	var bRows int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT count(*) FROM track_b.schema_migrations").Scan(&bRows))
	require.Equal(t, 0, bRows, "freshly-installed failing source must be reset to nil (no dirty marker left)")
}

// TestRunUp_RestoresUpgradedFailingSourceToEntry covers the key
// intermediate-commit case: a source upgraded across MULTIPLE migrations
// where a later one fails. The migrations that committed before the
// failure must be reversed back to the entry version — not left applied,
// and not over-rolled past the entry floor.
func TestRunUp_RestoresUpgradedFailingSourceToEntry(t *testing.T) {
	dsn := newDB(t)
	ctx := context.Background()
	upOnlyDown := []byte("DO $$ BEGIN RAISE EXCEPTION 'up-only'; END $$;")

	v1 := fstest.MapFS{
		"m/001_init.up.sql":   {Data: []byte("CREATE TABLE IF NOT EXISTS c_t (id int);")},
		"m/001_init.down.sql": {Data: upOnlyDown},
	}
	// 002 commits, 003 fails: 002's table must be dropped, schema back at v1.
	v3Fail := fstest.MapFS{
		"m/001_init.up.sql":   {Data: []byte("CREATE TABLE IF NOT EXISTS c_t (id int);")},
		"m/001_init.down.sql": {Data: upOnlyDown},
		"m/002_add.up.sql":    {Data: []byte("CREATE TABLE IF NOT EXISTS c_t2 (id int);")},
		"m/002_add.down.sql":  {Data: []byte("DROP TABLE IF EXISTS c_t2;")},
		"m/003_bad.up.sql":    {Data: []byte("SELECT 1/0;")},
		// 003's down is replayed during the restore even though 003's up
		// never committed (go-migrate runs every down between cur and the
		// target), so it must be a harmless no-op.
		"m/003_bad.down.sql": {Data: []byte("SELECT 1;")},
	}

	require.NoError(t, orchestrator.RunUp(ctx, dsn,
		[]orchestrator.Source{{Name: "track_c", Schema: database.MustNewSchema("track_c"), Files: v1, Dir: "m"}}))

	err := orchestrator.RunUp(ctx, dsn,
		[]orchestrator.Source{{Name: "track_c", Schema: database.MustNewSchema("track_c"), Files: v3Fail, Dir: "m"}})
	require.Error(t, err)

	db, e := sql.Open("pgx", dsn)
	require.NoError(t, e)
	defer func() { _ = db.Close() }()

	var version int
	var dirty bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT version, dirty FROM track_c.schema_migrations LIMIT 1").Scan(&version, &dirty))
	require.Equal(t, 1, version, "track_c must be restored to its entry version")
	require.False(t, dirty, "dirty marker from the failed 003 must be cleared")

	var hasC2 bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT to_regclass('track_c.c_t2') IS NOT NULL").Scan(&hasC2))
	require.False(t, hasC2, "002's table (committed before the failure) must be reversed")

	var hasC1 bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT to_regclass('track_c.c_t') IS NOT NULL").Scan(&hasC1))
	require.True(t, hasC1, "the entry-version floor must remain")
}

// TestRunUp_RestoresFreshInstallWithIntermediateCommit covers a
// first-installed source whose floor and an intermediate migration commit
// before a later one fails. The marker must be reset to nil (so a re-run
// isn't blocked) and the idempotent leftover tables let the re-run
// converge. The source is fresh, so there is no prior-release version to
// return to — only the dirty marker must be cleared.
func TestRunUp_RestoresFreshInstallWithIntermediateCommit(t *testing.T) {
	dsn := newDB(t)
	ctx := context.Background()

	// track_e is installed fresh this run: 001 + 002 commit, 003 fails.
	files := fstest.MapFS{
		"m/001_init.up.sql":   {Data: []byte("CREATE TABLE IF NOT EXISTS e_t (id int);")},
		"m/001_init.down.sql": {Data: []byte("DO $$ BEGIN RAISE EXCEPTION 'up-only'; END $$;")},
		"m/002_add.up.sql":    {Data: []byte("CREATE TABLE IF NOT EXISTS e_t2 (id int);")},
		"m/002_add.down.sql":  {Data: []byte("DROP TABLE IF EXISTS e_t2;")},
		"m/003_bad.up.sql":    {Data: []byte("SELECT 1/0;")},
		// The restore clears the dirty marker at 003 then migrates down to
		// nil, so go-migrate replays 003's down even though 003's up never
		// committed — a harmless no-op here, which is why it's `SELECT 1;`.
		"m/003_bad.down.sql": {Data: []byte("SELECT 1;")},
	}
	src := orchestrator.Source{Name: "track_e", Schema: database.MustNewSchema("track_e"), Files: files, Dir: "m"}

	err := orchestrator.RunUp(ctx, dsn, []orchestrator.Source{src})
	require.Error(t, err)

	db, e := sql.Open("pgx", dsn)
	require.NoError(t, e)
	defer func() { _ = db.Close() }()

	// Marker reset to nil — no dirty row blocks a re-run.
	var rows int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT count(*) FROM track_e.schema_migrations").Scan(&rows))
	require.Equal(t, 0, rows, "freshly-installed failing source must be reset to nil")

	// The committed-before-failure tables are left in place (idempotent on re-run).
	for _, tbl := range []string{"track_e.e_t", "track_e.e_t2"} {
		var present bool
		require.NoError(t, db.QueryRowContext(ctx,
			"SELECT to_regclass($1) IS NOT NULL", tbl).Scan(&present))
		require.True(t, present, "%s committed before the failure and is left in place", tbl)
	}

	// A re-run with the 003 failure removed converges cleanly from nil.
	fixed := fstest.MapFS{
		"m/001_init.up.sql":   {Data: []byte("CREATE TABLE IF NOT EXISTS e_t (id int);")},
		"m/001_init.down.sql": {Data: []byte("DO $$ BEGIN RAISE EXCEPTION 'up-only'; END $$;")},
		"m/002_add.up.sql":    {Data: []byte("CREATE TABLE IF NOT EXISTS e_t2 (id int);")},
		"m/002_add.down.sql":  {Data: []byte("DROP TABLE IF EXISTS e_t2;")},
		"m/003_ok.up.sql":     {Data: []byte("CREATE TABLE IF NOT EXISTS e_t3 (id int);")},
		"m/003_ok.down.sql":   {Data: []byte("DROP TABLE IF EXISTS e_t3;")},
	}
	require.NoError(t, orchestrator.RunUp(ctx, dsn,
		[]orchestrator.Source{{Name: "track_e", Schema: database.MustNewSchema("track_e"), Files: fixed, Dir: "m"}}))

	var version int
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT version FROM track_e.schema_migrations LIMIT 1").Scan(&version))
	require.Equal(t, 3, version, "re-run converges from nil to the latest version")
}

// TestRunUp_RefusesSourceAlreadyDirty asserts a source found dirty at the
// start of a run is not migrated; RunUp surfaces it for operator `force`
// rather than guessing a restore target.
func TestRunUp_RefusesSourceAlreadyDirty(t *testing.T) {
	dsn := newDB(t)
	ctx := context.Background()

	v1 := fstest.MapFS{
		"m/001_init.up.sql":   {Data: []byte("CREATE TABLE IF NOT EXISTS d_t (id int);")},
		"m/001_init.down.sql": {Data: []byte("DROP TABLE IF EXISTS d_t;")},
	}
	src := orchestrator.Source{Name: "track_d", Schema: database.MustNewSchema("track_d"), Files: v1, Dir: "m"}
	require.NoError(t, orchestrator.RunUp(ctx, dsn, []orchestrator.Source{src}))

	// Manually mark the source dirty, simulating a prior crashed run.
	db, e := sql.Open("pgx", dsn)
	require.NoError(t, e)
	defer func() { _ = db.Close() }()
	_, err := db.ExecContext(ctx, "UPDATE track_d.schema_migrations SET dirty = true")
	require.NoError(t, err)

	err = orchestrator.RunUp(ctx, dsn, []orchestrator.Source{src})
	require.Error(t, err)
	require.Contains(t, err.Error(), "entered dirty")
	require.Contains(t, err.Error(), "force")
}
