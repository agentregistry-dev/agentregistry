//go:build integration

// Package dbtest holds DSN plumbing shared by integration-test DB helpers.
// Failure messages never include the DSN — connection errors already name
// host:port, and credentials must stay out of test output and CI logs.
package dbtest

import (
	"context"
	"errors"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// AdminDSN returns the admin connection URI for integration-test DB helpers:
// AGENT_REGISTRY_TEST_DATABASE_URL when set, otherwise the local dev default
// (localhost:5432, user/password agentregistry). The value must be a
// URL-form DSN (postgres://...); keyword/value DSNs are not supported.
func AdminDSN() string {
	if dsn := os.Getenv("AGENT_REGISTRY_TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "postgres://agentregistry:agentregistry@localhost:5432/postgres?sslmode=disable"
}

// validateAdminDSN rejects non-URL-form DSNs up front: pgx would accept a
// keyword/value DSN for the admin connection, but every derived per-test URI
// would then be broken in confusing ways. The value is not echoed.
func validateAdminDSN(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return errors.New("admin DSN must be a URL-form DSN (postgres://...)")
	}
	return nil
}

// ConnectAdminDSN validates the given admin DSN and connects to it, failing
// the test with an actionable message when Postgres is unreachable.
func ConnectAdminDSN(ctx context.Context, t *testing.T, adminDSN string) *pgx.Conn {
	t.Helper()
	if err := validateAdminDSN(adminDSN); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("PostgreSQL not available: %v — start it (e.g. 'make run-docker') or run unit tests only ('make test-unit')", err)
	}
	return conn
}

// ConnectAdmin resolves the admin DSN via AdminDSN and connects to it.
// Returns the connection and the resolved admin DSN.
func ConnectAdmin(ctx context.Context, t *testing.T) (*pgx.Conn, string) {
	t.Helper()
	adminDSN := AdminDSN()
	return ConnectAdminDSN(ctx, t, adminDSN), adminDSN
}

// DBURI returns adminURI with its database replaced by dbName. A dbname
// query parameter is dropped — pgx would apply it after the path, silently
// redirecting every per-test URI back to the override's database.
func DBURI(adminURI, dbName string) (string, error) {
	if err := validateAdminDSN(adminURI); err != nil {
		return "", err
	}
	u, err := url.Parse(adminURI)
	if err != nil {
		return "", errors.New("parse admin URI: invalid URL")
	}
	u.Path = "/" + dbName
	if q := u.Query(); q.Has("dbname") {
		q.Del("dbname")
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}
