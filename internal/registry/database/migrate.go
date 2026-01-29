package database

import (
	"embed"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// DefaultMigratorConfig returns the default configuration for OSS migrations.
func DefaultMigratorConfig() database.MigratorConfig {
	return database.MigratorConfig{
		MigrationFiles: migrationFiles,
		VersionOffset:  0,
		EnsureTable:    true,
	}
}
