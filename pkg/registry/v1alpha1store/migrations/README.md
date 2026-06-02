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

The rule catalogue and lint engine live in the shared
`pkg/registry/database/migrationlint` package. `migrationlint.Check(fs,
dir)` walks an embedded migration set and returns one violation per
rejected line (`filename:line` pointer). The OSS set runs it on every
`go test ./...` via `migrations_lint_test.go`; downstream migration sets
run the same `Check` against their own embed (see the package doc).

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

`NNN_short_name.up.sql` is the schema change. **Do not add explicit
`BEGIN;` / `COMMIT;` / `START TRANSACTION`, do not use `CONCURRENTLY`,
and do not use a statement Postgres refuses to run inside a transaction
block** (`VACUUM`, `CREATE DATABASE`, `ALTER SYSTEM`, …). The driver
sends the whole file to Postgres in one `Exec` over the simple query
protocol, which runs it as a **single implicit transaction**: every
statement in the file commits together, or — if any statement fails —
they all roll back together. Postgres DDL is transactional, so a failed
migration leaves no half-applied schema. Any of the constructs above
breaks that single-transaction guarantee. The lint test rejects the
common ones (see "Atomicity invariant" below); it is not exhaustive, so
avoid non-transactional statements generally even if the lint is silent.

Even though the DDL rolls back, a failed migration leaves go-migrate's
`schema_migrations` row marked **dirty**: go-migrate commits the
`(version, dirty=true)` marker in a *separate* transaction before
running the migration body and only clears it after the body succeeds.
So the state after a failure is *clean schema at the prior version +
a dirty bookkeeping marker*. Subsequent `up` invocations refuse to run
until the marker is cleared. On the normal CLI path operators clear it
via `arctl db migrate force V`, where `V` is the version named in the
failure message. (When the failure happens mid-`RunUp` the orchestrator
clears the marker itself as part of restoring the source — see
"Atomicity invariant".) Author every migration with idempotent DDL so a
retry is safe regardless:

| Use | Instead of |
|---|---|
| `CREATE TABLE IF NOT EXISTS ...` | `CREATE TABLE ...` |
| `CREATE INDEX IF NOT EXISTS ...` | `CREATE INDEX ...` |
| `ALTER TABLE ... ADD COLUMN IF NOT EXISTS ...` | `ALTER TABLE ... ADD COLUMN ...` |
| `CREATE OR REPLACE FUNCTION ...` | `CREATE FUNCTION ...` |
| `CREATE OR REPLACE TRIGGER ...` | `CREATE TRIGGER ...` (Postgres 14+) |
| `DROP TABLE IF EXISTS ...` | `DROP TABLE ...` |

### Atomicity invariant (relied on by the orchestrator)

When `RunUp` applies more than one source and a *later* source fails,
the orchestrator restores every source it touched this run — including
the failing one — back to the version it sat at before the run, so the
database returns to the prior release's version combo rather than a
cross-track state no release ships. For the failing source that means:
clear the dirty marker, then run the `.down.sql` files for the
migrations that committed this run, back down to the entry version.

That restore is only safe because of the single-transaction property
above: a failed migration's DDL has fully rolled back, so clearing the
marker and replaying downs lands the schema exactly at the entry
version. **A migration that breaks atomicity — explicit transaction
control, `CONCURRENTLY`, or any other statement that cannot run inside a
transaction block — could leave half-applied DDL that the restore would
then mismatch, silently corrupting the schema.** This is why the lint
test rejects those constructs; the rejection is not stylistic. The lint
catalogue is not exhaustive, so the invariant is ultimately the author's
to uphold: never write a non-transactional statement in a migration. A
source found *already* dirty at the start of a run is not restored at
all — its true state is ambiguous, so `RunUp` surfaces it for `arctl db
migrate force` instead of guessing.

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

The lint (`migrationlint.Check`) runs in every `go test ./...` and is the
first gate any new migration must pass. Schema-and-data integration
coverage lives under `pkg/registry/database/integration/` with
`//go:build integration`.

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
