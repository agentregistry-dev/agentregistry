package service

import (
	"context"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/require"
)

type storeTestDB struct {
	database.Store
	testingT         *testing.T
	inTransaction    bool
	listServersFn    func(ctx context.Context, txToken *int, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	listAgentsFn     func(ctx context.Context, txToken *int, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	listSkillsFn     func(ctx context.Context, txToken *int, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error)
	listPromptsFn    func(ctx context.Context, txToken *int, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error)
	listProvidersFn  func(ctx context.Context, txToken *int, platform *string) ([]*models.Provider, error)
	getDeploymentsFn func(ctx context.Context, txToken *int, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	deleteServerFn   func(ctx context.Context, txToken *int, serverName, version string) error
	deleteAgentFn    func(ctx context.Context, txToken *int, agentName, version string) error
}

type storeTestScope struct {
	db      *storeTestDB
	txToken *int
}

type storeTestServerStore struct {
	database.ServerStore
	db      *storeTestDB
	txToken *int
}

type storeTestAgentStore struct {
	database.AgentStore
	db      *storeTestDB
	txToken *int
}

type storeTestSkillStore struct {
	database.SkillStore
	db      *storeTestDB
	txToken *int
}

type storeTestPromptStore struct {
	database.PromptStore
	db      *storeTestDB
	txToken *int
}

type storeTestProviderStore struct {
	database.ProviderStore
	db      *storeTestDB
	txToken *int
}

type storeTestDeploymentStore struct {
	database.DeploymentStore
	db      *storeTestDB
	txToken *int
}

var _ database.Store = (*storeTestDB)(nil)

func (m *storeTestDB) scope(txToken *int) storeTestScope {
	return storeTestScope{db: m, txToken: txToken}
}

func (m *storeTestDB) Servers() database.ServerStore { return m.scope(nil).Servers() }

func (m *storeTestDB) Providers() database.ProviderStore { return m.scope(nil).Providers() }

func (m *storeTestDB) Agents() database.AgentStore { return m.scope(nil).Agents() }

func (m *storeTestDB) Skills() database.SkillStore { return m.scope(nil).Skills() }

func (m *storeTestDB) Prompts() database.PromptStore { return m.scope(nil).Prompts() }

func (m *storeTestDB) Deployments() database.DeploymentStore { return m.scope(nil).Deployments() }

func (m *storeTestDB) InTransaction(ctx context.Context, fn func(context.Context, database.Scope) error) error {
	m.inTransaction = true
	defer func() {
		m.inTransaction = false
	}()

	token := 1
	return fn(ctx, m.scope(&token))
}

func (m *storeTestDB) Close() error {
	return nil
}

func (m *storeTestDB) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	return m.Servers().ListServers(ctx, filter, cursor, limit)
}

func (m *storeTestDB) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	return m.Agents().ListAgents(ctx, filter, cursor, limit)
}

func (m *storeTestDB) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	return m.Skills().ListSkills(ctx, filter, cursor, limit)
}

func (m *storeTestDB) ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	return m.Prompts().ListPrompts(ctx, filter, cursor, limit)
}

func (m *storeTestDB) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	return m.Providers().ListProviders(ctx, platform)
}

func (m *storeTestDB) ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	return m.Deployments().ListDeployments(ctx, filter)
}

func (m *storeTestDB) DeleteServer(ctx context.Context, serverName, version string) error {
	return m.Servers().DeleteServer(ctx, serverName, version)
}

func (m *storeTestDB) DeleteAgent(ctx context.Context, agentName, version string) error {
	return m.Agents().DeleteAgent(ctx, agentName, version)
}

func (s storeTestScope) Servers() database.ServerStore {
	return storeTestServerStore{db: s.db, txToken: s.txToken}
}

func (s storeTestScope) Providers() database.ProviderStore {
	return storeTestProviderStore{db: s.db, txToken: s.txToken}
}

func (s storeTestScope) Agents() database.AgentStore {
	return storeTestAgentStore{db: s.db, txToken: s.txToken}
}

func (s storeTestScope) Skills() database.SkillStore {
	return storeTestSkillStore{db: s.db, txToken: s.txToken}
}

func (s storeTestScope) Prompts() database.PromptStore {
	return storeTestPromptStore{db: s.db, txToken: s.txToken}
}

func (s storeTestScope) Deployments() database.DeploymentStore {
	return storeTestDeploymentStore{db: s.db, txToken: s.txToken}
}

func (s storeTestServerStore) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	require.NotNil(s.db.testingT, s.db.testingT, "testingT must be set")
	require.NotNil(s.db.testingT, s.db.listServersFn, "listServersFn must be set")
	return s.db.listServersFn(ctx, s.txToken, filter, cursor, limit)
}

func (s storeTestAgentStore) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	require.NotNil(s.db.testingT, s.db.testingT, "testingT must be set")
	require.NotNil(s.db.testingT, s.db.listAgentsFn, "listAgentsFn must be set")
	return s.db.listAgentsFn(ctx, s.txToken, filter, cursor, limit)
}

func (s storeTestSkillStore) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	require.NotNil(s.db.testingT, s.db.testingT, "testingT must be set")
	require.NotNil(s.db.testingT, s.db.listSkillsFn, "listSkillsFn must be set")
	return s.db.listSkillsFn(ctx, s.txToken, filter, cursor, limit)
}

func (s storeTestPromptStore) ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	require.NotNil(s.db.testingT, s.db.testingT, "testingT must be set")
	require.NotNil(s.db.testingT, s.db.listPromptsFn, "listPromptsFn must be set")
	return s.db.listPromptsFn(ctx, s.txToken, filter, cursor, limit)
}

func (s storeTestProviderStore) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	require.NotNil(s.db.testingT, s.db.testingT, "testingT must be set")
	require.NotNil(s.db.testingT, s.db.listProvidersFn, "listProvidersFn must be set")
	return s.db.listProvidersFn(ctx, s.txToken, platform)
}

func (s storeTestDeploymentStore) ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	require.NotNil(s.db.testingT, s.db.testingT, "testingT must be set")
	require.NotNil(s.db.testingT, s.db.getDeploymentsFn, "getDeploymentsFn must be set")
	return s.db.getDeploymentsFn(ctx, s.txToken, filter)
}

func (s storeTestServerStore) DeleteServer(ctx context.Context, serverName, version string) error {
	require.NotNil(s.db.testingT, s.db.testingT, "testingT must be set")
	require.NotNil(s.db.testingT, s.db.deleteServerFn, "deleteServerFn must be set")
	return s.db.deleteServerFn(ctx, s.txToken, serverName, version)
}

func (s storeTestAgentStore) DeleteAgent(ctx context.Context, agentName, version string) error {
	require.NotNil(s.db.testingT, s.db.testingT, "testingT must be set")
	require.NotNil(s.db.testingT, s.db.deleteAgentFn, "deleteAgentFn must be set")
	return s.db.deleteAgentFn(ctx, s.txToken, agentName, version)
}

func TestReadStoresUsesStore(t *testing.T) {
	called := false
	mockDB := &storeTestDB{
		testingT: t,
		listServersFn: func(ctx context.Context, txToken *int, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
			require.Nil(t, txToken)
			require.Nil(t, filter)
			require.Equal(t, "", cursor)
			require.Equal(t, 25, limit)
			called = true
			return nil, "next-cursor", nil
		},
	}

	svc := &registryServiceImpl{storeDB: mockDB}

	_, nextCursor, err := svc.readStores().servers.ListServers(context.Background(), nil, "", 25)
	require.NoError(t, err)
	require.True(t, called)
	require.Equal(t, "next-cursor", nextCursor)
}

func TestReadStoresUsesRepositoryOverrides(t *testing.T) {
	tests := []struct {
		name      string
		configure func(base, override *storeTestDB, baseCalled, overrideCalled *bool)
		setRepo   func(svc *registryServiceImpl, repo *storeTestDB)
		invoke    func(t *testing.T, stores storeBundle)
	}{
		{
			name: "servers",
			configure: func(base, override *storeTestDB, baseCalled, overrideCalled *bool) {
				base.listServersFn = func(ctx context.Context, txToken *int, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
					*baseCalled = true
					return nil, "database", nil
				}
				override.listServersFn = func(ctx context.Context, txToken *int, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
					*overrideCalled = true
					return nil, "override", nil
				}
			},
			setRepo: func(svc *registryServiceImpl, repo *storeTestDB) {
				svc.serverRepo = repo.Servers()
			},
			invoke: func(t *testing.T, stores storeBundle) {
				_, nextCursor, err := stores.servers.ListServers(context.Background(), nil, "", 10)
				require.NoError(t, err)
				require.Equal(t, "override", nextCursor)
			},
		},
		{
			name: "agents",
			configure: func(base, override *storeTestDB, baseCalled, overrideCalled *bool) {
				base.listAgentsFn = func(ctx context.Context, txToken *int, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
					*baseCalled = true
					return nil, "database", nil
				}
				override.listAgentsFn = func(ctx context.Context, txToken *int, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
					*overrideCalled = true
					return nil, "override", nil
				}
			},
			setRepo: func(svc *registryServiceImpl, repo *storeTestDB) {
				svc.agentRepo = repo.Agents()
			},
			invoke: func(t *testing.T, stores storeBundle) {
				_, nextCursor, err := stores.agents.ListAgents(context.Background(), nil, "", 10)
				require.NoError(t, err)
				require.Equal(t, "override", nextCursor)
			},
		},
		{
			name: "skills",
			configure: func(base, override *storeTestDB, baseCalled, overrideCalled *bool) {
				base.listSkillsFn = func(ctx context.Context, txToken *int, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
					*baseCalled = true
					return nil, "database", nil
				}
				override.listSkillsFn = func(ctx context.Context, txToken *int, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
					*overrideCalled = true
					return nil, "override", nil
				}
			},
			setRepo: func(svc *registryServiceImpl, repo *storeTestDB) {
				svc.skillRepo = repo.Skills()
			},
			invoke: func(t *testing.T, stores storeBundle) {
				_, nextCursor, err := stores.skills.ListSkills(context.Background(), nil, "", 10)
				require.NoError(t, err)
				require.Equal(t, "override", nextCursor)
			},
		},
		{
			name: "prompts",
			configure: func(base, override *storeTestDB, baseCalled, overrideCalled *bool) {
				base.listPromptsFn = func(ctx context.Context, txToken *int, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
					*baseCalled = true
					return nil, "database", nil
				}
				override.listPromptsFn = func(ctx context.Context, txToken *int, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
					*overrideCalled = true
					return nil, "override", nil
				}
			},
			setRepo: func(svc *registryServiceImpl, repo *storeTestDB) {
				svc.promptRepo = repo.Prompts()
			},
			invoke: func(t *testing.T, stores storeBundle) {
				_, nextCursor, err := stores.prompts.ListPrompts(context.Background(), nil, "", 10)
				require.NoError(t, err)
				require.Equal(t, "override", nextCursor)
			},
		},
		{
			name: "providers",
			configure: func(base, override *storeTestDB, baseCalled, overrideCalled *bool) {
				base.listProvidersFn = func(ctx context.Context, txToken *int, platform *string) ([]*models.Provider, error) {
					*baseCalled = true
					return []*models.Provider{{ID: "database"}}, nil
				}
				override.listProvidersFn = func(ctx context.Context, txToken *int, platform *string) ([]*models.Provider, error) {
					*overrideCalled = true
					return []*models.Provider{{ID: "override"}}, nil
				}
			},
			setRepo: func(svc *registryServiceImpl, repo *storeTestDB) {
				svc.providerRepo = repo.Providers()
			},
			invoke: func(t *testing.T, stores storeBundle) {
				providers, err := stores.providers.ListProviders(context.Background(), nil)
				require.NoError(t, err)
				require.Len(t, providers, 1)
				require.Equal(t, "override", providers[0].ID)
			},
		},
		{
			name: "deployments",
			configure: func(base, override *storeTestDB, baseCalled, overrideCalled *bool) {
				base.getDeploymentsFn = func(ctx context.Context, txToken *int, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
					*baseCalled = true
					return []*models.Deployment{{ID: "database"}}, nil
				}
				override.getDeploymentsFn = func(ctx context.Context, txToken *int, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
					*overrideCalled = true
					return []*models.Deployment{{ID: "override"}}, nil
				}
			},
			setRepo: func(svc *registryServiceImpl, repo *storeTestDB) {
				svc.deploymentRepo = repo.Deployments()
			},
			invoke: func(t *testing.T, stores storeBundle) {
				deployments, err := stores.deployments.ListDeployments(context.Background(), nil)
				require.NoError(t, err)
				require.Len(t, deployments, 1)
				require.Equal(t, "override", deployments[0].ID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseCalled := false
			overrideCalled := false

			base := &storeTestDB{testingT: t}
			override := &storeTestDB{testingT: t}
			tt.configure(base, override, &baseCalled, &overrideCalled)

			svc := &registryServiceImpl{storeDB: base}
			tt.setRepo(svc, override)

			tt.invoke(t, svc.readStores())
			require.True(t, overrideCalled)
			require.False(t, baseCalled)
		})
	}
}

func TestInTransactionUsesTransactionalStores(t *testing.T) {
	var mockDB *storeTestDB
	mockDB = &storeTestDB{
		testingT: t,
		deleteServerFn: func(ctx context.Context, txToken *int, serverName, version string) error {
			require.True(t, mockDB.inTransaction)
			require.NotNil(t, txToken)
			require.Equal(t, "io.test/server", serverName)
			require.Equal(t, "1.0.0", version)
			return nil
		},
	}

	svc := &registryServiceImpl{storeDB: mockDB}

	err := svc.inTransaction(context.Background(), func(ctx context.Context, stores storeBundle) error {
		return stores.servers.DeleteServer(ctx, "io.test/server", "1.0.0")
	})
	require.NoError(t, err)
}

func TestInTransactionReusesTransactionAcrossStoreTypes(t *testing.T) {
	var serverToken *int
	var agentToken *int

	mockDB := &storeTestDB{
		testingT: t,
		deleteServerFn: func(ctx context.Context, txToken *int, serverName, version string) error {
			serverToken = txToken
			return nil
		},
		deleteAgentFn: func(ctx context.Context, txToken *int, agentName, version string) error {
			agentToken = txToken
			return nil
		},
	}

	svc := &registryServiceImpl{storeDB: mockDB}

	err := svc.inTransaction(context.Background(), func(ctx context.Context, stores storeBundle) error {
		if err := stores.servers.DeleteServer(ctx, "io.test/server", "1.0.0"); err != nil {
			return err
		}
		return stores.agents.DeleteAgent(ctx, "io.test/agent", "1.0.0")
	})
	require.NoError(t, err)
	require.NotNil(t, serverToken)
	require.Same(t, serverToken, agentToken)
}

func TestInTransactionRequiresStore(t *testing.T) {
	svc := &registryServiceImpl{}

	err := svc.inTransaction(context.Background(), func(ctx context.Context, stores storeBundle) error {
		return nil
	})
	require.EqualError(t, err, "store is not configured")
}
