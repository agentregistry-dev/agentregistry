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
	"log/slog"
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
		Name:      database.OSSSourceName,
		Schema:    database.MustNewSchema(database.OSSSchema),
		Files:     v1alpha1store.MigrationFiles,
		Dir:       v1alpha1store.MigrationsDir,
		LegacyRun: RunOSS,
	}
}

// Pre-redesign migrations were recorded in a shared public.schema_migrations
// table partitioned by a per-source version offset, so multiple tracks could
// coexist. OSS migrations carry offset 200 (migration N recorded as 200+N);
// other registered tracks use higher offsets, keeping their versions above
// the OSS range.
const (
	// legacyOSSVersionOffset is the offset added to OSS migration versions in
	// the legacy public.schema_migrations table (migration N is recorded as
	// 200+N). The version probe bounds between this offset and the floor, so
	// higher-numbered rows owned by downstream/extension tracks stay out of the
	// result without encoding their numbering here.
	legacyOSSVersionOffset = 200

	// legacyOSSVersionFloor is the offset version of the last OSS migration
	// shipped before the migration redesign — migration 008, recorded as 208.
	// The frozen column lists below describe the v1alpha1 schema as of this
	// version, so a database that stopped short of it cannot be bridged safely:
	// the copy would reference a column the source lacks (aborting the upgrade)
	// or skip data a later pre-redesign migration would have added. Such
	// databases are rejected and must finish applying the pre-redesign
	// migrations first.
	legacyOSSVersionFloor = legacyOSSVersionOffset + 8
)

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

// RunOSS copies each `v1alpha1.<t>` row into the destination `schema`'s
// `<t>` (the OSS schema in practice) via `INSERT (cols) ... SELECT cols
// ... ON CONFLICT DO NOTHING` inside a single transaction. schema is the
// orchestrator-resolved Source.Schema, passed in rather than re-derived.
// Columns are addressed explicitly so the copy stays correct if source
// and destination column orders ever diverge.
// Defensive: if `v1alpha1.agents` doesn't exist, returns nil without
// touching the database (the orchestrator already gates on
// `public.schema_migrations` existing, so this is belt-and-suspenders).
// Within the copy loop, each table is probed individually with
// `to_regclass` and skipped if missing so an operator who manually
// dropped one of the legacy tables doesn't fault the entire bridge.
//
// Partial failure rolls back the whole copy; the orchestrator's
// advisory lock + re-run guard cover the retry.
//
// Legacy `v1alpha1.*` tables are not dropped — a follow-up regular
// go-migrate migration handles that.
func RunOSS(ctx context.Context, db *sql.DB, schema database.Schema) error {
	// Reject a partially-migrated OSS track up front rather than abort
	// mid-copy; see legacyOSSVersionFloor for why the column lists below
	// require the full pre-redesign sequence.
	legacyVer, hasLegacyOSS, err := legacyOSSVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("probe legacy OSS migration version: %w", err)
	}
	if hasLegacyOSS && legacyVer < legacyOSSVersionFloor {
		return fmt.Errorf(
			"OSS database is at migration v%d but the migration redesign requires the last "+
				"release before it (through migration v%d); finish applying the pre-redesign "+
				"migrations first, then re-run the upgrade",
			legacyVer-legacyOSSVersionOffset, legacyOSSVersionFloor-legacyOSSVersionOffset)
	}

	exists, err := legacyAgentsExists(ctx, db)
	if err != nil {
		return fmt.Errorf("probe v1alpha1.agents: %w", err)
	}
	if !exists {
		return nil
	}

	destSchema := schema.Quoted()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy-copy tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	logger := slog.Default().With("component", "database.legacymigrate.oss")
	for _, table := range ossTables {
		cols, ok := ossTableColumns[table]
		if !ok {
			return fmt.Errorf("copy v1alpha1.%s: no column list registered", table)
		}
		// Skip tables an operator may have manually dropped — the
		// gate's `legacy_agents` proxy doesn't catch per-table
		// absence, so probe within the loop.
		tableExists, err := legacyTableExists(ctx, tx, table)
		if err != nil {
			return fmt.Errorf("probe v1alpha1.%s: %w", table, err)
		}
		if !tableExists {
			// Logged (not silent) so an unexpected legacy layout surfaces.
			logger.Warn("legacy source table absent; skipping", "table", "v1alpha1."+table)
			continue
		}
		quotedTable := pgx.Identifier{table}.Sanitize()
		var srcRows int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM v1alpha1."+quotedTable).Scan(&srcRows); err != nil {
			return fmt.Errorf("count v1alpha1.%s: %w", table, err)
		}
		colList := quotedColumnList(cols)
		q := fmt.Sprintf(
			"INSERT INTO %s.%s (%s) SELECT %s FROM v1alpha1.%s ON CONFLICT DO NOTHING",
			destSchema, quotedTable, colList, colList, quotedTable,
		)
		res, err := tx.ExecContext(ctx, q)
		if err != nil {
			return fmt.Errorf("copy v1alpha1.%s: %w", table, err)
		}
		inserted, _ := res.RowsAffected()
		logger.Info("bridged table",
			"source", "v1alpha1."+table, "dest", schema.Name()+"."+table,
			"source_rows", srcRows, "inserted", inserted)
		// Reconciliation: warn only when the destination ends up genuinely
		// short (re-run-safe — on re-run dest_rows >= source_rows). A default
		// seed from 001 keeps dest_rows >= source_rows for tables like runtimes.
		if srcRows > 0 && inserted < srcRows {
			var destRows int64
			if err := tx.QueryRowContext(ctx,
				fmt.Sprintf("SELECT count(*) FROM %s.%s", destSchema, quotedTable)).Scan(&destRows); err != nil {
				return fmt.Errorf("count %s.%s: %w", destSchema, quotedTable, err)
			}
			if destRows < srcRows {
				logger.Warn("destination has fewer rows than legacy source after copy; possible dropped rows",
					"source", "v1alpha1."+table, "dest", schema.Name()+"."+table,
					"source_rows", srcRows, "dest_rows", destRows)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy-copy tx: %w", err)
	}
	return nil
}

// legacyTableExists reports whether `v1alpha1.<table>` is present.
// Probed inside the bridge transaction so a missing table can be
// silently skipped without faulting the whole copy.
func legacyTableExists(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	var oid sql.NullString
	if err := tx.QueryRowContext(ctx,
		"SELECT to_regclass($1)::text", "v1alpha1."+table).Scan(&oid); err != nil {
		return false, err
	}
	return oid.Valid, nil
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

// legacyOSSVersion returns the highest OSS migration version recorded in the
// prior custom migrator's public.schema_migrations table, bounded to the OSS
// track's own version space (legacyOSSVersionOffset, legacyOSSVersionFloor] so
// higher-numbered rows owned by downstream/extension sources don't mask a short
// OSS upgrade, and whether any OSS migration was recorded at all. A missing
// table — or a table with only downstream-track rows — reports (0, false).
func legacyOSSVersion(ctx context.Context, db *sql.DB) (version int, present bool, err error) {
	var oid sql.NullString
	if err := db.QueryRowContext(ctx,
		"SELECT to_regclass('public.schema_migrations')::text").Scan(&oid); err != nil {
		return 0, false, err
	}
	if !oid.Valid {
		return 0, false, nil
	}
	var maxVersion sql.NullInt64
	if err := db.QueryRowContext(ctx,
		"SELECT MAX(version) FROM public.schema_migrations WHERE version > $1 AND version <= $2",
		legacyOSSVersionOffset, legacyOSSVersionFloor).Scan(&maxVersion); err != nil {
		return 0, false, fmt.Errorf("read legacy OSS migration version: %w", err)
	}
	if !maxVersion.Valid {
		return 0, false, nil
	}
	return int(maxVersion.Int64), true, nil
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
