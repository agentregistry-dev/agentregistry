package database

import (
	"embed"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

//go:embed migrations_vector/*.sql
var vectorMigrationFiles embed.FS

//go:embed migrations_v1alpha1/*.sql
var v1alpha1MigrationFiles embed.FS

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

// V1Alpha1MigratorConfig returns the configuration for the v1alpha1 schema
// migrations. Introduced as part of the Kubernetes-style API refactor; during
// PR 2 this config is consumed only by the new generic Store's unit tests.
// PR 3 flips the production migration runner to use this config in place of
// DefaultMigratorConfig.
//
// VersionOffset 200 keeps the v1alpha1 namespace clear of both the legacy OSS
// migrations (0-) and the vector migrations (100-).
func V1Alpha1MigratorConfig() database.MigratorConfig {
	return database.MigratorConfig{
		MigrationFiles: v1alpha1MigrationFiles,
		MigrationDir:   "migrations_v1alpha1",
		VersionOffset:  200,
		EnsureTable:    true,
	}
}
