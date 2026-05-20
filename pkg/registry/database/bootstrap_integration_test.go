//go:build integration

package database_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/migratetest"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// newBootstrapTestDB creates a fresh empty database for the test and
// returns its DSN. Cleanup runs on t.Cleanup. Skips when Postgres is
// not reachable at the canonical dev port.
func newBootstrapTestDB(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminURI := "postgres://agentregistry:agentregistry@localhost:5432/postgres?sslmode=disable"
	adminConn, err := pgx.Connect(ctx, adminURI)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()

	var rnd [8]byte
	_, err = rand.Read(rnd[:])
	require.NoError(t, err)
	dbName := fmt.Sprintf("test_bootstrap_%d", binary.BigEndian.Uint64(rnd[:]))

	_, err = adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, err := pgx.Connect(cleanupCtx, adminURI)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(cleanupCtx) }()
		_, _ = conn.Exec(cleanupCtx, fmt.Sprintf(
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()",
			dbName,
		))
		_, _ = conn.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	})

	return fmt.Sprintf("postgres://agentregistry:agentregistry@localhost:5432/%s?sslmode=disable", dbName)
}

// TestIntegration_BootstrapPreservesLegacyOSSRows is the merge gate
// per docs/design/migration-engine-evaluation.md. It seeds a realistic
// pre-engine-swap state (legacy schema_migrations with the custom
// migrator's columns + offset, plus a populated v1alpha1.agents row)
// and asserts that bootstrap + go-migrate Up:
//
//   - preserves the legacy table verbatim as schema_migrations_v0_legacy
//   - writes a single-row schema_migrations in go-migrate shape whose
//     version is the highest legacy OSS version (offset stripped)
//     that has a matching .up.sql in the current embed
//   - leaves v1alpha1 data untouched
//   - no-ops on a second invocation
//   - silently drops orphan rows (no matching .up.sql in the embed)
func TestIntegration_BootstrapPreservesLegacyOSSRows(t *testing.T) {
	dsn := newBootstrapTestDB(t)
	ctx := context.Background()

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Seed: simulate a pre-PR-503 OSS deployment.
	// Step 1 — create the v1alpha1 schema and one v1alpha1 table so
	// the test can prove data isn't disturbed.
	_, err = db.ExecContext(ctx, `CREATE SCHEMA v1alpha1`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE v1alpha1.agents (
		    namespace VARCHAR(255) NOT NULL,
		    name      VARCHAR(255) NOT NULL,
		    tag       VARCHAR(255) NOT NULL DEFAULT 'latest',
		    payload   JSONB        NOT NULL DEFAULT '{}',
		    PRIMARY KEY (namespace, name, tag)
		)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO v1alpha1.agents (namespace, name, tag, payload)
		VALUES ('default', 'preserved-agent', 'v1', '{"flag": true}')`)
	require.NoError(t, err)

	// Step 2 — seed legacy schema_migrations with the canonical OSS
	// fixture (7 migrations under +200 offset + 1 orphan row to
	// assert orphan handling).
	migratetest.SeedLegacySchemaMigrations(t, db, migratetest.OSSLegacyRows())

	// Run the bootstrap + go-migrate Up via the OSS factory.
	mg, err := v1alpha1store.NewOSSMigrator(ctx, dsn)
	require.NoError(t, err)
	_, err = database.RunUpWithRecovery(mg, "oss")
	require.NoError(t, err)
	srcErr, dbErr := mg.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	// Assert 1: schema_migrations_v0_legacy retains the original 8
	// rows with name/applied_at intact.
	require.Equal(t, 8, migratetest.LegacyRowCount(t, db), "legacy table must retain every seeded row")
	require.Equal(t, "001_v1alpha1_schema", migratetest.LegacyRowName(t, db, 201))

	// Assert 2: new schema_migrations has go-migrate shape with a
	// single row at the highest carryable version. go-migrate keeps
	// only one row representing the current schema state; we hand it
	// the highest legacy OSS version (208) stripped of the +200
	// offset (8). The orphan v205 is dropped because no .up.sql for
	// version 5 exists in the embed.
	bridged := migratetest.BridgedRow(t, db, "schema_migrations")
	require.Equal(t, 8, bridged.Version, "highest legacy OSS row (208) bridged to v8")
	require.False(t, bridged.Dirty, "bridged row must be clean")
	require.Equal(t, 1, bridged.Rows, "go-migrate stores only the current version as a single row")

	// Assert 3: v1alpha1.agents row survives byte-for-byte.
	var ns, name, tag, payload string
	err = db.QueryRowContext(ctx,
		`SELECT namespace, name, tag, payload::text FROM v1alpha1.agents`).Scan(&ns, &name, &tag, &payload)
	require.NoError(t, err)
	require.Equal(t, "default", ns)
	require.Equal(t, "preserved-agent", name)
	require.Equal(t, "v1", tag)
	require.JSONEq(t, `{"flag": true}`, payload)

	// Assert 4: second run is a no-op. Re-construct the migrator and
	// call Up again; legacy table must still exist with the same row
	// count, no new schema_migrations_v0_legacy_* duplicate, no
	// duplicated rows in schema_migrations.
	mg2, err := v1alpha1store.NewOSSMigrator(ctx, dsn)
	require.NoError(t, err)
	_, err = database.RunUpWithRecovery(mg2, "oss")
	require.NoError(t, err)
	srcErr, dbErr = mg2.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	require.Equal(t, 8, migratetest.LegacyRowCount(t, db), "legacy table unchanged on re-run")
	require.Equal(t, 1, migratetest.BridgedRow(t, db, "schema_migrations").Rows, "go-migrate row count unchanged on re-run")

	// Assert no extra "legacy" copies were created (a second bridge
	// pass would have errored on RENAME, so this is also implicit).
	var extras int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name LIKE 'schema_migrations_v0_legacy%'`).Scan(&extras)
	require.NoError(t, err)
	require.Equal(t, 1, extras, "exactly one schema_migrations_v0_legacy table")
}

// TestIntegration_BootstrapNoOpOnFreshDatabase asserts the bootstrap
// is a true no-op on a database that's never seen the custom
// migrator. Together with the bridge test, this is the contract:
// fresh installs work; legacy installs upgrade cleanly.
func TestIntegration_BootstrapNoOpOnFreshDatabase(t *testing.T) {
	dsn := newBootstrapTestDB(t)
	ctx := context.Background()

	mg, err := v1alpha1store.NewOSSMigrator(ctx, dsn)
	require.NoError(t, err)
	_, err = database.RunUpWithRecovery(mg, "oss")
	require.NoError(t, err)
	srcErr, dbErr := mg.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Legacy table should never exist.
	var legacyExists bool
	err = db.QueryRowContext(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM information_schema.tables
		    WHERE table_schema = 'public' AND table_name = 'schema_migrations_v0_legacy'
		)`).Scan(&legacyExists)
	require.NoError(t, err)
	require.False(t, legacyExists, "fresh install must not create the legacy table")

	// schema_migrations holds a single row at the highest applied
	// version (8 — the last of 001/002/003/004/006/007/008).
	var version int
	var dirty bool
	err = db.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty)
	require.NoError(t, err)
	require.Equal(t, 8, version)
	require.False(t, dirty)
}
