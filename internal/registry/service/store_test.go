package service

import (
	"context"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/jackc/pgx/v5"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/require"
)

type storeTestTx struct{ pgx.Tx }

type storeTestDB struct {
	database.Database
	testingT       *testing.T
	inTransaction  bool
	listServersFn  func(ctx context.Context, tx pgx.Tx, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	deleteServerFn func(ctx context.Context, tx pgx.Tx, serverName, version string) error
}

func (m *storeTestDB) InTransaction(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	m.inTransaction = true
	defer func() {
		m.inTransaction = false
	}()

	return fn(ctx, storeTestTx{})
}

func (m *storeTestDB) ListServers(ctx context.Context, tx pgx.Tx, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	require.NotNil(m.testingT, m.testingT, "testingT must be set")
	require.NotNil(m.testingT, m.listServersFn, "listServersFn must be set")
	return m.listServersFn(ctx, tx, filter, cursor, limit)
}

func (m *storeTestDB) DeleteServer(ctx context.Context, tx pgx.Tx, serverName, version string) error {
	require.NotNil(m.testingT, m.testingT, "testingT must be set")
	require.NotNil(m.testingT, m.deleteServerFn, "deleteServerFn must be set")
	return m.deleteServerFn(ctx, tx, serverName, version)
}

func TestReadStoresFallsBackToDatabaseRepositories(t *testing.T) {
	called := false
	mockDB := &storeTestDB{
		testingT: t,
		listServersFn: func(ctx context.Context, tx pgx.Tx, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
			require.Nil(t, tx)
			require.Nil(t, filter)
			require.Equal(t, "", cursor)
			require.Equal(t, 25, limit)
			called = true
			return nil, "next-cursor", nil
		},
	}

	svc := &registryServiceImpl{db: mockDB}

	_, nextCursor, err := svc.readStores().servers.ListServers(context.Background(), nil, "", 25)
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, "next-cursor", nextCursor)
}

func TestReadStoresUsesRepositoryOverrides(t *testing.T) {
	databaseCalled := false
	overrideCalled := false

	mockDB := &storeTestDB{
		testingT: t,
		listServersFn: func(ctx context.Context, tx pgx.Tx, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
			databaseCalled = true
			return nil, "database", nil
		},
	}
	override := &storeTestDB{
		testingT: t,
		listServersFn: func(ctx context.Context, tx pgx.Tx, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
			overrideCalled = true
			return nil, "override", nil
		},
	}

	svc := &registryServiceImpl{
		db:         mockDB,
		serverRepo: database.NewServiceDatabase(override),
	}

	_, nextCursor, err := svc.readStores().servers.ListServers(context.Background(), nil, "", 10)
	require.NoError(t, err)
	require.True(t, overrideCalled)
	require.False(t, databaseCalled)
	require.Equal(t, "override", nextCursor)
}

func TestInTransactionUsesTransactionalStores(t *testing.T) {
	var mockDB *storeTestDB
	mockDB = &storeTestDB{
		testingT: t,
		deleteServerFn: func(ctx context.Context, tx pgx.Tx, serverName, version string) error {
			require.True(t, mockDB.inTransaction)
			_, ok := tx.(storeTestTx)
			require.True(t, ok)
			require.Equal(t, "io.test/server", serverName)
			require.Equal(t, "1.0.0", version)
			return nil
		},
	}

	svc := &registryServiceImpl{db: mockDB}

	err := svc.inTransaction(context.Background(), func(ctx context.Context, stores storeBundle) error {
		return stores.servers.DeleteServer(ctx, "io.test/server", "1.0.0")
	})
	require.NoError(t, err)
}
