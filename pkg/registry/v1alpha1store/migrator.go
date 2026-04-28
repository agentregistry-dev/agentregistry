package v1alpha1store

import (
	"embed"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

//go:embed migrations/*.sql
var v1alpha1MigrationFiles embed.FS

// embeddingsMigrationVersion is the filename-version of the pgvector
// migration that ships embedding columns + HNSW indexes. Skipped when
// the runtime embeddings flag is off so a vanilla PostgreSQL install
// (no pgvector extension available) starts cleanly.
const embeddingsMigrationVersion = 3

// MigratorConfig returns the configuration for the v1alpha1 schema
// migrations. Every table the server touches in production lives under
// the v1alpha1 PostgreSQL schema.
//
// embeddingsEnabled gates the pgvector migration (003_embeddings.sql).
// When false (the AGENT_REGISTRY_EMBEDDINGS_ENABLED default), the
// embedding columns + HNSW indexes are not created and `CREATE
// EXTENSION vector` never runs — pgvector becomes a feature
// dependency rather than an install dependency. Flipping the flag on
// later applies the migration on next start; existing installs with
// the flag on keep working because the migration is idempotent.
func MigratorConfig(embeddingsEnabled bool) database.MigratorConfig {
	cfg := database.MigratorConfig{
		MigrationFiles: v1alpha1MigrationFiles,
		MigrationDir:   "migrations",
		VersionOffset:  200,
		EnsureTable:    true,
	}
	if !embeddingsEnabled {
		cfg.Skip = func(version int) bool {
			return version == embeddingsMigrationVersion
		}
	}
	return cfg
}
