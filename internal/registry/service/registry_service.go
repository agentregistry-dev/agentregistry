package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
)

const (
	maxServerVersionsPerServer = 10000

	resourceTypeMCP   = "mcp"
	resourceTypeAgent = "agent"
	originDiscovered  = "discovered"
)

// UnsupportedDeploymentPlatformError is returned when no deployment adapter is
// registered for a provider platform.
type UnsupportedDeploymentPlatformError struct {
	Platform string
}

func (e *UnsupportedDeploymentPlatformError) Error() string {
	platform := strings.TrimSpace(e.Platform)
	if platform == "" {
		platform = "unknown"
	}
	return fmt.Sprintf("unsupported deployment platform: %s", platform)
}

func (e *UnsupportedDeploymentPlatformError) Unwrap() error {
	return database.ErrInvalidInput
}

// IsUnsupportedDeploymentPlatformError reports whether err indicates an
// unsupported deployment platform.
func IsUnsupportedDeploymentPlatformError(err error) bool {
	var target *UnsupportedDeploymentPlatformError
	return errors.As(err, &target)
}

// registryServiceImpl implements the RegistryService interface using our Database.
type registryServiceImpl struct {
	storeDB            database.ServiceDatabase
	serverRepo         database.ServerStore
	agentRepo          database.AgentStore
	skillRepo          database.SkillStore
	promptRepo         database.PromptStore
	providerRepo       database.ProviderStore
	deploymentRepo     database.DeploymentStore
	cfg                *config.Config
	embeddingsProvider embeddings.Provider
	deploymentAdapters map[string]registrytypes.DeploymentPlatformAdapter
	logger             *slog.Logger
}

// DeploymentPlatformStaleCleaner is an optional adapter hook for stale deployment replacement.
type DeploymentPlatformStaleCleaner interface {
	CleanupStale(ctx context.Context, deployment *models.Deployment) error
}

// NewRegistryService creates a new registry service with the provided database and configuration.
func NewRegistryService(
	storeDB database.ServiceDatabase,
	cfg *config.Config,
	embeddingProvider embeddings.Provider,
) RegistryService {
	return &registryServiceImpl{
		storeDB:            storeDB,
		serverRepo:         storeDB,
		agentRepo:          storeDB,
		skillRepo:          storeDB,
		promptRepo:         storeDB,
		providerRepo:       storeDB,
		deploymentRepo:     storeDB,
		cfg:                cfg,
		embeddingsProvider: embeddingProvider,
		logger:             slog.Default().With("component", "registry"),
	}
}

// SetPlatformAdapters wires platform extension adapters into the service.
func (s *registryServiceImpl) SetPlatformAdapters(
	deploymentPlatforms map[string]registrytypes.DeploymentPlatformAdapter,
) {
	s.deploymentAdapters = deploymentPlatforms
}

func (s *registryServiceImpl) resolveDeploymentAdapter(platform string) (registrytypes.DeploymentPlatformAdapter, error) {
	providerPlatform := strings.ToLower(strings.TrimSpace(platform))
	if providerPlatform == "" {
		return nil, fmt.Errorf("%w: deployment platform is required", database.ErrInvalidInput)
	}
	adapter, ok := s.deploymentAdapters[providerPlatform]
	if !ok {
		return nil, &UnsupportedDeploymentPlatformError{Platform: providerPlatform}
	}
	return adapter, nil
}

// shouldGenerateEmbeddingsOnPublish returns true if embeddings should be generated when resources are created.
func (s *registryServiceImpl) shouldGenerateEmbeddingsOnPublish() bool {
	return s.cfg != nil && s.cfg.Embeddings.Enabled && s.cfg.Embeddings.OnPublish && s.embeddingsProvider != nil
}

func (s *registryServiceImpl) ensureSemanticEmbedding(ctx context.Context, opts *database.SemanticSearchOptions) error {
	if opts == nil {
		return nil
	}
	if len(opts.QueryEmbedding) > 0 {
		return nil
	}
	if strings.TrimSpace(opts.RawQuery) == "" {
		return fmt.Errorf("%w: semantic search requires a non-empty search string", database.ErrInvalidInput)
	}
	if s.embeddingsProvider == nil {
		return fmt.Errorf("%w: semantic search provider is not configured", database.ErrInvalidInput)
	}

	result, err := s.embeddingsProvider.Generate(ctx, embeddings.Payload{
		Text: opts.RawQuery,
	})
	if err != nil {
		return fmt.Errorf("failed to generate semantic embedding: %w", err)
	}

	if s.cfg != nil && s.cfg.Embeddings.Dimensions > 0 && len(result.Vector) != s.cfg.Embeddings.Dimensions {
		return fmt.Errorf("%w: embedding dimensions mismatch (expected %d, got %d)", database.ErrInvalidInput, s.cfg.Embeddings.Dimensions, len(result.Vector))
	}

	opts.QueryEmbedding = result.Vector
	return nil
}
