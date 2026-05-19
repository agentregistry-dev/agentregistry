package database

import (
	"embed"
	"strings"
	"testing"
)

//go:embed testdata/down_fixture/*.sql
var downFixtureFiles embed.FS

// TestLoadMigrations_PairsUpAndDownSiblings verifies the loader's
// behavior around the .down.sql sibling convention without touching a
// database. Up files surface as Migration entries; their .down.sql
// content lands in DownSQL; .down.sql files themselves are not
// double-counted as up migrations.
func TestLoadMigrations_PairsUpAndDownSiblings(t *testing.T) {
	m := NewMigrator(nil, MigratorConfig{
		MigrationFiles: downFixtureFiles,
		MigrationDir:   "testdata/down_fixture",
		VersionOffset:  100,
	})
	migrations, err := m.loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("expected 2 up migrations (001, 002); got %d", len(migrations))
	}

	// 001_first.sql is up-only (no .down.sql sibling).
	if got := migrations[0].Version; got != 101 {
		t.Errorf("migration[0].Version = %d; want 101", got)
	}
	if migrations[0].DownSQL != "" {
		t.Errorf("migration[0].DownSQL should be empty (up-only); got %q", migrations[0].DownSQL)
	}

	// 002_with_down.sql has a sibling.
	if got := migrations[1].Version; got != 102 {
		t.Errorf("migration[1].Version = %d; want 102", got)
	}
	if !strings.Contains(migrations[1].DownSQL, "DROP TABLE second") {
		t.Errorf("migration[1].DownSQL should contain DROP TABLE second; got %q", migrations[1].DownSQL)
	}
	// The up SQL should not have been polluted by the down sibling.
	if strings.Contains(migrations[1].SQL, "DROP TABLE") {
		t.Errorf("migration[1].SQL leaked DROP TABLE from sibling: %q", migrations[1].SQL)
	}
}

// TestLoadMigrations_SkipsDownFileAsTopLevelEntry guards against the
// loader treating "002_with_down.down.sql" as its own up migration
// (version 2, with .down.sql in the filename suffix).
func TestLoadMigrations_SkipsDownFileAsTopLevelEntry(t *testing.T) {
	m := NewMigrator(nil, MigratorConfig{
		MigrationFiles: downFixtureFiles,
		MigrationDir:   "testdata/down_fixture",
		VersionOffset:  0,
	})
	migrations, err := m.loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	// 001, 002 — and crucially NOT a third entry parsed from the
	// .down.sql sibling.
	if len(migrations) != 2 {
		t.Fatalf("expected exactly 2 entries; got %d (down sibling counted as up?)", len(migrations))
	}
}
