//go:build integration

// Package dbtest holds DSN plumbing shared by integration-test DB helpers.
package dbtest

import (
	"context"
	"errors"
	"fmt"
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
// would then be broken in confusing ways.
func validateAdminDSN(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return fmt.Errorf("AGENT_REGISTRY_TEST_DATABASE_URL must be a URL-form DSN (postgres://...), got %s", RedactDSN(dsn))
	}
	return nil
}

// ConnectAdmin resolves and validates the admin DSN and connects to it,
// failing the test with an actionable message when Postgres is unreachable.
// Returns the connection and the resolved admin DSN.
func ConnectAdmin(ctx context.Context, t *testing.T) (*pgx.Conn, string) {
	t.Helper()
	adminURI := AdminDSN()
	if err := validateAdminDSN(adminURI); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, adminURI)
	if err != nil {
		t.Fatalf("PostgreSQL not available at %s: %v — start it (e.g. 'make run-docker') or run unit tests only ('make test-unit')", RedactDSN(adminURI), err)
	}
	return conn, adminURI
}

// RedactDSN masks credentials for log output: the userinfo password and any
// password/sslpassword query parameters. Only URL-form DSNs can be redacted
// reliably; anything else is masked wholesale.
func RedactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return "(redacted non-URL DSN)"
	}
	q := u.Query()
	for _, k := range []string{"password", "sslpassword"} {
		if q.Has(k) {
			q.Set(k, "xxxxx")
		}
	}
	u.RawQuery = q.Encode()
	return u.Redacted()
}

// DBURI returns adminURI with its database replaced by dbName. A dbname
// query parameter is dropped — pgx would apply it after the path, silently
// redirecting every per-test URI back to the override's database.
func DBURI(adminURI, dbName string) (string, error) {
	u, err := url.Parse(adminURI)
	if err != nil {
		// url.Error embeds the full unredacted URI; unwrap before wrapping.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return "", fmt.Errorf("parse admin URI %s: %w", RedactDSN(adminURI), err)
	}
	u.Path = "/" + dbName
	if q := u.Query(); q.Has("dbname") {
		q.Del("dbname")
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}
