//go:build unit

package embeddings

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestBuildMCPServerEmbeddingPayload_StableAcrossRuns(t *testing.T) {
	meta := v1alpha1.ObjectMeta{Namespace: "default", Name: "foo", Version: "v1"}
	spec := v1alpha1.MCPServerSpec{
		Title:       "Foo",
		Description: "demo server",
	}
	a := BuildMCPServerEmbeddingPayload(meta, spec)
	b := BuildMCPServerEmbeddingPayload(meta, spec)
	require.Equal(t, a, b)

	// Field values appear in the payload so downstream checksum shifts
	// when they change.
	require.Contains(t, a, "foo")
	require.Contains(t, a, "Foo")
	require.Contains(t, a, "demo server")
}

func TestBuildMCPServerEmbeddingPayload_SkipsEmptyFields(t *testing.T) {
	out := BuildMCPServerEmbeddingPayload(
		v1alpha1.ObjectMeta{Name: "only-name"},
		v1alpha1.MCPServerSpec{},
	)
	lines := strings.Split(out, "\n")
	require.Equal(t, []string{"only-name"}, lines)
}

func TestBuildRemoteMCPServerEmbeddingPayload_IncludesRemoteEndpoint(t *testing.T) {
	out := BuildRemoteMCPServerEmbeddingPayload(
		v1alpha1.ObjectMeta{Name: "remote-search", Version: "v2"},
		v1alpha1.RemoteMCPServerSpec{
			Title:       "Remote Search",
			Description: "already running",
			Remote: v1alpha1.MCPTransport{
				Type: "streamable-http",
				URL:  "https://mcp.example.com",
			},
		},
	)

	require.Contains(t, out, "remote-search")
	require.Contains(t, out, "Remote Search")
	require.Contains(t, out, "already running")
	require.Contains(t, out, "v2")
	require.Contains(t, out, "streamable-http")
	require.Contains(t, out, "https://mcp.example.com")
}

func TestBuildAgentEmbeddingPayload_IncludesModelFields(t *testing.T) {
	out := BuildAgentEmbeddingPayload(
		v1alpha1.ObjectMeta{Name: "scorer"},
		v1alpha1.AgentSpec{
			Title:         "Scorer",
			ModelProvider: "openai",
			ModelName:     "gpt-4o",
			Framework:     "langchain",
		},
	)
	require.Contains(t, out, "openai")
	require.Contains(t, out, "gpt-4o")
	require.Contains(t, out, "langchain")
}

func TestBuildPromptEmbeddingPayload_IncludesContent(t *testing.T) {
	out := BuildPromptEmbeddingPayload(
		v1alpha1.ObjectMeta{Name: "greet"},
		v1alpha1.PromptSpec{
			Description: "friendly hello",
			Content:     "Hello, {{name}}!",
		},
	)
	require.Contains(t, out, "Hello, {{name}}!")
	require.Contains(t, out, "friendly hello")
}

func TestPayloadChecksum_Stable(t *testing.T) {
	a := PayloadChecksum("hello")
	b := PayloadChecksum("hello")
	require.Equal(t, a, b)
	require.NotEqual(t, a, PayloadChecksum("world"))
}

type stubProvider struct {
	vector     []float32
	dims       int
	err        error
	calledWith Payload
}

func (s *stubProvider) Generate(ctx context.Context, p Payload) (*Result, error) {
	s.calledWith = p
	if s.err != nil {
		return nil, s.err
	}
	return &Result{
		Vector:     s.vector,
		Provider:   "stub",
		Model:      "model-x",
		Dimensions: s.dims,
	}, nil
}

func TestGenerateSemanticEmbedding_Success(t *testing.T) {
	p := &stubProvider{vector: []float32{0.1, 0.2, 0.3}, dims: 3}
	emb, err := GenerateSemanticEmbedding(context.Background(), p, "some text", 3)
	require.NoError(t, err)
	require.Equal(t, 3, emb.Dimensions)
	require.Equal(t, "stub", emb.Provider)
	require.Equal(t, "model-x", emb.Model)
	require.NotEmpty(t, emb.Checksum)
	require.Equal(t, "some text", p.calledWith.Text)
}

func TestGenerateSemanticEmbedding_DimensionsMismatchErrors(t *testing.T) {
	p := &stubProvider{vector: []float32{0.1, 0.2}, dims: 2}
	_, err := GenerateSemanticEmbedding(context.Background(), p, "text", 3)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dimensions mismatch")
}

func TestGenerateSemanticEmbedding_ProviderError(t *testing.T) {
	p := &stubProvider{err: errors.New("boom")}
	_, err := GenerateSemanticEmbedding(context.Background(), p, "text", 0)
	require.ErrorContains(t, err, "boom")
}

func TestGenerateSemanticEmbedding_RejectsEmptyPayload(t *testing.T) {
	_, err := GenerateSemanticEmbedding(context.Background(), &stubProvider{}, "   ", 0)
	require.Error(t, err)
}

func TestGenerateSemanticEmbedding_RejectsNilProvider(t *testing.T) {
	_, err := GenerateSemanticEmbedding(context.Background(), nil, "text", 0)
	require.Error(t, err)
}
