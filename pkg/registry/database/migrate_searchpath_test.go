//go:build integration

package database_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// testAdminDSN returns the admin connection URI:
// AGENT_REGISTRY_TEST_DATABASE_URL when set, otherwise the local dev
// default (localhost:5432, user/password agentregistry). The value must
// be a URL-form DSN (postgres://...); keyword/value DSNs are not supported.
func testAdminDSN() string {
	if dsn := os.Getenv("AGENT_REGISTRY_TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "postgres://agentregistry:agentregistry@localhost:5432/postgres?sslmode=disable"
}

// redactDSN masks the password for log output. Only URL-form DSNs can be
// redacted reliably; anything else is masked wholesale.
func redactDSN(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		return u.Redacted()
	}
	return "(redacted non-URL DSN)"
}

// testDBURI returns adminURI with its database replaced by dbName.
func testDBURI(t *testing.T, adminURI, dbName string) string {
	t.Helper()
	u, err := url.Parse(adminURI)
	require.NoError(t, err)
	u.Path = "/" + dbName
	return u.String()
}

// freshDB creates a fresh per-test Postgres database. Fails when
// PostgreSQL is unavailable.
func freshDB(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminURI := testAdminDSN()
	adminConn, err := pgx.Connect(ctx, adminURI)
	if err != nil {
		t.Fatalf("PostgreSQL not available at %s: %v — start it (e.g. 'make run-docker') or run unit tests only ('make test-unit')", redactDSN(adminURI), err)
	}
	defer func() { _ = adminConn.Close(ctx) }()

	var randomBytes [8]byte
	_, err = rand.Read(randomBytes[:])
	require.NoError(t, err)
	dbName := fmt.Sprintf("test_migrator_sp_%d", binary.BigEndian.Uint64(randomBytes[:]))

	_, err = adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		c, cerr := pgx.Connect(cleanupCtx, adminURI)
		if cerr != nil {
			return
		}
		defer func() { _ = c.Close(cleanupCtx) }()
		_, _ = c.Exec(cleanupCtx,
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			dbName)
		_, _ = c.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	})

	return testDBURI(t, adminURI, dbName)
}

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
