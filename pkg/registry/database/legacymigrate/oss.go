// Package legacymigrate copies pre-PR-#503 OSS data from the legacy
// `v1alpha1.*` schema into the new orchestrator-owned `agentregistry`
// schema. The orchestrator calls `RunOSS` once per upgraded deployment
// (gated on `public.schema_migrations` existing and the new schema's
// `schema_migrations` being empty); fresh installs and re-runs skip it.
//
// The package will be removed in a future release once a follow-up
// migration drops the `v1alpha1.*` residue tables.
package legacymigrate

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/orchestrator"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// OSSSource returns the orchestrator.Source value for the OSS
// migration set. Used by both the server startup path
// (`internal/registry/database/postgres.go`) and the CLI's `arctl db
// migrate up` to register or run the OSS source with identical
// configuration.
func OSSSource() orchestrator.Source {
	return orchestrator.Source{
		Name:      "oss",
		Schema:    database.OSSSchema,
		Files:     v1alpha1store.MigrationFiles,
		Dir:       v1alpha1store.MigrationsDir,
		LegacyRun: RunOSS,
	}
}

// ossTables is the set of pre-#503 OSS data tables the copy covers,
// in dependency order (none reference another via FK today, but the
// order is stable for log readability and future-proofing).
var ossTables = []string{
	"agents",
	"mcp_servers",
	"runtimes",
	"skills",
	"prompts",
	"deployments",
}

// RunOSS copies each `v1alpha1.<t>` row into `agentregistry.<t>` via
// `INSERT ... SELECT ... ON CONFLICT DO NOTHING` inside a single
// transaction. Defensive: if `v1alpha1.agents` doesn't exist, returns
// nil without touching the database (the orchestrator already gates
// on `public.schema_migrations` existing, so this is belt-and-suspenders).
//
// Partial failure rolls back the whole copy; the orchestrator's
// advisory lock + re-run guard cover the retry.
//
// Old `v1alpha1.*` tables are not dropped — a future regular
// go-migrate migration handles that.
func RunOSS(ctx context.Context, db *sql.DB) error {
	exists, err := legacyAgentsExists(ctx, db)
	if err != nil {
		return fmt.Errorf("probe v1alpha1.agents: %w", err)
	}
	if !exists {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy-copy tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, table := range ossTables {
		q := fmt.Sprintf(
			"INSERT INTO agentregistry.%s SELECT * FROM v1alpha1.%s ON CONFLICT DO NOTHING",
			table, table)
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("copy v1alpha1.%s: %w", table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy-copy tx: %w", err)
	}
	return nil
}

// legacyAgentsExists probes for the canonical `v1alpha1.agents` table
// as a proxy for the legacy data set's presence. Picked because every
// pre-#503 deployment has it; any other table in `ossTables` would
// work equally well.
func legacyAgentsExists(ctx context.Context, db *sql.DB) (bool, error) {
	var oid sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT to_regclass('v1alpha1.agents')::text").Scan(&oid); err != nil {
		return false, err
	}
	return oid.Valid, nil
}
