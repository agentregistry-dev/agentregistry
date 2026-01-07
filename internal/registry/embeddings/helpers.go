package embeddings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

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
	appendIf := func(values ...string) {
		for _, v := range values {
			if strings.TrimSpace(v) != "" {
				parts = append(parts, v)
			}
		}
	}

	appendIf(server.Name, server.Title, server.Description, server.Version, server.WebsiteURL)

	if server.Repository != nil {
		if repoJSON, err := json.Marshal(server.Repository); err == nil {
			parts = append(parts, string(repoJSON))
		}
	}

	if len(server.Packages) > 0 {
		if pkgJSON, err := json.Marshal(server.Packages); err == nil {
			parts = append(parts, string(pkgJSON))
		}
	}

	if len(server.Remotes) > 0 {
		if remotesJSON, err := json.Marshal(server.Remotes); err == nil {
			parts = append(parts, string(remotesJSON))
		}
	}

	if server.Meta != nil && server.Meta.PublisherProvided != nil {
		if metaJSON, err := json.Marshal(server.Meta.PublisherProvided); err == nil {
			parts = append(parts, string(metaJSON))
		}
	}

	return strings.Join(parts, "\n")
}

// PayloadChecksum returns the deterministic checksum for an embedding payload.
func PayloadChecksum(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// GenerateSemanticEmbedding transforms the provided payload into a SemanticEmbedding
// by invoking the configured provider. The payload must be non-empty.
func GenerateSemanticEmbedding(ctx context.Context, provider Provider, payload string) (*database.SemanticEmbedding, error) {
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
