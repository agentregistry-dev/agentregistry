# OSS migration set

This directory holds the v1alpha1 Postgres migrations applied by both
the server (at startup, unless `SkipMigrations` is set) and the
`arctl db migrate` CLI. The migrator is `golang-migrate/migrate v4`
with the `pgx/v5` driver and the `iofs` source — see
`pkg/registry/database/migrate.go`.

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

The auto-recovery wrapper in `pkg/registry/database/migrate.go`
clears go-migrate's dirty-state bookkeeping (`Force(current-1)`) so a
partial-failure Up surfaces as an actionable error instead of
`Dirty database version N. Fix and force version.`. **This is
bookkeeping recovery only — it does not undo any DDL that committed
before the migration failed.** Author every migration with
idempotent DDL so a retry of the partially-applied migration is
safe:

Write idempotent DDL whenever the operation has a natural idempotent
form, so a retry after partial failure is safe:

| Use | Instead of |
|---|---|
| `CREATE TABLE IF NOT EXISTS ...` | `CREATE TABLE ...` |
| `CREATE INDEX IF NOT EXISTS ...` | `CREATE INDEX ...` |
| `ALTER TABLE ... ADD COLUMN IF NOT EXISTS ...` | `ALTER TABLE ... ADD COLUMN ...` |
| `CREATE OR REPLACE FUNCTION ...` | `CREATE FUNCTION ...` |
| `DROP TABLE IF EXISTS ...` | `DROP TABLE ...` |

## Reverse migrations

`NNN_short_name.down.sql` is the inverse. The iofs source requires the
file to exist (the pair is the unit of work); it does not run the
file unless an operator invokes `arctl db migrate down` or backward
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
  All migrations predating the convention switch (001/002/003/004/006/007/008)
  ship with this raise-exception form.

## Quick examples

A simple forward + reverse pair:

```
NNN_widget_owner_index.up.sql:
  CREATE INDEX IF NOT EXISTS widget_owner_idx ON widget (owner);

NNN_widget_owner_index.down.sql:
  DROP INDEX IF EXISTS widget_owner_idx;
```

A column add:

```
NNN_widget_archived.up.sql:
  ALTER TABLE widget ADD COLUMN IF NOT EXISTS archived BOOLEAN NOT NULL DEFAULT FALSE;

NNN_widget_archived.down.sql:
  ALTER TABLE widget DROP COLUMN IF EXISTS archived;
```

## Testing

Integration tests for the migrator live in
`pkg/registry/database/migrate_integration_test.go` (build tag
`integration`). They use the `testdata/integration_*` fixture
directories rather than this OSS set — the fixtures stay small and
focused on migrator behavior, not v1alpha1 semantics.

Run them via:

```
make test    # runs the full suite; the integration cases need Postgres at localhost:5432
```
