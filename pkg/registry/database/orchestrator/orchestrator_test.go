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
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/legacymigrate"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/orchestrator"
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

// TestRunUp_LegacyBridge: seed pre-#503 production state and confirm
// LegacyRun copies data, rows land in agentregistry.*, the rename
// fires, and the v1alpha1.* tables retain the original rows.
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
// seeded-legacy DB; the second invocation is a no-op (LegacyRun
// doesn't fire because preStepsCount is now 1, not 0).
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

// seedLegacyState plants pre-PR-#503 production state in the DB:
// legacy public.schema_migrations + v1alpha1.* tables with sample rows.
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

