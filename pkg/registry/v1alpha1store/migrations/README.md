# OSS migration set

This directory holds the OSS Postgres migrations applied by both the
server (at startup, unless `SkipMigrations` is set) and the `arctl db
migrate` CLI. The migrator is `golang-migrate/migrate v4` with the
`pgx/v5` driver and the `iofs` source — see
`pkg/registry/database/migrate.go`.

The current set is a single consolidated migration
(`001_initial_schema`) representing the current baseline schema. Any
new schema change is a new `NNN_*.up.sql` / `.down.sql` pair stacked
on top.

## Schema-agnostic discipline (enforced by lint test)

Migrations must NOT reference a Postgres schema by name. The runtime
schema is set via `migratepgx.Config{SchemaName: ...}` and resolved
through `search_path`. Authors:

- Use unqualified identifiers everywhere (`CREATE TABLE agents`, not
  `CREATE TABLE agentregistry.agents`).
- Do **not** include `CREATE SCHEMA` — the orchestrator creates the
  source's schema before the migration runs.
- Do **not** include `SET search_path` — the driver sets it.
- Cross-schema references in migrations are not allowed.

`pkg/registry/v1alpha1store/migrations_lint_test.go` walks the embed
on every `go test ./...` and rejects any of those patterns with a
`filename:line` pointer.

### Invariant: unqualified DDL ↔ `search_path` set on the connection

`migratepgx.Config{SchemaName: ...}` only controls where the
`schema_migrations` bookkeeping table lives — it does NOT set
`search_path` on the connection. Unqualified DDL in migrations
therefore depends on `search_path` being set elsewhere, or it falls
through to the connecting user's default (`"$user", public`) and
lands tables in the wrong schema. `database.NewMigrator` injects
`search_path=<schema>` into the DSN as a connection-startup parameter
so every migratepgx-acquired connection sees the right value from the
moment it's established. **Any factory that bypasses `NewMigrator`
and opens its own `*sql.DB` for a `*migrate.Migrate` must replicate
the same DSN injection** (or set `search_path` explicitly per
connection) or migrations will silently land in the wrong schema
whenever the connecting user's name doesn't match the target schema.
Regression test: `pkg/registry/database/migrate_searchpath_test.go`.

## File-naming convention

```
NNN_short_name.up.sql        # forward (required)
NNN_short_name.down.sql      # reverse (required by the iofs source)
```

- `NNN` is the migration version, zero-padded to three digits. New
  migrations get the next unused number. Numbers do not have to be
  contiguous — gaps are fine and stay in place forever once a number
  has been used.
- `short_name` is a lowercase, underscore-separated description of the
  change. It appears in error messages and recovery logs — keep it
  readable.
- Same prefix on both files. The iofs source pairs them by version
  prefix and refuses to load if the pair is incomplete.

## Forward migrations

`NNN_short_name.up.sql` is the schema change. **Do not wrap the file
in `BEGIN;` / `COMMIT;`** — go-migrate runs the SQL through `Exec`,
and Postgres autocommits single-statement DDL. Multi-statement
migrations are not atomic by default.

A partially-applied migration leaves go-migrate's `schema_migrations`
row marked dirty; subsequent `up` invocations refuse to run until the
marker is cleared. The orchestrator does not auto-recover from the
dirty state — operators clear it via `arctl db migrate force V`, where
`V` is the version named in the failure message. **This is bookkeeping
recovery only — it does not undo any DDL that committed before the
migration failed.** Author every migration with idempotent DDL so a
retry of the partially-applied migration is safe:

| Use | Instead of |
|---|---|
| `CREATE TABLE IF NOT EXISTS ...` | `CREATE TABLE ...` |
| `CREATE INDEX IF NOT EXISTS ...` | `CREATE INDEX ...` |
| `ALTER TABLE ... ADD COLUMN IF NOT EXISTS ...` | `ALTER TABLE ... ADD COLUMN ...` |
| `CREATE OR REPLACE FUNCTION ...` | `CREATE FUNCTION ...` |
| `CREATE OR REPLACE TRIGGER ...` | `CREATE TRIGGER ...` (Postgres 14+) |
| `DROP TABLE IF EXISTS ...` | `DROP TABLE ...` |

## Reverse migrations

`NNN_short_name.down.sql` is the inverse. The iofs source requires the
file to exist (the pair is the unit of work); it does not run the file
unless an operator invokes `arctl db migrate down` or a backward
`goto`.

- **Reversible migrations** — ship the inverse:
  ```
  005_widget_owner_index.up.sql:
    CREATE INDEX IF NOT EXISTS widget_owner_idx ON widget (owner);

  005_widget_owner_index.down.sql:
    DROP INDEX IF EXISTS widget_owner_idx;
  ```
- **Up-only / destructive migrations** — write a `.down.sql` that
  fails loudly so attempted rollbacks surface clearly:
  ```sql
  DO $$ BEGIN
    RAISE EXCEPTION 'migration NNN_<name> is not reversible (up-only)';
  END $$;
  ```
  `001_initial_schema` ships with this form (the consolidated baseline
  is not reversible — operators wishing to start over drop the
  database).

## Testing

The lint test runs in every `go test ./...` and is the first gate any
new migration must pass. Schema-and-data integration coverage lives
under `pkg/registry/database/integration/` with `//go:build integration`.

## Downgrade is not supported once the bridge has fired

The first boot of an engine-swap binary against a pre-engine-swap
database fires the legacy bridge: rows are copied from `v1alpha1.*`
into the orchestrator-owned schema (default `agentregistry`),
`public.schema_migrations` is renamed to
`public.schema_migrations_v0_legacy`, and all subsequent app writes
go to the new schema. The `v1alpha1.*` tables are left in place but
become frozen — nothing updates them after the bridge.

Rolling back to a pre-engine-swap binary at this point is not
supported. The old binary would read and write the frozen
`v1alpha1.*` tables, silently regressing the application's view of
the data to the pre-bridge snapshot. Any rows written after the
bridge would be invisible to the downgraded binary. The failure mode
is silent, not loud — operators don't get an error; they get stale
data.

A follow-up release will ship a regular migration that drops the
`v1alpha1.*` residue tables. After that release the downgrade
constraint becomes a physical impossibility (the rollback target no
longer exists) rather than a documented one. Until then the rollback
target exists but is stale; the constraint is load-bearing during
the lifetime of this release.

Operators who need a true rollback path must restore the database
from a backup taken before the engine-swap binary was deployed.
