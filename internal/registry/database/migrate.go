package database

import (
	"embed"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

//go:embed migrations_vector/*.sql
var vectorMigrationFiles embed.FS

// DefaultMigratorConfig returns the default configuration for OSS migrations.
func DefaultMigratorConfig() database.MigratorConfig {
	return database.MigratorConfig{
		MigrationFiles: migrationFiles,
		VersionOffset:  0,
		EnsureTable:    true,
	}
}

// VectorMigratorConfig returns the configuration for vector/pgvector migrations.
// These are applied separately, only when vector support is enabled.
// VersionOffset 100 keeps vector migrations in a separate namespace from base migrations.
func VectorMigratorConfig() database.MigratorConfig {
	return database.MigratorConfig{
		MigrationFiles: vectorMigrationFiles,
		MigrationDir:   "migrations_vector",
		VersionOffset:  100,
		EnsureTable:    false,
	}
}
