package embeddingutil

import (
	"context"
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

func EnabledOnPublish(cfg *config.Config, provider embeddings.Provider) bool {
	return cfg != nil && cfg.Embeddings.Enabled && cfg.Embeddings.OnPublish && provider != nil
}

func EnsureQueryEmbedding(
	ctx context.Context,
	cfg *config.Config,
	provider embeddings.Provider,
	opts *database.SemanticSearchOptions,
) error {
	if opts == nil {
		return nil
	}
	if len(opts.QueryEmbedding) > 0 {
		return nil
	}
	if strings.TrimSpace(opts.RawQuery) == "" {
		return fmt.Errorf("%w: semantic search requires a non-empty search string", database.ErrInvalidInput)
	}
	if provider == nil {
		return fmt.Errorf("%w: semantic search provider is not configured", database.ErrInvalidInput)
	}

	result, err := provider.Generate(ctx, embeddings.Payload{Text: opts.RawQuery})
	if err != nil {
		return fmt.Errorf("failed to generate semantic embedding: %w", err)
	}

	if cfg != nil && cfg.Embeddings.Dimensions > 0 && len(result.Vector) != cfg.Embeddings.Dimensions {
		return fmt.Errorf(
			"%w: embedding dimensions mismatch (expected %d, got %d)",
			database.ErrInvalidInput,
			cfg.Embeddings.Dimensions,
			len(result.Vector),
		)
	}

	opts.QueryEmbedding = result.Vector
	return nil
}
