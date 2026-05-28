// Package legacymigrate copies legacy OSS data from the prior
// `v1alpha1.*` schema into the orchestrator-owned `agentregistry`
// schema. The orchestrator calls `RunOSS` once per upgraded deployment
// (gated on `public.schema_migrations` existing and the new schema's
// `schema_migrations` being empty); fresh installs and re-runs skip it.
//
// A follow-up release will ship a regular migration that drops the
// residue `v1alpha1.*` tables, at which point this package can be
// removed.
package legacymigrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

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

// ossTables is the set of legacy OSS data tables the copy covers,
// in dependency order (none reference another via FK today, but the
// order is stable for log readability and future-proofing). Column
// lists per table are pinned in ossTableColumns below.
var ossTables = []string{
	"agents",
	"mcp_servers",
	"runtimes",
	"skills",
	"prompts",
	"deployments",
}

// ossTableColumns enumerates the columns the legacy-bridge copy
// addresses for each table. INSERTing by an explicit column list keeps
// the copy correct if the source (`v1alpha1.<t>`) and destination
// (`<OSSSchema>.<t>`) ever diverge in column order or add new columns.
//
// The schema this list reflects is FROZEN: it captures what
// `v1alpha1.<t>` looks like for any deployment that lived on the
// pre-engine-swap migrator. Columns added by 002+ migrations to
// `<OSSSchema>.<t>` belong here ONLY if a corresponding column was
// also present in the legacy `v1alpha1.<t>` at the moment of upgrade.
// In practice that means columns added by 002+ stay out of this map
// — the legacy schema is closed.
var ossTableColumns = map[string][]string{
	"agents": {
		"namespace", "name", "tag", "uid", "generation",
		"labels", "annotations", "spec", "content_hash", "status",
		"created_at", "updated_at", "deletion_timestamp",
	},
	"mcp_servers": {
		"namespace", "name", "tag", "uid", "generation",
		"labels", "annotations", "spec", "content_hash", "status",
		"created_at", "updated_at", "deletion_timestamp",
	},
	"runtimes": {
		"namespace", "name", "uid", "generation",
		"labels", "annotations", "spec", "status",
		"deletion_timestamp", "finalizers",
		"created_at", "updated_at",
	},
	"skills": {
		"namespace", "name", "tag", "uid", "generation",
		"labels", "annotations", "spec", "content_hash", "status",
		"created_at", "updated_at", "deletion_timestamp",
	},
	"prompts": {
		"namespace", "name", "tag", "uid", "generation",
		"labels", "annotations", "spec", "content_hash", "status",
		"created_at", "updated_at", "deletion_timestamp",
	},
	"deployments": {
		"namespace", "name", "uid", "generation",
		"labels", "annotations", "spec", "status",
		"deletion_timestamp", "finalizers",
		"created_at", "updated_at",
	},
}

// RunOSS copies each `v1alpha1.<t>` row into `<OSSSchema>.<t>` via
// `INSERT (cols) ... SELECT cols ... ON CONFLICT DO NOTHING` inside a
// single transaction. Columns are addressed explicitly so the copy stays
// correct if source and destination column orders ever diverge.
// Defensive: if `v1alpha1.agents` doesn't exist, returns nil without
// touching the database (the orchestrator already gates on
// `public.schema_migrations` existing, so this is belt-and-suspenders).
//
// Partial failure rolls back the whole copy; the orchestrator's
// advisory lock + re-run guard cover the retry.
//
// Legacy `v1alpha1.*` tables are not dropped — a follow-up regular
// go-migrate migration handles that.
func RunOSS(ctx context.Context, db *sql.DB) error {
	exists, err := legacyAgentsExists(ctx, db)
	if err != nil {
		return fmt.Errorf("probe v1alpha1.agents: %w", err)
	}
	if !exists {
		return nil
	}

	destSchema := pgx.Identifier{database.OSSSchema}.Sanitize()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy-copy tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, table := range ossTables {
		cols, ok := ossTableColumns[table]
		if !ok {
			return fmt.Errorf("copy v1alpha1.%s: no column list registered", table)
		}
		quotedTable := pgx.Identifier{table}.Sanitize()
		colList := quotedColumnList(cols)
		q := fmt.Sprintf(
			"INSERT INTO %s.%s (%s) SELECT %s FROM v1alpha1.%s ON CONFLICT DO NOTHING",
			destSchema, quotedTable, colList, colList, quotedTable,
		)
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("copy v1alpha1.%s: %w", table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy-copy tx: %w", err)
	}
	return nil
}

// quotedColumnList joins an explicit column list with each identifier
// quoted via pgx.Identifier.Sanitize so reserved-word columns and
// future identifier choices stay safe under INSERT/SELECT.
func quotedColumnList(cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = pgx.Identifier{c}.Sanitize()
	}
	return strings.Join(quoted, ", ")
}

// legacyAgentsExists probes for the canonical `v1alpha1.agents` table
// as a proxy for the legacy data set's presence. Picked because every
// pre-engine-swap deployment has it; any other table in `ossTables`
// would work equally well.
func legacyAgentsExists(ctx context.Context, db *sql.DB) (bool, error) {
	var oid sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT to_regclass('v1alpha1.agents')::text").Scan(&oid); err != nil {
		return false, err
	}
	return oid.Valid, nil
}
