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
	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/versionutil"
	"github.com/agentregistry-dev/agentregistry/internal/registry/validators"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const maxVersionsPerAgent = 10000

type Dependencies struct {
	StoreDB            database.Store
	Agents             database.AgentStore
	Skills             database.SkillStore
	Prompts            database.PromptStore
	Tx                 database.Transactor
	Config             *config.Config
	EmbeddingsProvider embeddings.Provider
	Logger             *slog.Logger
}

type Registry interface {
	database.AgentReader
	PublishAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error)
	DeleteAgent(ctx context.Context, agentName, version string) error
	SetAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error
	ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error)
	ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error)
}

type registry struct {
	database.AgentStore
	skills             database.SkillStore
	prompts            database.PromptStore
	tx                 database.Transactor
	cfg                *config.Config
	embeddingsProvider embeddings.Provider
	logger             *slog.Logger
}

var _ Registry = (*registry)(nil)

func New(deps Dependencies) Registry {
	if deps.Agents == nil && deps.StoreDB != nil {
		deps.Agents = deps.StoreDB.Agents()
	}
	if deps.Skills == nil && deps.StoreDB != nil {
		deps.Skills = deps.StoreDB.Skills()
	}
	if deps.Prompts == nil && deps.StoreDB != nil {
		deps.Prompts = deps.StoreDB.Prompts()
	}
	if deps.Tx == nil {
		deps.Tx = deps.StoreDB
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default().With("component", "registry.agent")
	}

	return &registry{
		AgentStore:         deps.Agents,
		skills:             deps.Skills,
		prompts:            deps.Prompts,
		tx:                 deps.Tx,
		cfg:                deps.Config,
		embeddingsProvider: deps.EmbeddingsProvider,
		logger:             logger,
	}
}

func (s *registry) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	if filter != nil {
		if err := embeddingutil.EnsureQueryEmbedding(ctx, s.cfg, s.embeddingsProvider, filter.Semantic); err != nil {
			return nil, "", err
		}
	}
	return s.AgentStore.ListAgents(ctx, filter, cursor, limit)
}

func (s *registry) PublishAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
	return database.InTransactionT(ctx, s.tx, func(txCtx context.Context, scope database.Scope) (*models.AgentResponse, error) {
		return s.createAgentInTransaction(txCtx, scope.Agents(), req)
	})
}

func (s *registry) DeleteAgent(ctx context.Context, agentName, version string) error {
	return database.InTransaction(ctx, s.tx, func(txCtx context.Context, scope database.Scope) error {
		return scope.Agents().DeleteAgent(txCtx, agentName, version)
	})
}

func (s *registry) SetAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	return database.InTransaction(ctx, s.tx, func(txCtx context.Context, scope database.Scope) error {
		return scope.Agents().SetAgentEmbedding(txCtx, agentName, version, embedding)
	})
}

func (s *registry) ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error) {
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

func (s *registry) ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
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
			promptResp, err = s.prompts.GetPrompt(ctx, promptName)
		} else {
			promptResp, err = s.prompts.GetPromptVersion(ctx, promptName, version)
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

func (s *registry) createAgentInTransaction(ctx context.Context, agents database.AgentStore, req *models.AgentJSON) (*models.AgentResponse, error) {
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
		for _, existingAgent := range existing {
			if existingAgent.Agent.Name != agentJSON.Name {
				return nil, fmt.Errorf("remote URL %s is already used by agent %s", remote.URL, existingAgent.Agent.Name)
			}
		}
	}

	versionCount, err := agents.CountAgentVersions(ctx, agentJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxVersionsPerAgent {
		return nil, database.ErrMaxVersionsReached
	}

	exists, err := agents.CheckAgentVersionExists(ctx, agentJSON.Name, agentJSON.Version)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, database.ErrInvalidVersion
	}

	currentLatest, err := agents.GetLatestAgent(ctx, agentJSON.Name)
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
				if err := s.SetAgentEmbedding(bgCtx, agentJSON.Name, agentJSON.Version, embedding); err != nil {
					s.logger.Warn("failed to store embedding for agent", "name", agentJSON.Name, "version", agentJSON.Version, "error", err)
				}
			}
		}()
	}

	return result, nil
}

func (s *registry) resolveSkillRef(ctx context.Context, skill models.SkillRef) (platformtypes.AgentSkillRef, error) {
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

	skillResp, err := s.skills.GetSkillVersion(ctx, registrySkillName, version)
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
