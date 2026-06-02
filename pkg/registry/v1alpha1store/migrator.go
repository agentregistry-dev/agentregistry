package v1alpha1store

import (
	"context"
	"embed"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

//go:embed migrations/*.sql
var v1alpha1MigrationFiles embed.FS

// MigrationsDir is the directory inside MigrationFiles holding
// NNN_name.up.sql / NNN_name.down.sql pairs. Exported so the CLI and
// the orchestrator can pass it alongside MigrationFiles.
const MigrationsDir = "migrations"

// MigrationFiles is the embedded FS containing every OSS migration.
// Exported so callers (the CLI, the orchestrator, downstream tooling)
// can compute pending-migration counts and pass the embed to
// `golang-migrate`'s iofs source without piercing migrate.Migrate's
// internals.
var MigrationFiles fs.FS = v1alpha1MigrationFiles

// NewOSSMigrator constructs a `*migrate.Migrate` against
// `database.OSSSchema` for the OSS migration set. The caller owns
// `mg.Close()`. ctx is accepted for API symmetry with the surrounding
// startup path.
func NewOSSMigrator(ctx context.Context, dsn string) (*migrate.Migrate, error) {
	return database.NewMigrator(ctx, dsn, v1alpha1MigrationFiles, MigrationsDir, database.MustNewSchema(database.OSSSchema))
}
