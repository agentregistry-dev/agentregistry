//go:build integration

// Package dbtest holds DSN plumbing shared by integration-test DB helpers.
package dbtest

import (
	"fmt"
	"net/url"
	"os"
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
		return "", fmt.Errorf("parse admin URI: %w", err)
	}
	u.Path = "/" + dbName
	if q := u.Query(); q.Has("dbname") {
		q.Del("dbname")
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}
