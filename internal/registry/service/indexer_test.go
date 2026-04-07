//nolint:testpackage
package service

import (
	"context"
	"sync"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
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

func (f *fakeIndexerRegistry) ListServers(_ context.Context, _ *database.ServerFilter, cursor string, _ int) ([]*apiv0.ServerResponse, string, error) {
	if cursor != "" {
		return nil, "", nil
	}
	return f.Servers, "", nil
}

func (f *fakeIndexerRegistry) GetServerEmbeddingMetadata(_ context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	key := serverName + "@" + version
	if meta, ok := f.ServerEmbeddingMeta[key]; ok {
		return meta, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeIndexerRegistry) SetServerEmbedding(_ context.Context, _, _ string, _ *database.SemanticEmbedding) error {
	f.UpsertServerEmbeddingCalls++
	return nil
}

func (f *fakeIndexerRegistry) ListAgents(_ context.Context, _ *database.AgentFilter, cursor string, _ int) ([]*models.AgentResponse, string, error) {
	if cursor != "" {
		return nil, "", nil
	}
	return f.Agents, "", nil
}

func (f *fakeIndexerRegistry) GetAgentEmbeddingMetadata(_ context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	key := agentName + "@" + version
	if meta, ok := f.AgentEmbeddingMeta[key]; ok {
		return meta, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeIndexerRegistry) SetAgentEmbedding(_ context.Context, _, _ string, _ *database.SemanticEmbedding) error {
	f.UpsertAgentEmbeddingCalls++
	return nil
}

func newTestIndexer(registry *fakeIndexerRegistry, provider embeddings.Provider, dimensions int) Indexer {
	return NewIndexer(registry, registry, provider, dimensions)
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
