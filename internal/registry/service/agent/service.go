package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/embeddingutil"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/txutil"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/versionutil"
	"github.com/agentregistry-dev/agentregistry/internal/registry/validators"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const maxVersionsPerAgent = 10000

type Dependencies struct {
	StoreDB            database.Store
	Config             *config.Config
	EmbeddingsProvider embeddings.Provider
	Logger             *slog.Logger
}

type Registry interface {
	ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error)
	GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error)
	GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error)
	CreateAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error)
	DeleteAgent(ctx context.Context, agentName, version string) error
	UpsertAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error
	GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error)
	ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error)
	ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error)
}

type Service struct {
	storeDB            database.Store
	cfg                *config.Config
	embeddingsProvider embeddings.Provider
	logger             *slog.Logger
}

var _ Registry = (*Service)(nil)

func New(deps Dependencies) Registry {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default().With("component", "registry.agent")
	}

	return &Service{
		storeDB:            deps.StoreDB,
		cfg:                deps.Config,
		embeddingsProvider: deps.EmbeddingsProvider,
		logger:             logger,
	}
}

func (s *Service) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	if filter != nil {
		if err := embeddingutil.EnsureQueryEmbedding(ctx, s.cfg, s.embeddingsProvider, filter.Semantic); err != nil {
			return nil, "", err
		}
	}
	return s.storeDB.ListAgents(ctx, filter, cursor, limit)
}

func (s *Service) GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return s.storeDB.GetAgentByName(ctx, agentName)
}

func (s *Service) GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	return s.storeDB.GetAgentByNameAndVersion(ctx, agentName, version)
}

func (s *Service) GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	return s.storeDB.GetAllVersionsByAgentName(ctx, agentName)
}

func (s *Service) CreateAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
	return txutil.RunT(ctx, s.storeDB, func(txCtx context.Context, store database.Store) (*models.AgentResponse, error) {
		return s.createAgentInTransaction(txCtx, store, req)
	})
}

func (s *Service) DeleteAgent(ctx context.Context, agentName, version string) error {
	return txutil.Run(ctx, s.storeDB, func(txCtx context.Context, store database.Store) error {
		return store.DeleteAgent(txCtx, agentName, version)
	})
}

func (s *Service) UpsertAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	return txutil.Run(ctx, s.storeDB, func(txCtx context.Context, store database.Store) error {
		return store.SetAgentEmbedding(txCtx, agentName, version, embedding)
	})
}

func (s *Service) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.storeDB.GetAgentEmbeddingMetadata(ctx, agentName, version)
}

func (s *Service) ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error) {
	if manifest == nil || len(manifest.Skills) == 0 {
		return nil, nil
	}

	resolved := make([]platformtypes.AgentSkillRef, 0, len(manifest.Skills))
	for _, skill := range manifest.Skills {
		ref, err := s.resolveSkillRef(ctx, skill)
		if err != nil {
			return nil, fmt.Errorf("resolve skill %q: %w", skill.Name, err)
		}
		resolved = append(resolved, ref)
	}
	return resolved, nil
}

func (s *Service) ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
	if manifest == nil || len(manifest.Prompts) == 0 {
		return nil, nil
	}

	resolved := make([]platformtypes.ResolvedPrompt, 0, len(manifest.Prompts))
	for _, ref := range manifest.Prompts {
		promptName := strings.TrimSpace(ref.RegistryPromptName)
		if promptName == "" {
			return nil, fmt.Errorf("prompt name is required")
		}

		version := strings.TrimSpace(ref.RegistryPromptVersion)

		var promptResp *models.PromptResponse
		var err error
		if version == "" || version == "latest" {
			promptResp, err = s.storeDB.GetPromptByName(ctx, promptName)
		} else {
			promptResp, err = s.storeDB.GetPromptByNameAndVersion(ctx, promptName, version)
		}
		if err != nil {
			return nil, fmt.Errorf("resolve prompt %q version %q: %w", promptName, version, err)
		}

		displayName := ref.Name
		if displayName == "" {
			displayName = promptName
		}
		resolved = append(resolved, platformtypes.ResolvedPrompt{
			Name:    displayName,
			Content: promptResp.Prompt.Content,
		})
	}

	return resolved, nil
}

func (s *Service) createAgentInTransaction(ctx context.Context, store database.Store, req *models.AgentJSON) (*models.AgentResponse, error) {
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid agent payload: name and version are required")
	}

	publishTime := time.Now()
	agentJSON := *req

	for _, remote := range agentJSON.Remotes {
		filter := &database.AgentFilter{RemoteURL: &remote.URL}
		existing, _, err := store.ListAgents(ctx, filter, "", 1000)
		if err != nil {
			return nil, fmt.Errorf("failed to check remote URL conflict: %w", err)
		}
		for _, existingAgent := range existing {
			if existingAgent.Agent.Name != agentJSON.Name {
				return nil, fmt.Errorf("remote URL %s is already used by agent %s", remote.URL, existingAgent.Agent.Name)
			}
		}
	}

	versionCount, err := store.CountAgentVersions(ctx, agentJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxVersionsPerAgent {
		return nil, database.ErrMaxVersionsReached
	}

	exists, err := store.CheckAgentVersionExists(ctx, agentJSON.Name, agentJSON.Version)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, database.ErrInvalidVersion
	}

	currentLatest, err := store.GetCurrentLatestAgentVersion(ctx, agentJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	isNewLatest := true
	if currentLatest != nil {
		var existingPublishedAt time.Time
		if currentLatest.Meta.Official != nil {
			existingPublishedAt = currentLatest.Meta.Official.PublishedAt
		}
		if versionutil.CompareVersions(agentJSON.Version, currentLatest.Agent.Version, publishTime, existingPublishedAt) <= 0 {
			isNewLatest = false
		}
	}

	if isNewLatest && currentLatest != nil {
		if err := store.UnmarkAgentAsLatest(ctx, agentJSON.Name); err != nil {
			return nil, err
		}
	}

	officialMeta := &models.AgentRegistryExtensions{
		Status:      string(model.StatusActive),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	result, err := store.CreateAgent(ctx, &agentJSON, officialMeta)
	if err != nil {
		return nil, err
	}

	if embeddingutil.EnabledOnPublish(s.cfg, s.embeddingsProvider) { //nolint:nestif
		go func() {
			bgCtx := context.Background()
			payload := embeddings.BuildAgentEmbeddingPayload(&agentJSON)
			if strings.TrimSpace(payload) == "" {
				return
			}
			embedding, err := embeddings.GenerateSemanticEmbedding(bgCtx, s.embeddingsProvider, payload, s.cfg.Embeddings.Dimensions)
			if err != nil {
				s.logger.Warn("failed to generate embedding for agent", "name", agentJSON.Name, "version", agentJSON.Version, "error", err)
			} else if embedding != nil {
				if err := s.UpsertAgentEmbedding(bgCtx, agentJSON.Name, agentJSON.Version, embedding); err != nil {
					s.logger.Warn("failed to store embedding for agent", "name", agentJSON.Name, "version", agentJSON.Version, "error", err)
				}
			}
		}()
	}

	return result, nil
}

func (s *Service) resolveSkillRef(ctx context.Context, skill models.SkillRef) (platformtypes.AgentSkillRef, error) {
	image := strings.TrimSpace(skill.Image)
	registrySkillName := strings.TrimSpace(skill.RegistrySkillName)
	hasImage := image != ""
	hasRegistry := registrySkillName != ""

	if !hasImage && !hasRegistry {
		return platformtypes.AgentSkillRef{}, fmt.Errorf("one of image or registrySkillName is required")
	}
	if hasImage && hasRegistry {
		return platformtypes.AgentSkillRef{}, fmt.Errorf("only one of image or registrySkillName may be set")
	}

	if hasImage {
		return platformtypes.AgentSkillRef{Name: skill.Name, Image: image}, nil
	}

	version := strings.TrimSpace(skill.RegistrySkillVersion)
	if version == "" {
		version = "latest"
	}

	skillResp, err := s.storeDB.GetSkillByNameAndVersion(ctx, registrySkillName, version)
	if err != nil {
		return platformtypes.AgentSkillRef{}, fmt.Errorf("fetch skill %q version %q: %w", registrySkillName, version, err)
	}

	for _, pkg := range skillResp.Skill.Packages {
		typ := strings.ToLower(strings.TrimSpace(pkg.RegistryType))
		if (typ == "docker" || typ == "oci") && strings.TrimSpace(pkg.Identifier) != "" {
			return platformtypes.AgentSkillRef{Name: skill.Name, Image: strings.TrimSpace(pkg.Identifier)}, nil
		}
	}

	if skillResp.Skill.Repository != nil &&
		strings.EqualFold(skillResp.Skill.Repository.Source, string(validators.SourceGit)) &&
		strings.TrimSpace(skillResp.Skill.Repository.URL) != "" {
		return platformtypes.AgentSkillRef{
			Name:    skill.Name,
			RepoURL: strings.TrimSpace(skillResp.Skill.Repository.URL),
		}, nil
	}

	return platformtypes.AgentSkillRef{}, fmt.Errorf(
		"skill %q (version %s): no docker/oci package or git repository found",
		registrySkillName,
		version,
	)
}
