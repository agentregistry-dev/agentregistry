//go:build integration

package v1alpha1store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTestDBURI(t *testing.T) {
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
			got, err := testDBURI(tt.adminURI, tt.dbName)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	// Non-URL DSNs are rejected without echoing the value.
	_, err := testDBURI("host=h user=u password=secret dbname=db", "test_db_3")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
}

func TestAdminDSN(t *testing.T) {
	t.Setenv("AGENT_REGISTRY_TEST_DATABASE_URL", "postgres://u:p@custom:5433/postgres")
	require.Equal(t, "postgres://u:p@custom:5433/postgres", adminDSN())

	t.Setenv("AGENT_REGISTRY_TEST_DATABASE_URL", "")
	require.Equal(t, "postgres://agentregistry:agentregistry@localhost:5432/postgres?sslmode=disable", adminDSN())
	require.NoError(t, validateAdminDSN(adminDSN()))
}

func TestValidateAdminDSN(t *testing.T) {
	require.NoError(t, validateAdminDSN("postgres://u:p@h:5432/postgres"))
	require.NoError(t, validateAdminDSN("postgresql://h/db"))

	err := validateAdminDSN("host=h user=u password=secret dbname=db")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")

	require.Error(t, validateAdminDSN("mysql://h/db"))
}
