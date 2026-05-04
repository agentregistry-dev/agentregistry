package embeddings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/semantic"
)

// BuildMCPServerEmbeddingPayload assembles the canonical text used to
// generate an MCPServer's semantic embedding. Deterministic across runs
// so the checksum can gate idempotent re-index passes.
//
// Enrichment annotations are intentionally excluded from the payload
// — they're scanner output, not user-authored search-relevant content.
func BuildMCPServerEmbeddingPayload(meta v1alpha1.ObjectMeta, spec v1alpha1.MCPServerSpec) string {
	var parts []string
	appendIf(&parts, meta.Name, spec.Title, spec.Description, meta.Version)
	var sourceRepo *v1alpha1.Repository
	var sourcePkg *v1alpha1.MCPPackage
	if spec.Source != nil {
		sourceRepo = spec.Source.Repository
		sourcePkg = spec.Source.Package
	}
	appendJSON(&parts, sourceRepo)
	appendJSON(&parts, sourcePkg)
	return strings.Join(parts, "\n")
}

// BuildRemoteMCPServerEmbeddingPayload assembles the canonical text for a
// RemoteMCPServer (already-running endpoint).
func BuildRemoteMCPServerEmbeddingPayload(meta v1alpha1.ObjectMeta, spec v1alpha1.RemoteMCPServerSpec) string {
	var parts []string
	appendIf(&parts, meta.Name, spec.Title, spec.Description, meta.Version, spec.Remote.URL, spec.Remote.Type)
	return strings.Join(parts, "\n")
}

// BuildAgentEmbeddingPayload assembles the canonical text for an Agent.
func BuildAgentEmbeddingPayload(meta v1alpha1.ObjectMeta, spec v1alpha1.AgentSpec) string {
	var parts []string
	var sourceImage string
	var sourceRepo *v1alpha1.Repository
	if spec.Source != nil {
		sourceImage = spec.Source.Image
		sourceRepo = spec.Source.Repository
	}
	appendIf(&parts,
		meta.Name,
		spec.Title,
		spec.Description,
		meta.Version,
		spec.Language,
		spec.Framework,
		spec.ModelProvider,
		spec.ModelName,
		sourceImage,
	)
	appendJSON(&parts, spec.MCPServers)
	appendJSON(&parts, spec.Skills)
	appendJSON(&parts, spec.Prompts)
	appendJSON(&parts, sourceRepo)
	return strings.Join(parts, "\n")
}

// BuildSkillEmbeddingPayload assembles the canonical text for a Skill.
func BuildSkillEmbeddingPayload(meta v1alpha1.ObjectMeta, spec v1alpha1.SkillSpec) string {
	var parts []string
	appendIf(&parts, meta.Name, spec.Title, spec.Description, meta.Version)
	appendJSON(&parts, spec.Source)
	return strings.Join(parts, "\n")
}

// BuildPromptEmbeddingPayload assembles the canonical text for a Prompt.
// Content is the interesting signal — include it verbatim so queries that
// match against prompt text actually find the prompt.
func BuildPromptEmbeddingPayload(meta v1alpha1.ObjectMeta, spec v1alpha1.PromptSpec) string {
	var parts []string
	appendIf(&parts, meta.Name, spec.Description, meta.Version, spec.Content)
	return strings.Join(parts, "\n")
}

// PayloadChecksum returns the deterministic checksum for an embedding
// payload. Callers use it as SemanticEmbedding.Checksum so the indexer
// can skip rows whose payload hasn't changed since the last index pass.
func PayloadChecksum(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// GenerateSemanticEmbedding runs the provider against the supplied
// payload and returns a fully-populated SemanticEmbedding ready for
// Store.SetEmbedding. The payload must be non-empty; when
// expectedDimensions > 0 the provider output is validated against it
// so schema mismatches surface early rather than at DB-write time.
func GenerateSemanticEmbedding(ctx context.Context, provider Provider, payload string, expectedDimensions int) (*semantic.SemanticEmbedding, error) {
	if provider == nil {
		return nil, errors.New("embedding provider is not configured")
	}
	if strings.TrimSpace(payload) == "" {
		return nil, errors.New("embedding payload is empty")
	}

	result, err := provider.Generate(ctx, Payload{Text: payload})
	if err != nil {
		return nil, err
	}

	dims := result.Dimensions
	if dims == 0 {
		dims = len(result.Vector)
	}
	if expectedDimensions > 0 && dims != expectedDimensions {
		return nil, fmt.Errorf("embedding dimensions mismatch: expected %d, got %d", expectedDimensions, dims)
	}

	// result.GeneratedAt is ignored here — Store.SetEmbedding stamps
	// semantic_embedding_generated_at with NOW() at write time, making
	// the provider's local timestamp redundant.

	return &semantic.SemanticEmbedding{
		Vector:     result.Vector,
		Provider:   result.Provider,
		Model:      result.Model,
		Dimensions: dims,
		Checksum:   PayloadChecksum(payload),
	}, nil
}

func appendIf(parts *[]string, values ...string) {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			*parts = append(*parts, v)
		}
	}
}

func appendJSON(parts *[]string, value any) {
	if value == nil {
		return
	}
	if data, err := json.Marshal(value); err == nil && len(data) > 0 && string(data) != "null" {
		*parts = append(*parts, string(data))
	}
}
