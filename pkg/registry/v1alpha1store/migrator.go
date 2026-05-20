package v1alpha1store

import (
	"embed"

	"github.com/golang-migrate/migrate/v4"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

//go:embed migrations/*.sql
var v1alpha1MigrationFiles embed.FS

// MigrationsTable is the schema_migrations table that holds the OSS
// migration audit trail. Exported so the legacy-bootstrap helper in
// `pkg/registry/database` can target the same name go-migrate creates.
const MigrationsTable = "schema_migrations"

// MigrationFiles is the embedded FS containing every OSS migration.
// Exported so the legacy-bootstrap helper can probe filename existence
// when deciding whether to carry an old `schema_migrations` row
// forward.
var MigrationFiles = v1alpha1MigrationFiles

// NewOSSMigrator constructs a golang-migrate migrator for the OSS
// schema migrations. The caller owns mg.Close().
func NewOSSMigrator(dsn string) (*migrate.Migrate, error) {
	return database.NewMigrator(dsn, v1alpha1MigrationFiles, "migrations", MigrationsTable)
}
