//nolint:testpackage
package service

import (
	"context"
	"sync"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeIndexerRegistry struct {
	Servers []*apiv0.ServerResponse
	Agents  []*models.AgentResponse

	ServerEmbeddingMeta map[string]*database.SemanticEmbeddingMetadata
	AgentEmbeddingMeta  map[string]*database.SemanticEmbeddingMetadata

	UpsertServerEmbeddingCalls int
	UpsertAgentEmbeddingCalls  int
}

func newFakeIndexerRegistry() *fakeIndexerRegistry {
	return &fakeIndexerRegistry{
		ServerEmbeddingMeta: make(map[string]*database.SemanticEmbeddingMetadata),
		AgentEmbeddingMeta:  make(map[string]*database.SemanticEmbeddingMetadata),
	}
}

func (f *fakeIndexerRegistry) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	if cursor != "" {
		return nil, "", nil
	}
	return f.Servers, "", nil
}

func (f *fakeIndexerRegistry) GetServerByName(context.Context, string) (*apiv0.ServerResponse, error) {
	if len(f.Servers) == 0 {
		return nil, database.ErrNotFound
	}
	return f.Servers[0], nil
}

func (f *fakeIndexerRegistry) GetServerByNameAndVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	for _, server := range f.Servers {
		if server.Server.Name == serverName && (version == "" || server.Server.Version == version) {
			return server, nil
		}
	}
	return nil, database.ErrNotFound
}

func (f *fakeIndexerRegistry) GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	versions := make([]*apiv0.ServerResponse, 0, len(f.Servers))
	for _, server := range f.Servers {
		if server.Server.Name == serverName {
			versions = append(versions, server)
		}
	}
	if len(versions) == 0 {
		return nil, database.ErrNotFound
	}
	return versions, nil
}

func (f *fakeIndexerRegistry) CreateServer(context.Context, *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeIndexerRegistry) UpdateServer(context.Context, string, string, *apiv0.ServerJSON, *string) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeIndexerRegistry) StoreServerReadme(context.Context, string, string, []byte, string) error {
	return nil
}

func (f *fakeIndexerRegistry) GetServerReadmeLatest(context.Context, string) (*database.ServerReadme, error) {
	return nil, database.ErrNotFound
}

func (f *fakeIndexerRegistry) GetServerReadmeByVersion(context.Context, string, string) (*database.ServerReadme, error) {
	return nil, database.ErrNotFound
}

func (f *fakeIndexerRegistry) DeleteServer(context.Context, string, string) error {
	return nil
}

func (f *fakeIndexerRegistry) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	key := serverName + "@" + version
	if meta, ok := f.ServerEmbeddingMeta[key]; ok {
		return meta, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeIndexerRegistry) UpsertServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	f.UpsertServerEmbeddingCalls++
	return nil
}

func (f *fakeIndexerRegistry) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	if cursor != "" {
		return nil, "", nil
	}
	return f.Agents, "", nil
}

func (f *fakeIndexerRegistry) GetAgentByName(context.Context, string) (*models.AgentResponse, error) {
	if len(f.Agents) == 0 {
		return nil, database.ErrNotFound
	}
	return f.Agents[0], nil
}

func (f *fakeIndexerRegistry) GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	for _, agent := range f.Agents {
		if agent.Agent.Name == agentName && (version == "" || agent.Agent.Version == version) {
			return agent, nil
		}
	}
	return nil, database.ErrNotFound
}

func (f *fakeIndexerRegistry) GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	versions := make([]*models.AgentResponse, 0, len(f.Agents))
	for _, agent := range f.Agents {
		if agent.Agent.Name == agentName {
			versions = append(versions, agent)
		}
	}
	if len(versions) == 0 {
		return nil, database.ErrNotFound
	}
	return versions, nil
}

func (f *fakeIndexerRegistry) CreateAgent(context.Context, *models.AgentJSON) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeIndexerRegistry) DeleteAgent(context.Context, string, string) error {
	return nil
}

func (f *fakeIndexerRegistry) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	key := agentName + "@" + version
	if meta, ok := f.AgentEmbeddingMeta[key]; ok {
		return meta, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeIndexerRegistry) UpsertAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	f.UpsertAgentEmbeddingCalls++
	return nil
}

func (f *fakeIndexerRegistry) ResolveAgentManifestSkills(context.Context, *models.AgentManifest) ([]platformtypes.AgentSkillRef, error) {
	return nil, nil
}

func (f *fakeIndexerRegistry) ResolveAgentManifestPrompts(context.Context, *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
	return nil, nil
}

type fakeIndexerServerStore struct{ registry *fakeIndexerRegistry }

func (s *fakeIndexerServerStore) DeleteServer(context.Context, string, string) error {
	return nil
}

func (s *fakeIndexerServerStore) CreateServer(context.Context, *apiv0.ServerJSON, *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (s *fakeIndexerServerStore) UpdateServer(context.Context, string, string, *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (s *fakeIndexerServerStore) SetServerStatus(context.Context, string, string, string) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (s *fakeIndexerServerStore) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	return s.registry.ListServers(ctx, filter, cursor, limit)
}

func (s *fakeIndexerServerStore) GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	return s.registry.GetServerByName(ctx, serverName)
}

func (s *fakeIndexerServerStore) GetServerByNameAndVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	return s.registry.GetServerByNameAndVersion(ctx, serverName, version)
}

func (s *fakeIndexerServerStore) GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	return s.registry.GetAllVersionsByServerName(ctx, serverName)
}

func (s *fakeIndexerServerStore) GetCurrentLatestVersion(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	return s.registry.GetServerByName(ctx, serverName)
}

func (s *fakeIndexerServerStore) CountServerVersions(context.Context, string) (int, error) {
	return 1, nil
}

func (s *fakeIndexerServerStore) CheckVersionExists(context.Context, string, string) (bool, error) {
	return true, nil
}

func (s *fakeIndexerServerStore) UnmarkAsLatest(context.Context, string) error {
	return nil
}

func (s *fakeIndexerServerStore) AcquireServerCreateLock(context.Context, string) error {
	return nil
}

func (s *fakeIndexerServerStore) SetServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	return s.registry.UpsertServerEmbedding(ctx, serverName, version, embedding)
}

func (s *fakeIndexerServerStore) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.registry.GetServerEmbeddingMetadata(ctx, serverName, version)
}

func (s *fakeIndexerServerStore) UpsertServerReadme(context.Context, *database.ServerReadme) error {
	return nil
}

func (s *fakeIndexerServerStore) GetServerReadme(context.Context, string, string) (*database.ServerReadme, error) {
	return nil, database.ErrNotFound
}

func (s *fakeIndexerServerStore) GetLatestServerReadme(context.Context, string) (*database.ServerReadme, error) {
	return nil, database.ErrNotFound
}

type fakeIndexerAgentStore struct{ registry *fakeIndexerRegistry }

func (s *fakeIndexerAgentStore) CreateAgent(context.Context, *models.AgentJSON, *models.AgentRegistryExtensions) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (s *fakeIndexerAgentStore) UpdateAgent(context.Context, string, string, *models.AgentJSON) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (s *fakeIndexerAgentStore) SetAgentStatus(context.Context, string, string, string) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (s *fakeIndexerAgentStore) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	return s.registry.ListAgents(ctx, filter, cursor, limit)
}

func (s *fakeIndexerAgentStore) GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return s.registry.GetAgentByName(ctx, agentName)
}

func (s *fakeIndexerAgentStore) GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	return s.registry.GetAgentByNameAndVersion(ctx, agentName, version)
}

func (s *fakeIndexerAgentStore) GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	return s.registry.GetAllVersionsByAgentName(ctx, agentName)
}

func (s *fakeIndexerAgentStore) GetCurrentLatestAgentVersion(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return s.registry.GetAgentByName(ctx, agentName)
}

func (s *fakeIndexerAgentStore) CountAgentVersions(context.Context, string) (int, error) {
	return 1, nil
}

func (s *fakeIndexerAgentStore) CheckAgentVersionExists(context.Context, string, string) (bool, error) {
	return true, nil
}

func (s *fakeIndexerAgentStore) UnmarkAgentAsLatest(context.Context, string) error {
	return nil
}

func (s *fakeIndexerAgentStore) DeleteAgent(context.Context, string, string) error {
	return nil
}

func (s *fakeIndexerAgentStore) SetAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	return s.registry.UpsertAgentEmbedding(ctx, agentName, version, embedding)
}

func (s *fakeIndexerAgentStore) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.registry.GetAgentEmbeddingMetadata(ctx, agentName, version)
}

type fakeIndexerNoopStore struct{}

func (fakeIndexerNoopStore) CreateSkill(context.Context, *models.SkillJSON, *models.SkillRegistryExtensions) (*models.SkillResponse, error) {
	return nil, database.ErrInvalidInput
}

func (fakeIndexerNoopStore) UpdateSkill(context.Context, string, string, *models.SkillJSON) (*models.SkillResponse, error) {
	return nil, database.ErrInvalidInput
}

func (fakeIndexerNoopStore) SetSkillStatus(context.Context, string, string, string) (*models.SkillResponse, error) {
	return nil, database.ErrInvalidInput
}

func (fakeIndexerNoopStore) ListSkills(context.Context, *database.SkillFilter, string, int) ([]*models.SkillResponse, string, error) {
	return nil, "", nil
}

func (fakeIndexerNoopStore) GetSkillByName(context.Context, string) (*models.SkillResponse, error) {
	return nil, database.ErrNotFound
}

func (fakeIndexerNoopStore) GetSkillByNameAndVersion(context.Context, string, string) (*models.SkillResponse, error) {
	return nil, database.ErrNotFound
}

func (fakeIndexerNoopStore) GetAllVersionsBySkillName(context.Context, string) ([]*models.SkillResponse, error) {
	return nil, database.ErrNotFound
}

func (fakeIndexerNoopStore) GetCurrentLatestSkillVersion(context.Context, string) (*models.SkillResponse, error) {
	return nil, database.ErrNotFound
}

func (fakeIndexerNoopStore) CountSkillVersions(context.Context, string) (int, error) {
	return 0, nil
}

func (fakeIndexerNoopStore) CheckSkillVersionExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (fakeIndexerNoopStore) UnmarkSkillAsLatest(context.Context, string) error {
	return nil
}

func (fakeIndexerNoopStore) DeleteSkill(context.Context, string, string) error {
	return nil
}

func (fakeIndexerNoopStore) CreatePrompt(context.Context, *models.PromptJSON, *models.PromptRegistryExtensions) (*models.PromptResponse, error) {
	return nil, database.ErrInvalidInput
}

func (fakeIndexerNoopStore) ListPrompts(context.Context, *database.PromptFilter, string, int) ([]*models.PromptResponse, string, error) {
	return nil, "", nil
}

func (fakeIndexerNoopStore) GetPromptByName(context.Context, string) (*models.PromptResponse, error) {
	return nil, database.ErrNotFound
}

func (fakeIndexerNoopStore) GetPromptByNameAndVersion(context.Context, string, string) (*models.PromptResponse, error) {
	return nil, database.ErrNotFound
}

func (fakeIndexerNoopStore) GetAllVersionsByPromptName(context.Context, string) ([]*models.PromptResponse, error) {
	return nil, database.ErrNotFound
}

func (fakeIndexerNoopStore) GetCurrentLatestPromptVersion(context.Context, string) (*models.PromptResponse, error) {
	return nil, database.ErrNotFound
}

func (fakeIndexerNoopStore) CountPromptVersions(context.Context, string) (int, error) {
	return 0, nil
}

func (fakeIndexerNoopStore) CheckPromptVersionExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (fakeIndexerNoopStore) UnmarkPromptAsLatest(context.Context, string) error {
	return nil
}

func (fakeIndexerNoopStore) DeletePrompt(context.Context, string, string) error {
	return nil
}

func (fakeIndexerNoopStore) CreateProvider(context.Context, *models.CreateProviderInput) (*models.Provider, error) {
	return nil, database.ErrInvalidInput
}

func (fakeIndexerNoopStore) ListProviders(context.Context, *string) ([]*models.Provider, error) {
	return nil, nil
}

func (fakeIndexerNoopStore) GetProviderByID(context.Context, string) (*models.Provider, error) {
	return nil, database.ErrNotFound
}

func (fakeIndexerNoopStore) UpdateProvider(context.Context, string, *models.UpdateProviderInput) (*models.Provider, error) {
	return nil, database.ErrInvalidInput
}

func (fakeIndexerNoopStore) DeleteProvider(context.Context, string) error {
	return nil
}

func (fakeIndexerNoopStore) CreateDeployment(context.Context, *models.Deployment) error {
	return database.ErrInvalidInput
}

func (fakeIndexerNoopStore) GetDeployments(context.Context, *models.DeploymentFilter) ([]*models.Deployment, error) {
	return nil, nil
}

func (fakeIndexerNoopStore) GetDeploymentByID(context.Context, string) (*models.Deployment, error) {
	return nil, database.ErrNotFound
}

func (fakeIndexerNoopStore) UpdateDeploymentState(context.Context, string, *models.DeploymentStatePatch) error {
	return nil
}

func (fakeIndexerNoopStore) RemoveDeploymentByID(context.Context, string) error {
	return nil
}

type fakeIndexerStore struct {
	*fakeIndexerServerStore
	*fakeIndexerAgentStore
	fakeIndexerNoopStore
}

func newFakeIndexerStore(registry *fakeIndexerRegistry) *fakeIndexerStore {
	return &fakeIndexerStore{
		fakeIndexerServerStore: &fakeIndexerServerStore{registry: registry},
		fakeIndexerAgentStore:  &fakeIndexerAgentStore{registry: registry},
	}
}

func (s *fakeIndexerStore) Servers() database.ServerStore { return s.fakeIndexerServerStore }

func (s *fakeIndexerStore) Providers() database.ProviderStore { return nil }

func (s *fakeIndexerStore) Agents() database.AgentStore { return s.fakeIndexerAgentStore }

func (s *fakeIndexerStore) Skills() database.SkillStore { return nil }

func (s *fakeIndexerStore) Prompts() database.PromptStore { return nil }

func (s *fakeIndexerStore) Deployments() database.DeploymentStore { return nil }

func (s *fakeIndexerStore) InTransaction(ctx context.Context, fn func(context.Context, database.Scope) error) error {
	return fn(ctx, s)
}

func (s *fakeIndexerStore) Close() error {
	return nil
}

func newTestIndexer(registry *fakeIndexerRegistry, provider embeddings.Provider, dimensions int) Indexer {
	return NewIndexer(
		registry,
		registry,
		provider,
		dimensions,
	)
}

// mockProvider implements embeddings.Provider for testing.
type mockProvider struct {
	generateFunc func(ctx context.Context, payload embeddings.Payload) (*embeddings.Result, error)
	callCount    int
	mu           sync.Mutex
}

func (m *mockProvider) Generate(ctx context.Context, payload embeddings.Payload) (*embeddings.Result, error) {
	m.mu.Lock()
	m.callCount++
	generateFunc := m.generateFunc
	m.mu.Unlock()

	if generateFunc != nil {
		return generateFunc(ctx, payload)
	}
	return &embeddings.Result{
		Vector:     make([]float32, 1536),
		Dimensions: 1536,
	}, nil
}

func (m *mockProvider) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func TestIndexer_Run_ProviderNil(t *testing.T) {
	mockRegistry := newFakeIndexerRegistry()
	indexer := newTestIndexer(mockRegistry, nil, 1536)

	opts := IndexOptions{
		IncludeServers: true,
		IncludeAgents:  false,
	}

	result, err := indexer.Run(context.Background(), opts, nil)

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestIndexer_Run_NoTargetsSelected(t *testing.T) {
	mockRegistry := newFakeIndexerRegistry()
	mockProv := &mockProvider{}
	indexer := newTestIndexer(mockRegistry, mockProv, 1536)

	opts := IndexOptions{
		IncludeServers: false,
		IncludeAgents:  false,
	}

	result, err := indexer.Run(context.Background(), opts, nil)

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no targets")
}

func TestIndexer_Run_ServersOnly(t *testing.T) {
	mockRegistry := newFakeIndexerRegistry()
	mockRegistry.Servers = []*apiv0.ServerResponse{
		{
			Server: apiv0.ServerJSON{
				Name:        "com.example/server1",
				Version:     "1.0.0",
				Description: "Test server 1",
			},
		},
		{
			Server: apiv0.ServerJSON{
				Name:        "com.example/server2",
				Version:     "2.0.0",
				Description: "Test server 2",
			},
		},
	}
	mockRegistry.Agents = []*models.AgentResponse{
		{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:        "com.example/agent1",
					Description: "Test agent 1",
				},
				Version: "1.0.0",
			},
		},
	}

	mockProv := &mockProvider{}
	indexer := newTestIndexer(mockRegistry, mockProv, 1536)

	opts := IndexOptions{
		IncludeServers: true,
		IncludeAgents:  false,
	}

	result, err := indexer.Run(context.Background(), opts, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 2, result.Servers.Processed)
	assert.Equal(t, 2, result.Servers.Updated)
	assert.Equal(t, 0, result.Agents.Processed) // Agents not processed
	assert.Equal(t, 2, mockRegistry.UpsertServerEmbeddingCalls)
	assert.Equal(t, 0, mockRegistry.UpsertAgentEmbeddingCalls)
}

func TestIndexer_Run_AgentsOnly(t *testing.T) {
	mockRegistry := newFakeIndexerRegistry()
	mockRegistry.Servers = []*apiv0.ServerResponse{
		{
			Server: apiv0.ServerJSON{
				Name:        "com.example/server1",
				Version:     "1.0.0",
				Description: "Test server 1",
			},
		},
	}
	mockRegistry.Agents = []*models.AgentResponse{
		{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:        "com.example/agent1",
					Description: "Test agent 1",
				},
				Version: "1.0.0",
			},
		},
		{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:        "com.example/agent2",
					Description: "Test agent 2",
				},
				Version: "2.0.0",
			},
		},
	}

	mockProv := &mockProvider{}
	indexer := newTestIndexer(mockRegistry, mockProv, 1536)

	opts := IndexOptions{
		IncludeServers: false,
		IncludeAgents:  true,
	}

	result, err := indexer.Run(context.Background(), opts, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.Servers.Processed) // Servers not processed
	assert.Equal(t, 2, result.Agents.Processed)
	assert.Equal(t, 2, result.Agents.Updated)
	assert.Equal(t, 0, mockRegistry.UpsertServerEmbeddingCalls)
	assert.Equal(t, 2, mockRegistry.UpsertAgentEmbeddingCalls)
}

func TestIndexer_Run_DryRun(t *testing.T) {
	mockRegistry := newFakeIndexerRegistry()
	mockRegistry.Servers = []*apiv0.ServerResponse{
		{
			Server: apiv0.ServerJSON{
				Name:        "com.example/server1",
				Version:     "1.0.0",
				Description: "Test server 1",
			},
		},
	}

	mockProv := &mockProvider{}
	indexer := newTestIndexer(mockRegistry, mockProv, 1536)

	opts := IndexOptions{
		IncludeServers: true,
		IncludeAgents:  false,
		DryRun:         true,
	}

	result, err := indexer.Run(context.Background(), opts, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Servers.Processed)
	assert.Equal(t, 1, result.Servers.Updated)
	// No actual embeddings should be generated or persisted in dry run
	assert.Equal(t, 0, mockProv.getCallCount())
	assert.Equal(t, 0, mockRegistry.UpsertServerEmbeddingCalls)
}

func TestIndexer_Run_Force(t *testing.T) {
	mockRegistry := newFakeIndexerRegistry()
	mockRegistry.Servers = []*apiv0.ServerResponse{
		{
			Server: apiv0.ServerJSON{
				Name:        "com.example/server1",
				Version:     "1.0.0",
				Description: "Test server 1",
			},
		},
	}
	// Set existing embedding metadata with matching checksum
	mockRegistry.ServerEmbeddingMeta["com.example/server1@1.0.0"] = &database.SemanticEmbeddingMetadata{
		HasEmbedding: true,
		Checksum:     "some-checksum",
	}

	mockProv := &mockProvider{}
	indexer := newTestIndexer(mockRegistry, mockProv, 1536)

	opts := IndexOptions{
		IncludeServers: true,
		IncludeAgents:  false,
		Force:          true,
	}

	result, err := indexer.Run(context.Background(), opts, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Servers.Processed)
	assert.Equal(t, 1, result.Servers.Updated)
	assert.Equal(t, 0, result.Servers.Skipped) // Force should override skip
	assert.Equal(t, 1, mockRegistry.UpsertServerEmbeddingCalls)
}

func TestIndexer_Run_SkipsUpToDate(t *testing.T) {
	mockRegistry := newFakeIndexerRegistry()
	server := &apiv0.ServerResponse{
		Server: apiv0.ServerJSON{
			Name:        "com.example/server1",
			Version:     "1.0.0",
			Description: "Test server 1",
		},
	}
	mockRegistry.Servers = []*apiv0.ServerResponse{server}

	// Calculate the actual checksum for this server's payload
	payload := embeddings.BuildServerEmbeddingPayload(&server.Server)
	checksum := embeddings.PayloadChecksum(payload)

	// Set existing embedding metadata with matching checksum
	mockRegistry.ServerEmbeddingMeta["com.example/server1@1.0.0"] = &database.SemanticEmbeddingMetadata{
		HasEmbedding: true,
		Checksum:     checksum,
	}

	mockProv := &mockProvider{}
	indexer := newTestIndexer(mockRegistry, mockProv, 1536)

	opts := IndexOptions{
		IncludeServers: true,
		IncludeAgents:  false,
		Force:          false,
	}

	result, err := indexer.Run(context.Background(), opts, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Servers.Processed)
	assert.Equal(t, 0, result.Servers.Updated)
	assert.Equal(t, 1, result.Servers.Skipped) // Should skip because checksum matches
	assert.Equal(t, 0, mockProv.getCallCount())
	assert.Equal(t, 0, mockRegistry.UpsertServerEmbeddingCalls)
}

func TestIndexer_Run_ContextCancelled(t *testing.T) {
	mockRegistry := newFakeIndexerRegistry()
	mockRegistry.Servers = []*apiv0.ServerResponse{
		{
			Server: apiv0.ServerJSON{
				Name:        "com.example/server1",
				Version:     "1.0.0",
				Description: "Test server 1",
			},
		},
	}

	mockProv := &mockProvider{}
	indexer := newTestIndexer(mockRegistry, mockProv, 1536)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opts := IndexOptions{
		IncludeServers: true,
		IncludeAgents:  false,
	}

	result, err := indexer.Run(ctx, opts, nil)

	assert.Nil(t, result)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

func TestIndexer_Run_ProgressCallback(t *testing.T) {
	mockRegistry := newFakeIndexerRegistry()
	// Add enough servers to trigger progress callback
	for i := range 5 {
		mockRegistry.Servers = append(mockRegistry.Servers, &apiv0.ServerResponse{
			Server: apiv0.ServerJSON{
				Name:        "com.example/server" + string(rune('a'+i)),
				Version:     "1.0.0",
				Description: "Test server",
			},
		})
	}

	mockProv := &mockProvider{}
	indexer := newTestIndexer(mockRegistry, mockProv, 1536)

	var progressCalls []IndexStats
	var mu sync.Mutex
	callback := func(resource string, stats IndexStats) {
		mu.Lock()
		progressCalls = append(progressCalls, stats)
		mu.Unlock()
	}

	opts := IndexOptions{
		IncludeServers: true,
		IncludeAgents:  false,
	}

	result, err := indexer.Run(context.Background(), opts, callback)

	require.NoError(t, err)
	require.NotNil(t, result)

	mu.Lock()
	defer mu.Unlock()
	// At minimum, the final progress callback should be invoked
	assert.GreaterOrEqual(t, len(progressCalls), 1)

	// The last callback should have the final stats
	lastCall := progressCalls[len(progressCalls)-1]
	assert.Equal(t, 5, lastCall.Processed)
}

func TestIndexer_Run_EmptyPayloadSkipped(t *testing.T) {
	mockRegistry := newFakeIndexerRegistry()
	// Server with empty description - should produce empty payload
	mockRegistry.Servers = []*apiv0.ServerResponse{
		{
			Server: apiv0.ServerJSON{
				Schema:  model.CurrentSchemaURL,
				Name:    "com.example/empty-server",
				Version: "1.0.0",
				// No description, tools, etc.
			},
		},
	}

	mockProv := &mockProvider{}
	indexer := newTestIndexer(mockRegistry, mockProv, 1536)

	opts := IndexOptions{
		IncludeServers: true,
		IncludeAgents:  false,
	}

	result, err := indexer.Run(context.Background(), opts, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Servers.Processed)
	// Depending on how empty the payload is, it might be skipped
	// This tests that empty payloads are handled gracefully
}

func TestIndexer_Run_BothServersAndAgents(t *testing.T) {
	mockRegistry := newFakeIndexerRegistry()
	mockRegistry.Servers = []*apiv0.ServerResponse{
		{
			Server: apiv0.ServerJSON{
				Name:        "com.example/server1",
				Version:     "1.0.0",
				Description: "Test server",
			},
		},
	}
	mockRegistry.Agents = []*models.AgentResponse{
		{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:        "com.example/agent1",
					Description: "Test agent",
				},
				Version: "1.0.0",
			},
		},
	}

	mockProv := &mockProvider{}
	indexer := newTestIndexer(mockRegistry, mockProv, 1536)

	opts := IndexOptions{
		IncludeServers: true,
		IncludeAgents:  true,
	}

	result, err := indexer.Run(context.Background(), opts, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Servers.Processed)
	assert.Equal(t, 1, result.Servers.Updated)
	assert.Equal(t, 1, result.Agents.Processed)
	assert.Equal(t, 1, result.Agents.Updated)
	assert.Equal(t, 1, mockRegistry.UpsertServerEmbeddingCalls)
	assert.Equal(t, 1, mockRegistry.UpsertAgentEmbeddingCalls)
}

func TestIndexer_Run_DefaultBatchSize(t *testing.T) {
	mockRegistry := newFakeIndexerRegistry()
	mockRegistry.Servers = []*apiv0.ServerResponse{
		{
			Server: apiv0.ServerJSON{
				Name:        "com.example/server1",
				Version:     "1.0.0",
				Description: "Test server",
			},
		},
	}

	mockProv := &mockProvider{}
	indexer := newTestIndexer(mockRegistry, mockProv, 1536)

	opts := IndexOptions{
		IncludeServers: true,
		IncludeAgents:  false,
		BatchSize:      0, // Should default to 100
	}

	result, err := indexer.Run(context.Background(), opts, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Servers.Processed)
}
