package embeddings

import (
	"context"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// OnPublishService generates embeddings when resources are published.
type OnPublishService struct {
	provider   Provider
	dimensions int
	enabled    bool
}

// NewOnPublishService creates a new on-publish embedding service.
func NewOnPublishService(provider Provider, dimensions int, enabled bool) *OnPublishService {
	return &OnPublishService{
		provider:   provider,
		dimensions: dimensions,
		enabled:    enabled,
	}
}

// IsEnabled returns whether on-publish embedding generation is enabled.
func (s *OnPublishService) IsEnabled() bool {
	return s.enabled && s.provider != nil
}

// GenerateServerEmbedding generates a semantic embedding for a server.
// Returns nil if the payload is empty or the service is not enabled.
func (s *OnPublishService) GenerateServerEmbedding(ctx context.Context, server *apiv0.ServerJSON) (*database.SemanticEmbedding, error) {
	if !s.IsEnabled() || server == nil {
		return nil, nil
	}

	payload := BuildServerEmbeddingPayload(server)
	if strings.TrimSpace(payload) == "" {
		return nil, nil
	}

	return GenerateSemanticEmbedding(ctx, s.provider, payload, s.dimensions)
}

// GenerateAgentEmbedding generates a semantic embedding for an agent.
// Returns nil if the payload is empty or the service is not enabled.
func (s *OnPublishService) GenerateAgentEmbedding(ctx context.Context, agent *models.AgentJSON) (*database.SemanticEmbedding, error) {
	if !s.IsEnabled() || agent == nil {
		return nil, nil
	}

	payload := BuildAgentEmbeddingPayload(agent)
	if strings.TrimSpace(payload) == "" {
		return nil, nil
	}

	return GenerateSemanticEmbedding(ctx, s.provider, payload, s.dimensions)
}
