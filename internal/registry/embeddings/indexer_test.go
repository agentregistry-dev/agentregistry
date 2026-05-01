//go:build integration

package embeddings

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

const (
	testNS      = "default"
	agentsTable = "v1alpha1.agents"
)

// mustSpec JSON-marshals v and fails the test on error.
func mustSpec(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// zeroPadVec returns a fixed-dimension vector with the first few
// positions set to the supplied values.
func zeroPadVec(values ...float32) []float32 {
	v := make([]float32, 1536)
	copy(v, values)
	return v
}

// deterministicProvider returns a Result derived from the payload so
// tests can assert checksum + payload-flow behavior without hitting an
// external API.
type deterministicProvider struct {
	mu     sync.Mutex
	calls  int
	failOn string // payload substring that triggers a provider error
	vector []float32
	dims   int
}

func newDeterministicProvider() *deterministicProvider {
	return &deterministicProvider{vector: zeroPadVec(1, 0, 0), dims: 1536}
}

func (d *deterministicProvider) Generate(ctx context.Context, p Payload) (*Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.failOn != "" && contains(p.Text, d.failOn) {
		return nil, errors.New("provider failed on cue")
	}
	return &Result{
		Vector:     d.vector,
		Provider:   "det",
		Model:      "model-x",
		Dimensions: d.dims,
	}, nil
}

func contains(haystack, needle string) bool {
	return needle != "" && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestIndexer_IndexesAgentsAndSkipsOnChecksumMatch(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, agentsTable)
	ctx := context.Background()

	_, err := agents.Upsert(ctx, testNS, "foo", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Title: "Foo", Description: "hello"}), v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	provider := newDeterministicProvider()
	idx, err := NewIndexer(IndexerConfig{
		Bindings: []KindBinding{{
			Kind:         v1alpha1.KindAgent,
			Store:        agents,
			BuildPayload: payloadBuilderFor(v1alpha1.KindAgent),
		}},
		Provider:   provider,
		Dimensions: 1536,
	})
	require.NoError(t, err)

	// First pass: one row, one generation.
	res, err := idx.Run(ctx, IndexOptions{}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, res.Stats[v1alpha1.KindAgent].Updated)
	require.Equal(t, 0, res.Stats[v1alpha1.KindAgent].Skipped)
	require.Equal(t, 1, provider.calls)

	// Second pass without force: checksum matches → skip, no provider call.
	res2, err := idx.Run(ctx, IndexOptions{}, nil)
	require.NoError(t, err)
	require.Equal(t, 0, res2.Stats[v1alpha1.KindAgent].Updated)
	require.Equal(t, 1, res2.Stats[v1alpha1.KindAgent].Skipped)
	require.Equal(t, 1, provider.calls, "provider should be skipped when checksum matches")
}

func TestIndexer_ForceRegeneratesAll(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, agentsTable)
	ctx := context.Background()

	_, err := agents.Upsert(ctx, testNS, "foo", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Title: "x"}), v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	provider := newDeterministicProvider()
	idx, err := NewIndexer(IndexerConfig{
		Bindings: []KindBinding{{
			Kind:         v1alpha1.KindAgent,
			Store:        agents,
			BuildPayload: payloadBuilderFor(v1alpha1.KindAgent),
		}},
		Provider:   provider,
		Dimensions: 1536,
	})
	require.NoError(t, err)

	_, err = idx.Run(ctx, IndexOptions{}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, provider.calls)

	_, err = idx.Run(ctx, IndexOptions{Force: true}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, provider.calls)
}

func TestIndexer_DryRunSkipsStoreWrites(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, agentsTable)
	ctx := context.Background()

	_, err := agents.Upsert(ctx, testNS, "foo", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Title: "x"}), v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	provider := newDeterministicProvider()
	idx, err := NewIndexer(IndexerConfig{
		Bindings: []KindBinding{{
			Kind:         v1alpha1.KindAgent,
			Store:        agents,
			BuildPayload: payloadBuilderFor(v1alpha1.KindAgent),
		}},
		Provider:   provider,
		Dimensions: 1536,
	})
	require.NoError(t, err)

	res, err := idx.Run(ctx, IndexOptions{DryRun: true}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, res.Stats[v1alpha1.KindAgent].Updated)
	require.Equal(t, 1, provider.calls)

	// Metadata stays nil because no write landed.
	meta, err := agents.GetEmbeddingMetadata(ctx, testNS, "foo", "v1")
	require.NoError(t, err)
	require.Nil(t, meta)
}

func TestIndexer_ProviderErrorIncrementsFailures(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, agentsTable)
	ctx := context.Background()

	for _, name := range []string{"good", "bad"} {
		_, err := agents.Upsert(ctx, testNS, name, "v1",
			mustSpec(t, v1alpha1.AgentSpec{Title: name}), v1alpha1store.UpsertOpts{})
		require.NoError(t, err)
	}

	provider := newDeterministicProvider()
	provider.failOn = "bad"
	idx, err := NewIndexer(IndexerConfig{
		Bindings: []KindBinding{{
			Kind:         v1alpha1.KindAgent,
			Store:        agents,
			BuildPayload: payloadBuilderFor(v1alpha1.KindAgent),
		}},
		Provider:   provider,
		Dimensions: 1536,
	})
	require.NoError(t, err)

	res, err := idx.Run(ctx, IndexOptions{}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, res.Stats[v1alpha1.KindAgent].Updated)
	require.Equal(t, 1, res.Stats[v1alpha1.KindAgent].Failures)
	require.Equal(t, 0, res.Stats[v1alpha1.KindAgent].Skipped)
}

func TestIndexer_ProgressCallbackInvoked(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, agentsTable)
	ctx := context.Background()

	_, err := agents.Upsert(ctx, testNS, "foo", "v1",
		mustSpec(t, v1alpha1.AgentSpec{}), v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	provider := newDeterministicProvider()
	idx, err := NewIndexer(IndexerConfig{
		Bindings: []KindBinding{{
			Kind:         v1alpha1.KindAgent,
			Store:        agents,
			BuildPayload: payloadBuilderFor(v1alpha1.KindAgent),
		}},
		Provider:   provider,
		Dimensions: 1536,
	})
	require.NoError(t, err)

	var got []IndexStats
	_, err = idx.Run(ctx, IndexOptions{}, func(kind string, stats IndexStats) {
		require.Equal(t, v1alpha1.KindAgent, kind)
		got = append(got, stats)
	})
	require.NoError(t, err)
	require.NotEmpty(t, got)
	require.Equal(t, 1, got[len(got)-1].Processed)
}

func TestIndexer_KindsFilter(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, agentsTable)
	mcpStore := v1alpha1store.NewStore(pool, "v1alpha1.mcp_servers")
	ctx := context.Background()

	_, err := agents.Upsert(ctx, testNS, "a", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Title: "a"}), v1alpha1store.UpsertOpts{})
	require.NoError(t, err)
	_, err = mcpStore.Upsert(ctx, testNS, "m", "v1",
		mustSpec(t, v1alpha1.MCPServerSpec{Title: "m"}), v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	idx, err := NewIndexer(IndexerConfig{
		Bindings: []KindBinding{
			{Kind: v1alpha1.KindAgent, Store: agents, BuildPayload: payloadBuilderFor(v1alpha1.KindAgent)},
			{Kind: v1alpha1.KindMCPServer, Store: mcpStore, BuildPayload: payloadBuilderFor(v1alpha1.KindMCPServer)},
		},
		Provider:   newDeterministicProvider(),
		Dimensions: 1536,
	})
	require.NoError(t, err)

	res, err := idx.Run(ctx, IndexOptions{Kinds: []string{v1alpha1.KindMCPServer}}, nil)
	require.NoError(t, err)
	_, hadAgents := res.Stats[v1alpha1.KindAgent]
	require.False(t, hadAgents, "filtered kind should not appear in Stats")
	require.Equal(t, 1, res.Stats[v1alpha1.KindMCPServer].Updated)
}

func TestPayloadBuilderFor_RemoteMCPServer(t *testing.T) {
	spec := mustSpec(t, v1alpha1.RemoteMCPServerSpec{
		Title:       "Remote Search",
		Description: "already running",
		Remote: v1alpha1.MCPTransport{
			Type: "streamable-http",
			URL:  "https://mcp.example.com",
		},
	})
	payload, err := payloadBuilderFor(v1alpha1.KindRemoteMCPServer)(&v1alpha1.RawObject{
		Metadata: v1alpha1.ObjectMeta{Name: "remote-search", Version: "v2"},
		Spec:     spec,
	})
	require.NoError(t, err)
	require.Contains(t, payload, "remote-search")
	require.Contains(t, payload, "Remote Search")
	require.Contains(t, payload, "already running")
	require.Contains(t, payload, "streamable-http")
	require.Contains(t, payload, "https://mcp.example.com")
}

func TestDefaultBindings_RequiresAllSearchableKinds(t *testing.T) {
	store := &v1alpha1store.Store{}
	_, err := DefaultBindings(map[string]*v1alpha1store.Store{
		v1alpha1.KindAgent:     store,
		v1alpha1.KindMCPServer: store,
		v1alpha1.KindSkill:     store,
		v1alpha1.KindPrompt:    store,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), v1alpha1.KindRemoteMCPServer)
}
