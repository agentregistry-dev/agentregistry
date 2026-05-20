package v1alpha1store

import (
	"context"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

//go:embed migrations/*.sql
var v1alpha1MigrationFiles embed.FS

// MigrationsTable is the schema_migrations table that holds the OSS
// migration audit trail. Exported so callers that need to interact
// with the same table by name (legacy-bootstrap, integration tests)
// can refer to a single source of truth.
const MigrationsTable = "schema_migrations"

// migrationsDir is the directory inside the embedded FS holding
// NNN_name.up.sql / NNN_name.down.sql pairs.
const migrationsDir = "migrations"

// MigrationFiles is the embedded FS containing every OSS migration.
// Exported so callers (the CLI, downstream tooling) can compute
// pending-migration counts without piercing migrate.Migrate's
// internals.
var MigrationFiles = v1alpha1MigrationFiles

// NewOSSMigrator constructs a golang-migrate migrator for the OSS
// schema migrations. Auto-runs the legacy bootstrap (idempotent
// no-op on fresh installs and already-bridged DBs) before
// constructing the migrator so existing pre-engine-swap deployments
// upgrade transparently. The caller owns mg.Close().
func NewOSSMigrator(dsn string) (*migrate.Migrate, error) {
	if err := database.BootstrapLegacyOSSMigrations(context.Background(), dsn, v1alpha1MigrationFiles, migrationsDir); err != nil {
		return nil, fmt.Errorf("bootstrap legacy OSS migrations: %w", err)
	}
	return database.NewMigrator(dsn, v1alpha1MigrationFiles, migrationsDir, MigrationsTable)
}
