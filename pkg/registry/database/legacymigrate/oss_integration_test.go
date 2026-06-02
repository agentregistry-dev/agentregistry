//go:build integration

package legacymigrate_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/legacymigrate"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

const ossTestAdminURI = "postgres://agentregistry:agentregistry@localhost:5432/postgres?sslmode=disable"

// freshDB creates a fresh per-test database and returns its DSN. Skips when
// PostgreSQL is not reachable at the canonical dev port.
func freshDB(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminConn, err := pgx.Connect(ctx, ossTestAdminURI)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()

	var randomBytes [8]byte
	_, err = rand.Read(randomBytes[:])
	require.NoError(t, err)
	dbName := fmt.Sprintf("test_ossbridge_%d", binary.BigEndian.Uint64(randomBytes[:]))

	_, err = adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
	require.NoError(t, err)

	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		c, cerr := pgx.Connect(cctx, ossTestAdminURI)
		if cerr != nil {
			return
		}
		defer func() { _ = c.Close(cctx) }()
		_, _ = c.Exec(cctx,
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			dbName)
		_, _ = c.Exec(cctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	})

	return fmt.Sprintf("postgres://agentregistry:agentregistry@localhost:5432/%s?sslmode=disable", dbName)
}

// applyOSSSchema brings the destination `agentregistry` schema up to 001 so
// RunOSS has somewhere to copy into.
func applyOSSSchema(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	mg, err := database.NewMigrator(ctx, dsn, v1alpha1store.MigrationFiles, v1alpha1store.MigrationsDir,
		database.MustNewSchema(database.OSSSchema))
	require.NoError(t, err)
	defer func() { _, _ = mg.Close() }()
	if err := mg.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("apply OSS migrations: %v", err)
	}
}

func openDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedLegacyOSSBookkeeping creates public.schema_migrations recording the OSS
// track at a specific (un-offset) migration version, for exercising RunOSS's
// version floor. OSS rows carry the +200 offset.
func seedLegacyOSSBookkeeping(t *testing.T, ctx context.Context, db *sql.DB, ossVersion int) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS public.schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO public.schema_migrations (version, name) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		200+ossVersion, fmt.Sprintf("%03d_oss", ossVersion))
	require.NoError(t, err)
}

// seedLegacyOSSAgents creates v1alpha1.agents (the bridge's existence canary)
// with one row, so a database that passes the floor proceeds to a real copy.
func seedLegacyOSSAgents(t *testing.T, ctx context.Context, db *sql.DB, name string) {
	t.Helper()
	_, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS v1alpha1`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS v1alpha1.agents (
			namespace          VARCHAR(255)  NOT NULL,
			name               VARCHAR(255)  NOT NULL,
			tag                VARCHAR(255)  NOT NULL,
			uid                UUID          NOT NULL DEFAULT gen_random_uuid(),
			generation         BIGINT        NOT NULL DEFAULT 1,
			labels             JSONB         NOT NULL DEFAULT '{}'::jsonb,
			annotations        JSONB         NOT NULL DEFAULT '{}'::jsonb,
			spec               JSONB         NOT NULL,
			content_hash       CHARACTER(64) NOT NULL,
			status             JSONB         NOT NULL DEFAULT '{}'::jsonb,
			created_at         TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
			updated_at         TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
			deletion_timestamp TIMESTAMPTZ,
			PRIMARY KEY (namespace, name, tag)
		)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO v1alpha1.agents (namespace, name, tag, spec, content_hash) VALUES ($1,$2,$3,$4::jsonb,$5) ON CONFLICT DO NOTHING`,
		"default", name, "v1", `{"k":"v"}`,
		"0000000000000000000000000000000000000000000000000000000000000000")
	require.NoError(t, err)
}

// TestRunOSS_RejectsBelowVersionFloor asserts the bridge refuses to copy when
// the legacy OSS track stopped short of the last pre-redesign release,
// surfaces an actionable error, and touches no destination data.
func TestRunOSS_RejectsBelowVersionFloor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := freshDB(t)
	applyOSSSchema(t, ctx, dsn)

	db := openDB(t, dsn)
	// OSS track recorded at v5 (offset version 205) — below the v8 floor.
	seedLegacyOSSBookkeeping(t, ctx, db, 5)
	seedLegacyOSSAgents(t, ctx, db, "agent-below-floor")

	err := legacymigrate.RunOSS(ctx, db, database.MustNewSchema(database.OSSSchema))
	require.Error(t, err)
	require.Contains(t, err.Error(), "migration v8",
		"error must name the last pre-redesign migration the operator must reach")

	// The upgrade must abort before touching data.
	var count int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT count(*) FROM agentregistry.agents`).Scan(&count))
	require.Equal(t, 0, count, "no data may be copied when below the version floor")
}

// TestRunOSS_AtVersionFloorProceeds asserts a database exactly at the last
// pre-redesign release is bridged normally.
func TestRunOSS_AtVersionFloorProceeds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := freshDB(t)
	applyOSSSchema(t, ctx, dsn)

	db := openDB(t, dsn)
	seedLegacyOSSBookkeeping(t, ctx, db, 8)
	seedLegacyOSSAgents(t, ctx, db, "agent-at-floor")

	require.NoError(t, legacymigrate.RunOSS(ctx, db, database.MustNewSchema(database.OSSSchema)))

	var got string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT name FROM agentregistry.agents WHERE name = $1`, "agent-at-floor").Scan(&got))
	require.Equal(t, "agent-at-floor", got)
}
