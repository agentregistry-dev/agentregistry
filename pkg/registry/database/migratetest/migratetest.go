// Package migratetest exposes test-only helpers for seeding a
// pre-engine-swap "legacy" schema_migrations table. The helpers
// mirror the input shape that pkg/registry/database/bootstrap.go's
// BootstrapLegacyTrack expects (version INTEGER PK, name VARCHAR,
// applied_at TIMESTAMP WITH TIME ZONE) so integration tests can
// stage a realistic upgrade scenario without re-deriving the
// table's column shape.
//
// The OSS data-preservation integration test uses these helpers;
// downstream extensions writing their own data-preservation tests
// for the same bootstrap should import this package rather than
// duplicating the seeding SQL — that way any future change to the
// expected legacy input shape is a single-file edit.
package migratetest

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// LegacyRow models one row of the pre-engine-swap custom-migrator
// schema_migrations table. Version is the on-disk version under the
// old +offset scheme (e.g. 201 for OSS migration 001). Name is the
// migration filename (e.g. "001_v1alpha1_schema"). AppliedAt is the
// historical timestamp — left at the zero value to default to a
// fixed test time so the helper produces deterministic rows.
type LegacyRow struct {
	Version   int
	Name      string
	AppliedAt time.Time
}

// defaultAppliedAt is the timestamp used when LegacyRow.AppliedAt is
// the zero value. Fixed so tests asserting on bookkeeping
// (e.g. that the renamed legacy table retains its applied_at column
// byte-identically) get a stable value.
var defaultAppliedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// SeedLegacySchemaMigrations creates `public.schema_migrations` in
// the pre-bridge custom-migrator shape and inserts the supplied
// rows. The table is created without IF NOT EXISTS — if a fixture
// or earlier setup already created one, the test surfaces the
// duplicate as a hard failure rather than silently appending rows.
//
// Intended for integration tests that need a realistic
// pre-engine-swap database state before exercising
// BootstrapLegacyTrack / BootstrapLegacyOSSMigrations.
func SeedLegacySchemaMigrations(t *testing.T, db *sql.DB, rows []LegacyRow) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := db.ExecContext(ctx, `
		CREATE TABLE schema_migrations (
		    version    INTEGER                  PRIMARY KEY,
		    name       VARCHAR(255)             NOT NULL,
		    applied_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)`)
	require.NoErrorf(t, err, "create legacy schema_migrations")

	for _, row := range rows {
		appliedAt := row.AppliedAt
		if appliedAt.IsZero() {
			appliedAt = defaultAppliedAt
		}
		_, err = db.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, name, applied_at) VALUES ($1, $2, $3)`,
			row.Version, row.Name, appliedAt)
		require.NoErrorf(t, err, "insert legacy row v%d (%s)", row.Version, row.Name)
	}
}

// OSSLegacyRows returns the canonical OSS legacy fixture: the seven
// shipped migrations under the +200 offset, plus one orphan row
// (v205 / "005_phantom_orphan") so consumers can assert that
// orphan-row handling drops rows whose stripped version has no
// matching .up.sql.
//
// Downstream tests typically want a different fixture — this is the
// OSS-side default, not a general primitive.
func OSSLegacyRows() []LegacyRow {
	return []LegacyRow{
		{Version: 201, Name: "001_v1alpha1_schema"},
		{Version: 202, Name: "002_enrichment_findings"},
		{Version: 203, Name: "003_embeddings"},
		{Version: 204, Name: "004_notify_payload_discrete"},
		{Version: 205, Name: "005_phantom_orphan"}, // no matching .up.sql in current embed
		{Version: 206, Name: "006_enrichment_findings_tag"},
		{Version: 207, Name: "007_drop_enrichment_findings"},
		{Version: 208, Name: "008_drop_semantic_embeddings"},
	}
}

// LegacyRowCount returns the number of legacy rows currently in
// `public.schema_migrations_v0_legacy`. Helper for assertions that
// the bootstrap preserved the original row set verbatim under the
// renamed audit table.
func LegacyRowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var n int
	err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations_v0_legacy`).Scan(&n)
	require.NoErrorf(t, err, "count rows in schema_migrations_v0_legacy")
	return n
}

// LegacyRowName returns the `name` column for a given pre-bridge
// version in `schema_migrations_v0_legacy`. Helper for assertions
// that legacy bookkeeping (the historical filename + applied_at)
// survives the rename byte-identically.
func LegacyRowName(t *testing.T, db *sql.DB, version int) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var name string
	err := db.QueryRowContext(ctx,
		`SELECT name FROM schema_migrations_v0_legacy WHERE version = $1`, version).Scan(&name)
	require.NoErrorf(t, err, "read legacy row v%d name", version)
	return name
}

// BridgedTable describes a single-row go-migrate schema_migrations
// (or schema_migrations_<name>) snapshot. Returned by BridgedRow.
type BridgedTable struct {
	Version int
	Dirty   bool
	Rows    int // total row count; should be 1 in steady state
}

// BridgedRow reads the (version, dirty) tuple plus row count from a
// go-migrate-shaped schema_migrations table. Helper for asserting
// that a bridge wrote exactly one row at the expected version.
func BridgedRow(t *testing.T, db *sql.DB, table string) BridgedTable {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var got BridgedTable
	err := db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT version, dirty FROM %s`, table)).Scan(&got.Version, &got.Dirty)
	require.NoErrorf(t, err, "read bridged row from %s", table)

	err = db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM %s`, table)).Scan(&got.Rows)
	require.NoErrorf(t, err, "count rows in %s", table)

	return got
}
