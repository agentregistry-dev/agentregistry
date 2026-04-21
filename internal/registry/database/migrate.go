package database

import (
	"embed"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

//go:embed migrations_v1alpha1/*.sql
var v1alpha1MigrationFiles embed.FS

// V1Alpha1MigratorConfig returns the configuration for the v1alpha1 schema
// migrations. Every table the server touches in production lives under the
// v1alpha1 PostgreSQL schema; the legacy public.* migrations were retired
// alongside the per-kind service / store stack.
func V1Alpha1MigratorConfig() database.MigratorConfig {
	return database.MigratorConfig{
		MigrationFiles: v1alpha1MigrationFiles,
		MigrationDir:   "migrations_v1alpha1",
		VersionOffset:  200,
		EnsureTable:    true,
	}
}
