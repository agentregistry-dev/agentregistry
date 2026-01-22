package embeddings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// BuildServerEmbeddingPayload converts a server document into the canonical text payload
// used for semantic embeddings. The payload deliberately combines all metadata that
// describes the resource so checksum comparisons stay stable across systems.
func BuildServerEmbeddingPayload(server *apiv0.ServerJSON) string {
	if server == nil {
		return ""
	}

	var parts []string
	appendIf(&parts, server.Name, server.Title, server.Description, server.Version, server.WebsiteURL)
	appendJSON(&parts, server.Repository)
	appendJSONArray(&parts, server.Packages)
	appendJSONArray(&parts, server.Remotes)

	if server.Meta != nil && server.Meta.PublisherProvided != nil {
		appendJSON(&parts, server.Meta.PublisherProvided)
	}

	return strings.Join(parts, "\n")
}

// BuildAgentEmbeddingPayload mirrors BuildServerEmbeddingPayload but for AgentJSON entries.
func BuildAgentEmbeddingPayload(agent *models.AgentJSON) string {
	if agent == nil {
		return ""
	}

	var parts []string
	appendIf(&parts,
		agent.Name,
		agent.Title,
		agent.Description,
		agent.Version,
		agent.WebsiteURL,
		agent.Language,
		agent.Framework,
		agent.ModelProvider,
		agent.ModelName,
		agent.Image,
	)
	appendJSONArray(&parts, agent.McpServers)
	appendJSON(&parts, agent.Repository)
	appendJSONArray(&parts, agent.Packages)
	appendJSONArray(&parts, agent.Remotes)

	return strings.Join(parts, "\n")
}

// PayloadChecksum returns the deterministic checksum for an embedding payload.
func PayloadChecksum(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// GenerateSemanticEmbedding transforms the provided payload into a SemanticEmbedding
// by invoking the configured provider. The payload must be non-empty.
// When expectedDimensions > 0, the provider output is validated against it.
func GenerateSemanticEmbedding(ctx context.Context, provider Provider, payload string, expectedDimensions int) (*database.SemanticEmbedding, error) {
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

	generated := result.GeneratedAt
	if generated.IsZero() {
		generated = time.Now().UTC()
	}

	return &database.SemanticEmbedding{
		Vector:     result.Vector,
		Provider:   result.Provider,
		Model:      result.Model,
		Dimensions: dims,
		Checksum:   PayloadChecksum(payload),
		Generated:  generated,
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
	if data, err := json.Marshal(value); err == nil && len(data) > 0 {
		*parts = append(*parts, string(data))
	}
}

func appendJSONArray(parts *[]string, value any) {
	if value == nil {
		return
	}
	appendJSON(parts, value)
}
