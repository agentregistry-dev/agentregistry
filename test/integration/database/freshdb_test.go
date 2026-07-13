//go:build integration

// Package database holds Postgres-backed integration tests for the
// pkg/registry/database tree (migrator, orchestrator, legacy bridge).
package database

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// adminDSN returns the admin connection URI: AGENT_REGISTRY_TEST_DATABASE_URL
// when set, otherwise the local dev default.
func adminDSN() string {
	if dsn := os.Getenv("AGENT_REGISTRY_TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "postgres://agentregistry:agentregistry@localhost:5432/postgres?sslmode=disable"
}

// freshDB creates a fresh per-test database and returns its URI, dropped on
// t.Cleanup. Fails when PostgreSQL is unavailable. Failure messages never
// include the DSN so credentials stay out of test output and CI logs.
func freshDB(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminURI := adminDSN()
	adminConn, err := pgx.Connect(ctx, adminURI)
	if err != nil {
		t.Fatalf("PostgreSQL not available: %v — start it (e.g. 'make run-docker') or run unit tests only ('make test-unit')", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()

	var randomBytes [8]byte
	_, err = rand.Read(randomBytes[:])
	require.NoError(t, err)
	dbName := fmt.Sprintf("test_db_%d", binary.BigEndian.Uint64(randomBytes[:]))

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

	uri, err := dbURI(adminURI, dbName)
	require.NoError(t, err)
	return uri
}

// dbURI returns adminURI with its database replaced by dbName. A dbname
// query parameter is dropped — pgx would apply it after the path, silently
// redirecting every per-test URI back to the override's database.
func dbURI(adminURI, dbName string) (string, error) {
	u, err := url.Parse(adminURI)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "", errors.New("admin DSN must be a URL-form DSN (postgres://...)")
	}
	u.Path = "/" + dbName
	if q := u.Query(); q.Has("dbname") {
		q.Del("dbname")
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}
