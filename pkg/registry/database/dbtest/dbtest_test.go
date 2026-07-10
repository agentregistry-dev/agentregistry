//go:build integration

package dbtest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactDSN(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{"userinfo password", "postgres://user:secret@host:5432/db?sslmode=disable",
			"postgres://user:xxxxx@host:5432/db?sslmode=disable"},
		{"password query param", "postgres://user@host:5432/db?password=secret&sslmode=disable",
			"postgres://user@host:5432/db?password=xxxxx&sslmode=disable"},
		{"sslpassword query param", "postgres://user@host:5432/db?sslpassword=secret",
			"postgres://user@host:5432/db?sslpassword=xxxxx"},
		{"postgresql scheme", "postgresql://user:secret@host/db",
			"postgresql://user:xxxxx@host/db"},
		{"no credentials", "postgres://host:5432/db?sslmode=disable",
			"postgres://host:5432/db?sslmode=disable"},
		{"keyword/value form", "host=h user=u password=secret dbname=db",
			"(redacted non-URL DSN)"},
		{"garbage", "://not a url", "(redacted non-URL DSN)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, RedactDSN(tt.dsn))
			require.NotContains(t, RedactDSN(tt.dsn), "secret")
		})
	}
}

func TestDBURI(t *testing.T) {
	tests := []struct {
		name     string
		adminURI string
		dbName   string
		want     string
	}{
		{"default admin DSN", "postgres://agentregistry:agentregistry@localhost:5432/postgres?sslmode=disable",
			"test_db_1", "postgres://agentregistry:agentregistry@localhost:5432/test_db_1?sslmode=disable"},
		{"dbname query param dropped", "postgres://u:p@h:5432/postgres?dbname=postgres&sslmode=disable",
			"test_db_2", "postgres://u:p@h:5432/test_db_2?sslmode=disable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DBURI(tt.adminURI, tt.dbName)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestValidateAdminDSN(t *testing.T) {
	require.NoError(t, validateAdminDSN("postgres://u:p@h:5432/postgres"))
	require.NoError(t, validateAdminDSN("postgresql://h/db"))

	err := validateAdminDSN("host=h user=u password=secret dbname=db")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")

	require.Error(t, validateAdminDSN("mysql://h/db"))
}
