//go:build integration

package database

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

//go:embed testdata/integration_full/*.sql
var integrationFullFiles embed.FS

//go:embed testdata/integration_sparse/*.sql
var integrationSparseFiles embed.FS

//go:embed testdata/integration_extension/*.sql
var integrationExtensionFiles embed.FS

const integrationAdminURI = "postgres://agentregistry:agentregistry@localhost:5432/postgres?sslmode=disable"

// newIntegrationConn returns a pgx.Conn against a fresh empty database
// and a context wired to test cleanup. Skips when localhost Postgres
// is unavailable so unit-only runs don't fail.
func newIntegrationConn(t *testing.T) (*pgx.Conn, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminConn, err := pgx.Connect(ctx, integrationAdminURI)
	if err != nil {
		t.Skipf("PostgreSQL not available at localhost:5432: %v", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()

	var randomBytes [8]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	dbName := fmt.Sprintf("test_migrator_%d", binary.BigEndian.Uint64(randomBytes[:]))

	if _, err := adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName)); err != nil {
		t.Fatalf("CREATE DATABASE %s: %v", dbName, err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cleanupConn, err := pgx.Connect(cleanupCtx, integrationAdminURI)
		if err != nil {
			return
		}
		defer func() { _ = cleanupConn.Close(cleanupCtx) }()
		_, _ = cleanupConn.Exec(cleanupCtx,
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			dbName)
		_, _ = cleanupConn.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	})

	testURI := fmt.Sprintf("postgres://agentregistry:agentregistry@localhost:5432/%s?sslmode=disable", dbName)
	conn, err := pgx.Connect(ctx, testURI)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn, ctx
}

func fullConfig() MigratorConfig {
	return MigratorConfig{
		MigrationFiles: integrationFullFiles,
		MigrationDir:   "testdata/integration_full",
		EnsureTable:    true,
	}
}

func sparseConfig() MigratorConfig {
	return MigratorConfig{
		MigrationFiles: integrationSparseFiles,
		MigrationDir:   "testdata/integration_sparse",
		EnsureTable:    true,
	}
}

// TestIntegrationStatus_FreshDBEnsureTable verifies the cycle-1 fix:
// Status against a clean DB no longer crashes with "relation does not
// exist". EnsureTable=true triggers a self-ensure inside Status.
func TestIntegrationStatus_FreshDBEnsureTable(t *testing.T) {
	conn, ctx := newIntegrationConn(t)
	m := NewMigrator(conn, fullConfig())

	applied, pending, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status on fresh DB: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("applied = %v; want empty", applied)
	}
	if len(pending) != 3 {
		t.Errorf("pending = %d; want 3 (alpha/beta/gamma)", len(pending))
	}

	v, err := m.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("CurrentVersion on fresh DB: %v", err)
	}
	if v != 0 {
		t.Errorf("CurrentVersion = %d; want 0", v)
	}
}

// TestIntegrationStatus_FreshDBEnsureTableFalse verifies the cycle-6
// fix: a non-owning source (EnsureTable=false) reading before any
// owning source has run treats the missing table as "no rows applied"
// via the pgcode 42P01 path.
func TestIntegrationStatus_FreshDBEnsureTableFalse(t *testing.T) {
	conn, ctx := newIntegrationConn(t)
	cfg := fullConfig()
	cfg.EnsureTable = false
	m := NewMigrator(conn, cfg)

	applied, pending, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status with EnsureTable=false on fresh DB: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("applied = %v; want empty (table doesn't exist)", applied)
	}
	if len(pending) != 3 {
		t.Errorf("pending = %d; want 3", len(pending))
	}

	v, err := m.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("CurrentVersion with EnsureTable=false on fresh DB: %v", err)
	}
	if v != 0 {
		t.Errorf("CurrentVersion = %d; want 0", v)
	}
}

// TestIntegrationMigrateAndStatus runs the full forward path and
// checks the resulting Status reflects all 3 migrations applied.
func TestIntegrationMigrateAndStatus(t *testing.T) {
	conn, ctx := newIntegrationConn(t)
	m := NewMigrator(conn, fullConfig())

	if err := m.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	applied, pending, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(applied) != 3 || len(pending) != 0 {
		t.Errorf("after Migrate: applied=%d pending=%d; want 3/0", len(applied), len(pending))
	}

	for _, table := range []string{"alpha", "beta", "gamma"} {
		assertTableExists(t, conn, ctx, table, true)
	}
}

// TestIntegrationDown_ExecutesDownSQL applies all 3 migrations then
// rolls back 1; verifies the gamma table is dropped (down SQL ran) and
// the schema_migrations row is gone (DELETE in the rollback tx).
func TestIntegrationDown_ExecutesDownSQL(t *testing.T) {
	conn, ctx := newIntegrationConn(t)
	m := NewMigrator(conn, fullConfig())

	if err := m.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := m.Down(ctx, 1); err != nil {
		t.Fatalf("Down(1): %v", err)
	}

	assertTableExists(t, conn, ctx, "gamma", false)
	assertTableExists(t, conn, ctx, "beta", true)

	v, err := m.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if v != 2 {
		t.Errorf("CurrentVersion after Down(1) = %d; want 2", v)
	}
}

// TestIntegrationDown_ErrNotReversible verifies that crossing an
// up-only migration during Down returns ErrNotReversible (wrapped with
// the migration name/version) and leaves the in-source state
// reflecting the partial rollback up to that point.
func TestIntegrationDown_ErrNotReversible(t *testing.T) {
	conn, ctx := newIntegrationConn(t)
	// down_fixture: 001 is up-only, 002 has .down.sql sibling.
	m := NewMigrator(conn, MigratorConfig{
		MigrationFiles: downFixtureFiles,
		MigrationDir:   "testdata/down_fixture",
		EnsureTable:    true,
	})

	if err := m.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Down(2): rolls back v2 (with .down.sql), then errors trying to
	// roll back v1 (no .down.sql).
	err := m.Down(ctx, 2)
	if err == nil {
		t.Fatalf("Down(2) across up-only migration: want error, got nil")
	}
	if !errors.Is(err, ErrNotReversible) {
		t.Errorf("Down error = %v; want errors.Is ErrNotReversible", err)
	}

	// v2's down SQL ran (table dropped + row deleted) before the error,
	// but v1 is still applied.
	assertTableExists(t, conn, ctx, "second", false)
	assertTableExists(t, conn, ctx, "first", true)
	v, err := m.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if v != 1 {
		t.Errorf("CurrentVersion after partial Down = %d; want 1", v)
	}
}

// TestIntegrationMigrateTo_ForwardAndBackward exercises the routing
// in both directions and confirms intermediate states.
func TestIntegrationMigrateTo_ForwardAndBackward(t *testing.T) {
	conn, ctx := newIntegrationConn(t)
	m := NewMigrator(conn, fullConfig())

	// Forward to v2 (apply 1, 2; not 3).
	if err := m.MigrateTo(ctx, 2); err != nil {
		t.Fatalf("MigrateTo(2): %v", err)
	}
	v, _ := m.CurrentVersion(ctx)
	if v != 2 {
		t.Errorf("after MigrateTo(2): CurrentVersion = %d; want 2", v)
	}
	assertTableExists(t, conn, ctx, "beta", true)
	assertTableExists(t, conn, ctx, "gamma", false)

	// Forward to v3.
	if err := m.MigrateTo(ctx, 3); err != nil {
		t.Fatalf("MigrateTo(3): %v", err)
	}
	v, _ = m.CurrentVersion(ctx)
	if v != 3 {
		t.Errorf("after MigrateTo(3): CurrentVersion = %d; want 3", v)
	}
	assertTableExists(t, conn, ctx, "gamma", true)

	// Backward to v1 (chains Down).
	if err := m.MigrateTo(ctx, 1); err != nil {
		t.Fatalf("MigrateTo(1): %v", err)
	}
	v, _ = m.CurrentVersion(ctx)
	if v != 1 {
		t.Errorf("after MigrateTo(1): CurrentVersion = %d; want 1", v)
	}
	assertTableExists(t, conn, ctx, "alpha", true)
	assertTableExists(t, conn, ctx, "beta", false)
	assertTableExists(t, conn, ctx, "gamma", false)
}

// TestIntegrationMigrateTo_UnknownVersionInRange verifies the cycle-4
// fix: MigrateTo to an in-range version that doesn't correspond to a
// migration file errors with ErrOutOfRange instead of silently no-
// opping at a different state.
func TestIntegrationMigrateTo_UnknownVersionInRange(t *testing.T) {
	conn, ctx := newIntegrationConn(t)
	// sparse fixture has files at v1 and v3 only; v2 is in [1, 3] but
	// not a real migration version.
	m := NewMigrator(conn, sparseConfig())

	err := m.MigrateTo(ctx, 2)
	if err == nil {
		t.Fatalf("MigrateTo(2) against sparse {1, 3}: want error, got nil")
	}
	if !errors.Is(err, ErrOutOfRange) {
		t.Errorf("error = %v; want errors.Is ErrOutOfRange", err)
	}
}

// TestIntegrationMigrateTo_OutOfRange verifies the standard out-of-
// range error path.
func TestIntegrationMigrateTo_OutOfRange(t *testing.T) {
	conn, ctx := newIntegrationConn(t)
	m := NewMigrator(conn, fullConfig())

	err := m.MigrateTo(ctx, 999)
	if err == nil {
		t.Fatalf("MigrateTo(999): want error, got nil")
	}
	if !errors.Is(err, ErrOutOfRange) {
		t.Errorf("error = %v; want errors.Is ErrOutOfRange", err)
	}
}

// TestIntegrationForce_Idempotent verifies that running Force twice
// against the same version doesn't error (ON CONFLICT DO NOTHING).
// Also verifies that Force does NOT run the migration SQL.
func TestIntegrationForce_Idempotent(t *testing.T) {
	conn, ctx := newIntegrationConn(t)
	m := NewMigrator(conn, fullConfig())

	if err := m.Force(ctx, 2); err != nil {
		t.Fatalf("Force(2): %v", err)
	}
	if err := m.Force(ctx, 2); err != nil {
		t.Fatalf("Force(2) second call: %v", err)
	}

	// The beta table should NOT exist — Force only writes the
	// schema_migrations row, it doesn't run the migration SQL.
	assertTableExists(t, conn, ctx, "beta", false)

	v, err := m.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if v != 2 {
		t.Errorf("CurrentVersion after Force(2) = %d; want 2", v)
	}
}

// TestIntegrationEmptySourceRange verifies the cycle-4 sentinel: a
// source where every migration is Skip-filtered (the realistic shape
// of an "empty" source — feature-flag-gated migrations all off)
// reports CurrentVersion=0 and Status applied=[] even when
// schema_migrations contains rows from another source. The
// `BETWEEN low AND low-1` query returns no rows.
func TestIntegrationEmptySourceRange(t *testing.T) {
	conn, ctx := newIntegrationConn(t)
	// First apply a populated source at offset 0.
	populated := NewMigrator(conn, fullConfig())
	if err := populated.Migrate(ctx); err != nil {
		t.Fatalf("populated Migrate: %v", err)
	}

	// Empty-via-Skip source at offset 500. EnsureTable=false because
	// the table was already created by `populated`.
	empty := NewMigrator(conn, MigratorConfig{
		MigrationFiles: integrationExtensionFiles,
		MigrationDir:   "testdata/integration_extension",
		VersionOffset:  500,
		EnsureTable:    false,
		Skip:           func(int) bool { return true },
	})
	v, err := empty.CurrentVersion(ctx)
	if err != nil {
		t.Fatalf("empty source CurrentVersion: %v", err)
	}
	if v != 0 {
		t.Errorf("empty source CurrentVersion = %d; want 0", v)
	}
	applied, _, err := empty.Status(ctx)
	if err != nil {
		t.Fatalf("empty source Status: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("empty source applied = %v; want empty", applied)
	}
}

// TestIntegrationCrossSource verifies two migrators with disjoint
// VersionOffsets share schema_migrations cleanly: each reports only
// its own versions in Status / CurrentVersion.
func TestIntegrationCrossSource(t *testing.T) {
	conn, ctx := newIntegrationConn(t)

	base := NewMigrator(conn, fullConfig())
	if err := base.Migrate(ctx); err != nil {
		t.Fatalf("base Migrate: %v", err)
	}

	ext := NewMigrator(conn, MigratorConfig{
		MigrationFiles: integrationExtensionFiles,
		MigrationDir:   "testdata/integration_extension",
		VersionOffset:  500,
		EnsureTable:    false,
	})
	if err := ext.Migrate(ctx); err != nil {
		t.Fatalf("ext Migrate: %v", err)
	}

	// Each source sees only its own versions.
	baseApplied, _, err := base.Status(ctx)
	if err != nil {
		t.Fatalf("base Status: %v", err)
	}
	if len(baseApplied) != 3 {
		t.Errorf("base applied = %v; want 3 versions", baseApplied)
	}
	for _, v := range baseApplied {
		if v < 1 || v > 3 {
			t.Errorf("base applied v=%d outside [1,3]", v)
		}
	}

	extApplied, _, err := ext.Status(ctx)
	if err != nil {
		t.Fatalf("ext Status: %v", err)
	}
	if len(extApplied) != 1 {
		t.Errorf("ext applied = %v; want 1 version (v501)", extApplied)
	}
	if len(extApplied) == 1 && extApplied[0] != 501 {
		t.Errorf("ext applied[0] = %d; want 501", extApplied[0])
	}

	assertTableExists(t, conn, ctx, "extension_one", true)
}

// assertTableExists fails the test if the table's existence doesn't
// match `want`.
func assertTableExists(t *testing.T, conn *pgx.Conn, ctx context.Context, table string, want bool) {
	t.Helper()
	var exists bool
	err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = $1)",
		table).Scan(&exists)
	if err != nil {
		t.Fatalf("checking table %q: %v", table, err)
	}
	if exists != want {
		t.Errorf("table %q exists=%v; want %v", table, exists, want)
	}
}
