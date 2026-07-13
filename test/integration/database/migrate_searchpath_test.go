//go:build integration

package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// TestNewMigrator_LandsTablesInTargetSchema asserts that unqualified
// CREATE TABLE in a migration lands in the SchemaName configured on
// the migrator — even when that schema name does not match the
// connecting user's default schema.
//
// Regression for the latent search_path bug: migratepgx.WithInstance
// uses SchemaName only for the `schema_migrations` location, not for
// the connection's search_path. When the schema name matches the
// connecting user (e.g. user "agentregistry" → default search_path
// "$user, public" → schema "agentregistry"), unqualified DDL
// coincidentally lands in the right place. When the schema name
// differs (e.g. a downstream schema like "agentregistry_ext" connecting
// as "agentregistry"), DDL falls through to "public". NewMigrator works
// around this by injecting `search_path=<schema>` into the DSN.
func TestNewMigrator_LandsTablesInTargetSchema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := freshDB(t)

	const targetSchema = "downstream_test_schema" // intentionally NOT matching the user name
	mfs := fstest.MapFS{
		"migrations/001_init.up.sql":   {Data: []byte("CREATE TABLE demo_tbl (id int);")},
		"migrations/001_init.down.sql": {Data: []byte("DROP TABLE demo_tbl;")},
	}

	mg, err := database.NewMigrator(ctx, dsn, mfs, "migrations", database.MustNewSchema(targetSchema))
	require.NoError(t, err)
	defer func() { _, _ = mg.Close() }()

	require.NoError(t, mg.Up())

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// `demo_tbl` MUST land in downstream_test_schema, not in public.
	var inTarget, inPublic bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT to_regclass($1) IS NOT NULL", targetSchema+".demo_tbl").Scan(&inTarget))
	require.True(t, inTarget, "demo_tbl must live in %s (the SchemaName configured on the migrator)", targetSchema)
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT to_regclass('public.demo_tbl') IS NOT NULL").Scan(&inPublic))
	require.False(t, inPublic, "demo_tbl must NOT have landed in public (search_path fix regression)")

	// schema_migrations also lives in the target schema (the
	// migratepgx SchemaName-driven path).
	var smInTarget bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT to_regclass($1) IS NOT NULL", targetSchema+".schema_migrations").Scan(&smInTarget))
	require.True(t, smInTarget, "schema_migrations must live in %s", targetSchema)
}

// TestNewMigrator_ReturnsErrNoChangeOnReRun asserts that calling Up()
// twice with the same migration set returns nil + ErrNoChange the
// second time. Belt-and-suspenders for the search_path-bearing DSN —
// confirms the rewritten DSN still parses on the second open.
func TestNewMigrator_ReturnsErrNoChangeOnReRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dsn := freshDB(t)

	mfs := fstest.MapFS{
		"migrations/001_init.up.sql":   {Data: []byte("CREATE TABLE rerun_tbl (id int);")},
		"migrations/001_init.down.sql": {Data: []byte("DROP TABLE rerun_tbl;")},
	}

	first, err := database.NewMigrator(ctx, dsn, mfs, "migrations", database.MustNewSchema("downstream_rerun_schema"))
	require.NoError(t, err)
	require.NoError(t, first.Up())
	_, _ = first.Close()

	second, err := database.NewMigrator(ctx, dsn, mfs, "migrations", database.MustNewSchema("downstream_rerun_schema"))
	require.NoError(t, err)
	require.True(t, errors.Is(second.Up(), migrate.ErrNoChange))
	_, _ = second.Close()
}
