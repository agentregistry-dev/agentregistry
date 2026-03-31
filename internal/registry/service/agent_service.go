package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	api "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/validators"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// AgentService defines agent catalog and mutation operations.
type AgentService interface {
	// ListAgents retrieve all agents with optional filtering
	ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	// GetAgentByName retrieve latest version of an agent by name
	GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error)
	// GetAgentByNameAndVersion retrieve specific version of an agent by name and version
	GetAgentByNameAndVersion(ctx context.Context, agentName string, version string) (*models.AgentResponse, error)
	// GetAllVersionsByAgentName retrieve all versions of an agent by name
	GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error)
	// CreateAgent creates a new agent version
	CreateAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error)
	// ResolveAgentManifestSkills resolves manifest skill refs to concrete image or repo refs.
	ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]api.AgentSkillRef, error)
	// ResolveAgentManifestPrompts resolves manifest prompt refs to concrete prompt content.
	ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]api.ResolvedPrompt, error)
	// DeleteAgent permanently removes an agent version from the registry
	DeleteAgent(ctx context.Context, agentName, version string) error
	// UpsertAgentEmbedding stores semantic embedding metadata for an agent version
	UpsertAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error
	// GetAgentEmbeddingMetadata retrieves the embedding metadata for an agent version
	GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error)
}

// ListAgents returns registry entries for agents with pagination and filtering.
func (s *agentServiceImpl) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	if filter != nil {
		if err := s.ensureSemanticEmbedding(ctx, filter.Semantic); err != nil {
			return nil, "", err
		}
	}
	agents, next, err := s.readStores().agents.ListAgents(ctx, filter, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	return agents, next, nil
}

// GetAgentByName retrieves the latest version of an agent by its name.
func (s *agentServiceImpl) GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return s.readStores().agents.GetAgentByName(ctx, agentName)
}

// GetAgentByNameAndVersion retrieves a specific version of an agent by name and version.
func (s *agentServiceImpl) GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	return s.readStores().agents.GetAgentByNameAndVersion(ctx, agentName, version)
}

// GetAllVersionsByAgentName retrieves all versions for an agent.
func (s *agentServiceImpl) GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	return s.readStores().agents.GetAllVersionsByAgentName(ctx, agentName)
}

// CreateAgent creates a new agent version.
func (s *agentServiceImpl) CreateAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
	return inTransactionT(ctx, s, func(ctx context.Context, stores storeBundle) (*models.AgentResponse, error) {
		return s.createAgentInTransaction(ctx, stores.agents, req)
	})
}

func (s *agentServiceImpl) createAgentInTransaction(ctx context.Context, agents database.AgentStore, req *models.AgentJSON) (*models.AgentResponse, error) {
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid agent payload: name and version are required")
	}

	publishTime := time.Now()
	agentJSON := *req

	for _, remote := range agentJSON.Remotes {
		filter := &database.AgentFilter{RemoteURL: &remote.URL}
		existing, _, err := agents.ListAgents(ctx, filter, "", 1000)
		if err != nil {
			return nil, fmt.Errorf("failed to check remote URL conflict: %w", err)
		}
		for _, e := range existing {
			if e.Agent.Name != agentJSON.Name {
				return nil, fmt.Errorf("remote URL %s is already used by agent %s", remote.URL, e.Agent.Name)
			}
		}
	}

	versionCount, err := agents.CountAgentVersions(ctx, agentJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxServerVersionsPerServer {
		return nil, database.ErrMaxVersionsReached
	}

	exists, err := agents.CheckAgentVersionExists(ctx, agentJSON.Name, agentJSON.Version)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, database.ErrInvalidVersion
	}

	currentLatest, err := agents.GetCurrentLatestAgentVersion(ctx, agentJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	isNewLatest := true
	if currentLatest != nil {
		var existingPublishedAt time.Time
		if currentLatest.Meta.Official != nil {
			existingPublishedAt = currentLatest.Meta.Official.PublishedAt
		}
		if CompareVersions(agentJSON.Version, currentLatest.Agent.Version, publishTime, existingPublishedAt) <= 0 {
			isNewLatest = false
		}
	}

	if isNewLatest && currentLatest != nil {
		if err := agents.UnmarkAgentAsLatest(ctx, agentJSON.Name); err != nil {
			return nil, err
		}
	}

	officialMeta := &models.AgentRegistryExtensions{
		Status:      string(model.StatusActive),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	result, err := agents.CreateAgent(ctx, &agentJSON, officialMeta)
	if err != nil {
		return nil, err
	}

	if s.shouldGenerateEmbeddingsOnPublish() { //nolint:nestif
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

// DeleteAgent permanently removes an agent version from the registry.
func (s *agentServiceImpl) DeleteAgent(ctx context.Context, agentName, version string) error {
	return s.inTransaction(ctx, func(txCtx context.Context, stores storeBundle) error {
		return stores.agents.DeleteAgent(txCtx, agentName, version)
	})
}

func (s *agentServiceImpl) UpsertAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	return s.inTransaction(ctx, func(txCtx context.Context, stores storeBundle) error {
		return stores.agents.SetAgentEmbedding(txCtx, agentName, version, embedding)
	})
}

func (s *agentServiceImpl) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.readStores().agents.GetAgentEmbeddingMetadata(ctx, agentName, version)
}

// ResolveAgentManifestSkills resolves registry-type skill references from the agent manifest into concrete runtime refs.
func (s *agentServiceImpl) ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]api.AgentSkillRef, error) {
	if manifest == nil || len(manifest.Skills) == 0 {
		return nil, nil
	}

	var resolved []api.AgentSkillRef
	for _, skill := range manifest.Skills {
		ref, err := s.resolveSkillRef(ctx, skill)
		if err != nil {
			return nil, fmt.Errorf("resolve skill %q: %w", skill.Name, err)
		}
		resolved = append(resolved, ref)
	}
	return resolved, nil
}

func (s *agentServiceImpl) resolveSkillRef(ctx context.Context, skill models.SkillRef) (api.AgentSkillRef, error) {
	image := strings.TrimSpace(skill.Image)
	registrySkillName := strings.TrimSpace(skill.RegistrySkillName)
	hasImage := image != ""
	hasRegistry := registrySkillName != ""

	if !hasImage && !hasRegistry {
		return api.AgentSkillRef{}, fmt.Errorf("one of image or registrySkillName is required")
	}
	if hasImage && hasRegistry {
		return api.AgentSkillRef{}, fmt.Errorf("only one of image or registrySkillName may be set")
	}

	if hasImage {
		return api.AgentSkillRef{Name: skill.Name, Image: image}, nil
	}

	version := strings.TrimSpace(skill.RegistrySkillVersion)
	if version == "" {
		version = "latest"
	}

	skillResp, err := s.GetSkillByNameAndVersion(ctx, registrySkillName, version)
	if err != nil {
		return api.AgentSkillRef{}, fmt.Errorf("fetch skill %q version %q: %w", registrySkillName, version, err)
	}

	for _, pkg := range skillResp.Skill.Packages {
		typ := strings.ToLower(strings.TrimSpace(pkg.RegistryType))
		if (typ == "docker" || typ == "oci") && strings.TrimSpace(pkg.Identifier) != "" {
			return api.AgentSkillRef{Name: skill.Name, Image: strings.TrimSpace(pkg.Identifier)}, nil
		}
	}

	if skillResp.Skill.Repository != nil &&
		strings.EqualFold(skillResp.Skill.Repository.Source, string(validators.SourceGit)) &&
		strings.TrimSpace(skillResp.Skill.Repository.URL) != "" {
		return api.AgentSkillRef{
			Name:    skill.Name,
			RepoURL: strings.TrimSpace(skillResp.Skill.Repository.URL),
		}, nil
	}

	return api.AgentSkillRef{}, fmt.Errorf("skill %q (version %s): no docker/oci package or git repository found", registrySkillName, version)
}

// ResolveAgentManifestPrompts resolves registry-type prompt references from the agent manifest into concrete prompt content.
func (s *agentServiceImpl) ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]api.ResolvedPrompt, error) {
	if manifest == nil || len(manifest.Prompts) == 0 {
		return nil, nil
	}

	resolved := make([]api.ResolvedPrompt, 0, len(manifest.Prompts))
	for _, ref := range manifest.Prompts {
		promptName := strings.TrimSpace(ref.RegistryPromptName)
		if promptName == "" {
			return nil, fmt.Errorf("prompt name is required")
		}

		version := strings.TrimSpace(ref.RegistryPromptVersion)

		var promptResp *models.PromptResponse
		var err error
		if version == "" || version == "latest" {
			promptResp, err = s.GetPromptByName(ctx, promptName)
		} else {
			promptResp, err = s.GetPromptByNameAndVersion(ctx, promptName, version)
		}
		if err != nil {
			return nil, fmt.Errorf("resolve prompt %q version %q: %w", promptName, version, err)
		}

		displayName := ref.Name
		if displayName == "" {
			displayName = promptName
		}
		resolved = append(resolved, api.ResolvedPrompt{
			Name:    displayName,
			Content: promptResp.Prompt.Content,
		})
	}
	return resolved, nil
}
