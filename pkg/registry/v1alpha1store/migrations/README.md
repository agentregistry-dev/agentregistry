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
