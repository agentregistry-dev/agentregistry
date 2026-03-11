package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	api "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/validators"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/jackc/pgx/v5"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
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
	db                 database.Database
	cfg                *config.Config
	embeddingsProvider embeddings.Provider
	deploymentAdapters map[string]registrytypes.DeploymentPlatformAdapter
	logger             *slog.Logger
}

// DeploymentPlatformStaleCleaner is an optional adapter hook for stale deployment replacement.
type DeploymentPlatformStaleCleaner interface {
	CleanupStale(ctx context.Context, deployment *models.Deployment) error
}

// NewRegistryService creates a new registry service with the provided database and configuration
func NewRegistryService(
	db database.Database,
	cfg *config.Config,
	embeddingProvider embeddings.Provider,
) RegistryService {
	return &registryServiceImpl{
		db:                 db,
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

// ListServers returns registry entries with cursor-based pagination and optional filtering
func (s *registryServiceImpl) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	// If limit is not set or negative, use a default limit
	if limit <= 0 {
		limit = 30
	}

	if filter != nil {
		if err := s.ensureSemanticEmbedding(ctx, filter.Semantic); err != nil {
			return nil, "", err
		}
	}

	// Use the database's ListServers method with pagination and filtering
	serverRecords, nextCursor, err := s.db.ListServers(ctx, nil, filter, cursor, limit)
	if err != nil {
		return nil, "", err
	}

	return serverRecords, nextCursor, nil
}

// GetServerByName retrieves the latest version of a server by its server name
func (s *registryServiceImpl) GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	serverRecord, err := s.db.GetServerByName(ctx, nil, serverName)
	if err != nil {
		return nil, err
	}

	return serverRecord, nil
}

// GetServerByNameAndVersion retrieves a specific version of a server by server name and version
func (s *registryServiceImpl) GetServerByNameAndVersion(ctx context.Context, serverName string, version string) (*apiv0.ServerResponse, error) {
	serverRecord, err := s.db.GetServerByNameAndVersion(ctx, nil, serverName, version)
	if err != nil {
		return nil, err
	}

	return serverRecord, nil
}

// GetAllVersionsByServerName retrieves all versions of a server by server name
func (s *registryServiceImpl) GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	serverRecords, err := s.db.GetAllVersionsByServerName(ctx, nil, serverName)
	if err != nil {
		return nil, err
	}

	return serverRecords, nil
}

// CreateServer creates a new server version
func (s *registryServiceImpl) CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	// Wrap the entire operation in a transaction
	return database.InTransactionT(ctx, s.db, func(ctx context.Context, tx pgx.Tx) (*apiv0.ServerResponse, error) {
		return s.createServerInTransaction(ctx, tx, req)
	})
}

// createServerInTransaction contains the actual CreateServer logic within a transaction
func (s *registryServiceImpl) createServerInTransaction(ctx context.Context, tx pgx.Tx, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	// Validate the request
	if err := validators.ValidatePublishRequest(ctx, *req, s.cfg); err != nil {
		return nil, err
	}

	publishTime := time.Now()
	serverJSON := *req

	// Serialize concurrent creates for the same server to avoid idx_unique_latest_per_server violations
	if err := s.db.AcquireServerCreateLock(ctx, tx, serverJSON.Name); err != nil {
		return nil, err
	}

	// Check for duplicate remote URLs
	if err := s.validateNoDuplicateRemoteURLs(ctx, tx, serverJSON); err != nil {
		return nil, err
	}

	// Check we haven't exceeded the maximum versions allowed for a server
	versionCount, err := s.db.CountServerVersions(ctx, tx, serverJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxServerVersionsPerServer {
		return nil, database.ErrMaxVersionsReached
	}

	// Check this isn't a duplicate version
	versionExists, err := s.db.CheckVersionExists(ctx, tx, serverJSON.Name, serverJSON.Version)
	if err != nil {
		return nil, err
	}
	if versionExists {
		return nil, database.ErrInvalidVersion
	}

	// Get current latest version to determine if new version should be latest
	currentLatest, err := s.db.GetCurrentLatestVersion(ctx, tx, serverJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	// Determine if this version should be marked as latest
	isNewLatest := true
	if currentLatest != nil {
		var existingPublishedAt time.Time
		if currentLatest.Meta.Official != nil {
			existingPublishedAt = currentLatest.Meta.Official.PublishedAt
		}
		isNewLatest = CompareVersions(
			serverJSON.Version,
			currentLatest.Server.Version,
			publishTime,
			existingPublishedAt,
		) > 0
	}

	// Unmark old latest version if needed
	if isNewLatest && currentLatest != nil {
		if err := s.db.UnmarkAsLatest(ctx, tx, serverJSON.Name); err != nil {
			return nil, err
		}
	}

	// Create metadata for the new server
	officialMeta := &apiv0.RegistryExtensions{
		Status:      model.StatusActive, /* New versions are active by default */
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	// Insert new server version
	result, err := s.db.CreateServer(ctx, tx, &serverJSON, officialMeta)
	if err != nil {
		return nil, err
	}

	// Generate embedding asynchronously (non-blocking, best-effort)
	if s.shouldGenerateEmbeddingsOnPublish() { //nolint:nestif
		go func() {
			bgCtx := context.Background()
			payload := embeddings.BuildServerEmbeddingPayload(&serverJSON)
			if strings.TrimSpace(payload) == "" {
				return
			}
			embedding, err := embeddings.GenerateSemanticEmbedding(bgCtx, s.embeddingsProvider, payload, s.cfg.Embeddings.Dimensions)
			if err != nil {
				s.logger.Warn("failed to generate embedding for server", "name", serverJSON.Name, "version", serverJSON.Version, "error", err)
			} else if embedding != nil {
				if err := s.UpsertServerEmbedding(bgCtx, serverJSON.Name, serverJSON.Version, embedding); err != nil {
					s.logger.Warn("failed to store embedding for server", "name", serverJSON.Name, "version", serverJSON.Version, "error", err)
				}
			}
		}()
	}

	return result, nil
}

// validateNoDuplicateRemoteURLs checks that no other server is using the same remote URLs
func (s *registryServiceImpl) validateNoDuplicateRemoteURLs(ctx context.Context, tx pgx.Tx, serverDetail apiv0.ServerJSON) error {
	// Check each remote URL in the new server for conflicts
	for _, remote := range serverDetail.Remotes {
		// Use filter to find servers with this remote URL
		filter := &database.ServerFilter{RemoteURL: &remote.URL}

		conflictingServers, _, err := s.db.ListServers(ctx, tx, filter, "", 1000)
		if err != nil {
			return fmt.Errorf("failed to check remote URL conflict: %w", err)
		}

		// Check if any conflicting server has a different name
		for _, conflictingServer := range conflictingServers {
			if conflictingServer.Server.Name != serverDetail.Name {
				return fmt.Errorf("remote URL %s is already used by server %s", remote.URL, conflictingServer.Server.Name)
			}
		}
	}

	return nil
}

// ==============================
// Skills service implementations
// ==============================

// ListSkills returns registry entries for skills with pagination and filtering
func (s *registryServiceImpl) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	skills, next, err := s.db.ListSkills(ctx, nil, filter, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	return skills, next, nil
}

// GetSkillByName retrieves the latest version of a skill by its name
func (s *registryServiceImpl) GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	return s.db.GetSkillByName(ctx, nil, skillName)
}

// GetSkillByNameAndVersion retrieves a specific version of a skill by name and version
func (s *registryServiceImpl) GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	return s.db.GetSkillByNameAndVersion(ctx, nil, skillName, version)
}

// GetAllVersionsBySkillName retrieves all versions for a skill
func (s *registryServiceImpl) GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	return s.db.GetAllVersionsBySkillName(ctx, nil, skillName)
}

// CreateSkill creates a new skill version
func (s *registryServiceImpl) CreateSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	return database.InTransactionT(ctx, s.db, func(ctx context.Context, tx pgx.Tx) (*models.SkillResponse, error) {
		return s.createSkillInTransaction(ctx, tx, req)
	})
}

func (s *registryServiceImpl) createSkillInTransaction(ctx context.Context, tx pgx.Tx, req *models.SkillJSON) (*models.SkillResponse, error) {
	// Basic validation: ensure required fields present
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid skill payload: name and version are required")
	}

	publishTime := time.Now()
	skillJSON := *req

	// Check duplicate remote URLs among skills
	for _, remote := range skillJSON.Remotes {
		filter := &database.SkillFilter{RemoteURL: &remote.URL}
		existing, _, err := s.db.ListSkills(ctx, tx, filter, "", 1000)
		if err != nil {
			return nil, fmt.Errorf("failed to check remote URL conflict: %w", err)
		}
		for _, e := range existing {
			if e.Skill.Name != skillJSON.Name {
				return nil, fmt.Errorf("remote URL %s is already used by skill %s", remote.URL, e.Skill.Name)
			}
		}
	}

	// Enforce maximum versions per skill similar to servers
	versionCount, err := s.db.CountSkillVersions(ctx, tx, skillJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxServerVersionsPerServer {
		return nil, database.ErrMaxVersionsReached
	}

	// Prevent duplicate version
	exists, err := s.db.CheckSkillVersionExists(ctx, tx, skillJSON.Name, skillJSON.Version)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, database.ErrInvalidVersion
	}

	// Determine latest
	currentLatest, err := s.db.GetCurrentLatestSkillVersion(ctx, tx, skillJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	isNewLatest := true
	if currentLatest != nil {
		var existingPublishedAt time.Time
		if currentLatest.Meta.Official != nil {
			existingPublishedAt = currentLatest.Meta.Official.PublishedAt
		}
		// Reuse same version comparison semantics
		if CompareVersions(skillJSON.Version, currentLatest.Skill.Version, publishTime, existingPublishedAt) <= 0 {
			isNewLatest = false
		}
	}

	if isNewLatest && currentLatest != nil {
		if err := s.db.UnmarkSkillAsLatest(ctx, tx, skillJSON.Name); err != nil {
			return nil, err
		}
	}

	officialMeta := &models.SkillRegistryExtensions{
		Status:      string(model.StatusActive),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	return s.db.CreateSkill(ctx, tx, &skillJSON, officialMeta)
}

// UpdateServer updates an existing server with new details
func (s *registryServiceImpl) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	// Wrap the entire operation in a transaction
	return database.InTransactionT(ctx, s.db, func(ctx context.Context, tx pgx.Tx) (*apiv0.ServerResponse, error) {
		return s.updateServerInTransaction(ctx, tx, serverName, version, req, newStatus)
	})
}

// updateServerInTransaction contains the actual UpdateServer logic within a transaction
func (s *registryServiceImpl) updateServerInTransaction(ctx context.Context, tx pgx.Tx, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	// Get current server to check if it's deleted or being deleted
	currentServer, err := s.db.GetServerByNameAndVersion(ctx, tx, serverName, version)
	if err != nil {
		return nil, err
	}

	// Skip registry validation if:
	// 1. Server is currently deleted, OR
	// 2. Server is being set to deleted status
	currentlyDeleted := currentServer.Meta.Official != nil && currentServer.Meta.Official.Status == model.StatusDeleted
	beingDeleted := newStatus != nil && *newStatus == string(model.StatusDeleted)
	skipRegistryValidation := currentlyDeleted || beingDeleted

	// Validate the request, potentially skipping registry validation for deleted servers
	if err := s.validateUpdateRequest(ctx, *req, skipRegistryValidation); err != nil {
		return nil, err
	}

	// Merge the request with the current server, preserving metadata
	updatedServer := *req

	// Check for duplicate remote URLs using the updated server
	if err := s.validateNoDuplicateRemoteURLs(ctx, tx, updatedServer); err != nil {
		return nil, err
	}

	// Update server in database
	updatedServerResponse, err := s.db.UpdateServer(ctx, tx, serverName, version, &updatedServer)
	if err != nil {
		return nil, err
	}

	// Handle status change if provided
	if newStatus != nil {
		updatedWithStatus, err := s.db.SetServerStatus(ctx, tx, serverName, version, *newStatus)
		if err != nil {
			return nil, err
		}
		return updatedWithStatus, nil
	}

	return updatedServerResponse, nil
}

func (s *registryServiceImpl) StoreServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
	if len(content) == 0 {
		return nil
	}
	if contentType == "" {
		contentType = "text/markdown"
	}

	return s.db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		if _, err := s.db.GetServerByNameAndVersion(txCtx, tx, serverName, version); err != nil {
			return err
		}

		readme := &database.ServerReadme{
			ServerName:  serverName,
			Version:     version,
			Content:     append([]byte(nil), content...),
			ContentType: contentType,
			SizeBytes:   len(content),
			FetchedAt:   time.Now(),
		}

		if err := s.db.UpsertServerReadme(txCtx, tx, readme); err != nil {
			return err
		}

		return nil
	})
}

func (s *registryServiceImpl) GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	return s.db.GetLatestServerReadme(ctx, nil, serverName)
}

func (s *registryServiceImpl) GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	return s.db.GetServerReadme(ctx, nil, serverName, version)
}

// DeleteServer permanently removes a server version from the registry
func (s *registryServiceImpl) DeleteServer(ctx context.Context, serverName, version string) error {
	return s.db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		return s.db.DeleteServer(txCtx, tx, serverName, version)
	})
}

// validateUpdateRequest validates an update request with optional registry validation skipping
func (s *registryServiceImpl) validateUpdateRequest(ctx context.Context, req apiv0.ServerJSON, skipRegistryValidation bool) error {
	// Always validate the server JSON structure
	if err := validators.ValidateServerJSON(&req); err != nil {
		return err
	}

	// Skip registry validation if requested (for deleted servers)
	if skipRegistryValidation || !s.cfg.EnableRegistryValidation {
		return nil
	}

	// Perform registry validation for all packages
	for i, pkg := range req.Packages {
		if err := validators.ValidatePackage(ctx, pkg, req.Name); err != nil {
			return fmt.Errorf("registry validation failed for package %d (%s): %w", i, pkg.Identifier, err)
		}
	}

	return nil
}

// ==============================
// Agents service implementations
// ==============================

// ListAgents returns registry entries for agents with pagination and filtering
func (s *registryServiceImpl) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	if filter != nil {
		if err := s.ensureSemanticEmbedding(ctx, filter.Semantic); err != nil {
			return nil, "", err
		}
	}
	agents, next, err := s.db.ListAgents(ctx, nil, filter, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	return agents, next, nil
}

// GetAgentByName retrieves the latest version of an agent by its name
func (s *registryServiceImpl) GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return s.db.GetAgentByName(ctx, nil, agentName)
}

// GetAgentByNameAndVersion retrieves a specific version of an agent by name and version
func (s *registryServiceImpl) GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	return s.db.GetAgentByNameAndVersion(ctx, nil, agentName, version)
}

// GetAllVersionsByAgentName retrieves all versions for an agent
func (s *registryServiceImpl) GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	return s.db.GetAllVersionsByAgentName(ctx, nil, agentName)
}

// CreateAgent creates a new agent version
func (s *registryServiceImpl) CreateAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
	return database.InTransactionT(ctx, s.db, func(ctx context.Context, tx pgx.Tx) (*models.AgentResponse, error) {
		return s.createAgentInTransaction(ctx, tx, req)
	})
}

func (s *registryServiceImpl) createAgentInTransaction(ctx context.Context, tx pgx.Tx, req *models.AgentJSON) (*models.AgentResponse, error) {
	// Basic validation: ensure required fields present
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid agent payload: name and version are required")
	}

	publishTime := time.Now()
	agentJSON := *req

	// Check duplicate remote URLs among agents
	for _, remote := range agentJSON.Remotes {
		filter := &database.AgentFilter{RemoteURL: &remote.URL}
		existing, _, err := s.db.ListAgents(ctx, tx, filter, "", 1000)
		if err != nil {
			return nil, fmt.Errorf("failed to check remote URL conflict: %w", err)
		}
		for _, e := range existing {
			if e.Agent.Name != agentJSON.Name {
				return nil, fmt.Errorf("remote URL %s is already used by agent %s", remote.URL, e.Agent.Name)
			}
		}
	}

	// Enforce maximum versions per agent similar to servers
	versionCount, err := s.db.CountAgentVersions(ctx, tx, agentJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxServerVersionsPerServer {
		return nil, database.ErrMaxVersionsReached
	}

	// Prevent duplicate version
	exists, err := s.db.CheckAgentVersionExists(ctx, tx, agentJSON.Name, agentJSON.Version)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, database.ErrInvalidVersion
	}

	// Determine latest
	currentLatest, err := s.db.GetCurrentLatestAgentVersion(ctx, tx, agentJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	isNewLatest := true
	if currentLatest != nil {
		var existingPublishedAt time.Time
		if currentLatest.Meta.Official != nil {
			existingPublishedAt = currentLatest.Meta.Official.PublishedAt
		}
		// Reuse same version comparison semantics
		if CompareVersions(agentJSON.Version, currentLatest.Agent.Version, publishTime, existingPublishedAt) <= 0 {
			isNewLatest = false
		}
	}

	if isNewLatest && currentLatest != nil {
		if err := s.db.UnmarkAgentAsLatest(ctx, tx, agentJSON.Name); err != nil {
			return nil, err
		}
	}

	officialMeta := &models.AgentRegistryExtensions{
		Status:      string(model.StatusActive),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	result, err := s.db.CreateAgent(ctx, tx, &agentJSON, officialMeta)
	if err != nil {
		return nil, err
	}

	// Generate embedding asynchronously (non-blocking, best-effort)
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

// DeleteAgent permanently removes an agent version from the registry
func (s *registryServiceImpl) DeleteAgent(ctx context.Context, agentName, version string) error {
	return s.db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		return s.db.DeleteAgent(txCtx, tx, agentName, version)
	})
}

func (s *registryServiceImpl) UpsertServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	return s.db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		return s.db.SetServerEmbedding(txCtx, tx, serverName, version, embedding)
	})
}

func (s *registryServiceImpl) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.db.GetServerEmbeddingMetadata(ctx, nil, serverName, version)
}

func (s *registryServiceImpl) UpsertAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	return s.db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		return s.db.SetAgentEmbedding(txCtx, tx, agentName, version, embedding)
	})
}

func (s *registryServiceImpl) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.db.GetAgentEmbeddingMetadata(ctx, nil, agentName, version)
}

// ListProviders lists providers, optionally filtered by platform.
func (s *registryServiceImpl) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	return s.db.ListProviders(ctx, nil, platform)
}

// GetProviderByID gets a provider by ID.
func (s *registryServiceImpl) GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error) {
	return s.db.GetProviderByID(ctx, nil, providerID)
}

// CreateProvider creates a provider.
func (s *registryServiceImpl) CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	return s.db.CreateProvider(ctx, nil, in)
}

// UpdateProvider updates mutable provider fields.
func (s *registryServiceImpl) UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error) {
	return s.db.UpdateProvider(ctx, nil, providerID, in)
}

// DeleteProvider removes a provider by ID.
func (s *registryServiceImpl) DeleteProvider(ctx context.Context, providerID string) error {
	return s.db.DeleteProvider(ctx, nil, providerID)
}

func shouldIncludeDiscoveredDeployments(filter *models.DeploymentFilter) bool {
	if filter == nil {
		return true
	}
	if filter.Origin == nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(*filter.Origin), originDiscovered)
}

func discoveredDeploymentID(providerID, resourceType, name, version string) string {
	return discoveredDeploymentIDWithNamespace(providerID, resourceType, name, version, "")
}

func discoveredDeploymentIDWithNamespace(providerID, resourceType, name, version, namespace string) string {
	raw := strings.ToLower(strings.TrimSpace(providerID)) + "|" +
		strings.ToLower(strings.TrimSpace(resourceType)) + "|" +
		strings.TrimSpace(name) + "|" +
		strings.TrimSpace(version) + "|" +
		strings.TrimSpace(namespace)
	sum := sha256.Sum256([]byte(raw))
	return "discovered-" + hex.EncodeToString(sum[:8])
}

func discoveredDeploymentNamespace(dep *models.Deployment) string {
	if dep == nil {
		return ""
	}
	meta := models.KubernetesProviderMetadata{}
	if err := dep.ProviderMetadata.UnmarshalInto(&meta); err == nil {
		if namespace := strings.TrimSpace(meta.Namespace); namespace != "" {
			return namespace
		}
	}
	return ""
}

func matchesDiscoveredDeploymentFilter(filter *models.DeploymentFilter, dep *models.Deployment, provider *models.Provider) bool {
	if filter == nil {
		return true
	}
	if filter.ProviderID != nil && strings.TrimSpace(dep.ProviderID) != strings.TrimSpace(*filter.ProviderID) {
		return false
	}
	if filter.Platform != nil && provider != nil && !strings.EqualFold(strings.TrimSpace(provider.Platform), strings.TrimSpace(*filter.Platform)) {
		return false
	}
	if filter.ResourceType != nil && dep.ResourceType != *filter.ResourceType {
		return false
	}
	if filter.Status != nil && dep.Status != *filter.Status {
		return false
	}
	if filter.Origin != nil && !strings.EqualFold(strings.TrimSpace(dep.Origin), strings.TrimSpace(*filter.Origin)) {
		return false
	}
	if filter.ResourceName != nil && !strings.Contains(strings.ToLower(dep.ServerName), strings.ToLower(*filter.ResourceName)) {
		return false
	}
	return true
}

func (s *registryServiceImpl) appendDiscoveredDeployments(ctx context.Context, deployments []*models.Deployment, filter *models.DeploymentFilter) []*models.Deployment {
	var platformFilter *string
	if filter != nil {
		platformFilter = filter.Platform
	}
	seenDeploymentIDs := make(map[string]struct{}, len(deployments))
	for _, dep := range deployments {
		if dep == nil {
			continue
		}
		if id := strings.TrimSpace(dep.ID); id != "" {
			seenDeploymentIDs[id] = struct{}{}
		}
	}

	providers, err := s.db.ListProviders(ctx, nil, platformFilter)
	if err != nil {
		log.Printf("Warning: Failed to list providers for discovery: %v", err)
		return deployments
	}

	for _, provider := range providers {
		if provider == nil {
			continue
		}
		if filter != nil && filter.ProviderID != nil && strings.TrimSpace(*filter.ProviderID) != "" &&
			!strings.EqualFold(strings.TrimSpace(provider.ID), strings.TrimSpace(*filter.ProviderID)) {
			continue
		}

		adapter, err := s.resolveDeploymentAdapter(provider.Platform)
		if err != nil {
			log.Printf("Warning: Failed to resolve deployment adapter for provider %s (%s): %v", provider.ID, provider.Platform, err)
			continue
		}
		discovered, err := adapter.Discover(ctx, provider.ID)
		if err != nil {
			log.Printf("Warning: Failed to discover deployments for provider %s: %v", provider.ID, err)
			continue
		}
		for _, dep := range discovered {
			if dep == nil {
				continue
			}
			if strings.TrimSpace(dep.ProviderID) == "" {
				dep.ProviderID = provider.ID
			}
			if strings.TrimSpace(dep.Origin) == "" {
				dep.Origin = originDiscovered
			}
			if strings.TrimSpace(dep.ID) == "" {
				dep.ID = discoveredDeploymentIDWithNamespace(
					dep.ProviderID,
					dep.ResourceType,
					dep.ServerName,
					dep.Version,
					discoveredDeploymentNamespace(dep),
				)
			}
			if _, seen := seenDeploymentIDs[dep.ID]; seen {
				continue
			}
			if !matchesDiscoveredDeploymentFilter(filter, dep, provider) {
				continue
			}
			seenDeploymentIDs[dep.ID] = struct{}{}
			deployments = append(deployments, dep)
		}
	}
	return deployments
}

// GetDeployments retrieves all deployed servers with optional filtering
func (s *registryServiceImpl) GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	// Get managed deployments from DB
	dbDeployments, err := s.db.GetDeployments(ctx, nil, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployments from DB: %w", err)
	}

	var deployments []*models.Deployment
	deployments = append(deployments, dbDeployments...)

	if shouldIncludeDiscoveredDeployments(filter) {
		deployments = s.appendDiscoveredDeployments(ctx, deployments, filter)
	}

	return deployments, nil
}

// GetDeploymentByID retrieves a specific deployment by UUID.
func (s *registryServiceImpl) GetDeploymentByID(ctx context.Context, id string) (*models.Deployment, error) {
	deployment, err := s.db.GetDeploymentByID(ctx, nil, id)
	if err == nil {
		return deployment, nil
	}
	if !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	return s.getDiscoveredDeploymentByID(ctx, id)
}

func (s *registryServiceImpl) getDiscoveredDeploymentByID(ctx context.Context, id string) (*models.Deployment, error) {
	discoveredID := strings.TrimSpace(id)
	if !strings.HasPrefix(discoveredID, "discovered-") {
		return nil, database.ErrNotFound
	}

	origin := originDiscovered
	deployments, err := s.GetDeployments(ctx, &models.DeploymentFilter{Origin: &origin})
	if err != nil {
		return nil, err
	}
	for _, dep := range deployments {
		if dep != nil && dep.ID == discoveredID {
			return dep, nil
		}
	}
	return nil, database.ErrNotFound
}

func (s *registryServiceImpl) resolveProviderByID(ctx context.Context, providerID string) (*models.Provider, error) {
	if strings.TrimSpace(providerID) == "" {
		return nil, fmt.Errorf("%w: provider id is required", database.ErrInvalidInput)
	}
	return s.db.GetProviderByID(ctx, nil, providerID)
}

func (s *registryServiceImpl) resolveDeploymentAdapterByProviderID(ctx context.Context, providerID string) (registrytypes.DeploymentPlatformAdapter, error) {
	resolvedProviderID := strings.TrimSpace(providerID)
	if resolvedProviderID == "" {
		return nil, fmt.Errorf("%w: provider id is required", database.ErrInvalidInput)
	}
	provider, err := s.resolveProviderByID(ctx, resolvedProviderID)
	if err != nil {
		return nil, err
	}
	providerPlatform := strings.ToLower(strings.TrimSpace(provider.Platform))
	if providerPlatform == "" {
		return nil, fmt.Errorf("%w: provider platform is required", database.ErrInvalidInput)
	}
	return s.resolveDeploymentAdapter(providerPlatform)
}

func deploymentAdapterSupportsResourceType(adapter registrytypes.DeploymentPlatformAdapter, resourceType string) bool {
	if adapter == nil {
		return false
	}
	for _, supported := range adapter.SupportedResourceTypes() {
		if strings.EqualFold(strings.TrimSpace(supported), strings.TrimSpace(resourceType)) {
			return true
		}
	}
	return false
}

func (s *registryServiceImpl) findDeploymentByIdentity(ctx context.Context, resourceName, version, artifactType string) (*models.Deployment, error) {
	filter := &models.DeploymentFilter{
		ResourceType: &artifactType,
		ResourceName: &resourceName,
	}
	deployments, err := s.db.GetDeployments(ctx, nil, filter)
	if err != nil {
		return nil, err
	}
	for _, deployment := range deployments {
		if deployment.ServerName == resourceName &&
			deployment.Version == version &&
			deployment.ResourceType == artifactType {
			return deployment, nil
		}
	}
	return nil, database.ErrNotFound
}

// cleanupExistingDeployment removes a stale deployment record and its associated runtime resources.
// Errors from runtime cleanup are logged but not fatal, since the resources may already be gone.
func (s *registryServiceImpl) cleanupExistingDeployment(ctx context.Context, resourceName, version, resourceType string) error {
	existing, err := s.findDeploymentByIdentity(ctx, resourceName, version, resourceType)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("looking up existing deployment: %w", err)
	}

	if existing == nil {
		return nil
	}

	cleanupPlatform, err := s.resolveExistingDeploymentCleanupPlatform(ctx, existing)
	if err != nil {
		return err
	}
	if cleanupPlatform == "" {
		log.Printf("Warning: skipping stale cleanup for deployment %s: provider platform unavailable", existing.ID)
	} else if err := s.cleanupStaleDeploymentOnPlatform(ctx, cleanupPlatform, existing); err != nil {
		log.Printf("Warning: failed stale cleanup for deployment %s on platform %s: %v", existing.ID, cleanupPlatform, err)
	}

	if err := s.db.RemoveDeploymentByID(ctx, nil, existing.ID); err != nil && !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("removing stale deployment record: %w", err)
	}

	return nil
}

func (s *registryServiceImpl) resolveExistingDeploymentCleanupPlatform(ctx context.Context, existing *models.Deployment) (string, error) {
	providerID := strings.TrimSpace(existing.ProviderID)
	if providerID == "" {
		return "", nil
	}

	provider, err := s.resolveProviderByID(ctx, providerID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("resolving provider for existing deployment: %w", err)
	}
	if provider == nil {
		return "", nil
	}
	return strings.ToLower(strings.TrimSpace(provider.Platform)), nil
}

func (s *registryServiceImpl) cleanupStaleDeploymentOnPlatform(ctx context.Context, cleanupPlatform string, existing *models.Deployment) error {
	adapter, err := s.resolveDeploymentAdapter(cleanupPlatform)
	if err != nil {
		return fmt.Errorf("resolve deployment adapter: %w", err)
	}

	cleaner, ok := adapter.(DeploymentPlatformStaleCleaner)
	if !ok {
		return nil
	}
	return cleaner.CleanupStale(ctx, existing)
}

// DeployServer deploys a server with environment variables.
func (s *registryServiceImpl) DeployServer(ctx context.Context, serverName, version string, env map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	return s.CreateDeployment(ctx, &models.Deployment{
		ServerName:   serverName,
		Version:      version,
		Env:          env,
		PreferRemote: preferRemote,
		ResourceType: resourceTypeMCP,
		ProviderID:   providerID,
		Origin:       "managed",
	})
}

// DeployAgent deploys an agent with environment variables.
func (s *registryServiceImpl) DeployAgent(ctx context.Context, agentName, version string, env map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	return s.CreateDeployment(ctx, &models.Deployment{
		ServerName:   agentName,
		Version:      version,
		Env:          env,
		PreferRemote: preferRemote,
		ResourceType: resourceTypeAgent,
		ProviderID:   providerID,
		Origin:       "managed",
	})
}

func (s *registryServiceImpl) removeDeploymentRecord(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil {
		return database.ErrNotFound
	}
	if deployment.ID == "" {
		return database.ErrInvalidInput
	}
	if deployment.Origin == originDiscovered {
		return database.ErrInvalidInput
	}

	if err := s.db.RemoveDeploymentByID(ctx, nil, deployment.ID); err != nil {
		return err
	}

	return nil
}

// RemoveDeploymentByID removes a deployment by UUID.
func (s *registryServiceImpl) RemoveDeploymentByID(ctx context.Context, id string) error {
	deployment, err := s.db.GetDeploymentByID(ctx, nil, id)
	if err != nil {
		return err
	}
	return s.removeDeploymentRecord(ctx, deployment)
}

// CreateDeployment dispatches deployment creation to the platform adapter.
func (s *registryServiceImpl) CreateDeployment(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: deployment request is required", database.ErrInvalidInput)
	}
	resourceType := strings.ToLower(strings.TrimSpace(req.ResourceType))
	if resourceType == "" {
		resourceType = resourceTypeMCP
	}
	if resourceType != resourceTypeMCP && resourceType != resourceTypeAgent {
		return nil, fmt.Errorf("%w: invalid resource type %q", database.ErrInvalidInput, req.ResourceType)
	}
	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		return nil, fmt.Errorf("%w: provider id is required", database.ErrInvalidInput)
	}

	adapter, err := s.resolveDeploymentAdapterByProviderID(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if !deploymentAdapterSupportsResourceType(adapter, resourceType) {
		return nil, fmt.Errorf("%w: provider does not support resource type %q", database.ErrInvalidInput, resourceType)
	}

	deploymentReq := *req
	deploymentReq.ResourceType = resourceType
	deploymentReq.ProviderID = providerID
	deploymentReq.Origin = strings.TrimSpace(deploymentReq.Origin)
	if deploymentReq.Origin == "" {
		deploymentReq.Origin = "managed"
	}
	if deploymentReq.Env == nil {
		deploymentReq.Env = map[string]string{}
	}

	created, err := s.createManagedDeploymentRecord(ctx, &deploymentReq)
	if err != nil {
		return nil, err
	}

	actionResult, deployErr := adapter.Deploy(ctx, created)
	if deployErr != nil {
		if stateErr := s.applyFailedDeploymentAction(ctx, created.ID, deployErr, actionResult); stateErr != nil {
			return nil, fmt.Errorf("deploy failed: %w (state patch failed: %v)", deployErr, stateErr)
		}
		return nil, deployErr
	}

	if err := s.applyDeploymentActionResult(ctx, created.ID, actionResult); err != nil {
		return nil, err
	}

	updated, err := s.db.GetDeploymentByID(ctx, nil, created.ID)
	if err != nil {
		return nil, err
	}

	return updated, nil
}

// UndeployDeployment dispatches undeploy to the platform adapter.
func (s *registryServiceImpl) UndeployDeployment(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil {
		return database.ErrNotFound
	}
	adapter, err := s.resolveDeploymentAdapterByProviderID(ctx, deployment.ProviderID)
	if err != nil {
		return err
	}
	if err := adapter.Undeploy(ctx, deployment); err != nil {
		return err
	}
	return s.removeDeploymentRecord(ctx, deployment)
}

func (s *registryServiceImpl) createManagedDeploymentRecord(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	now := time.Now()
	deployment := &models.Deployment{
		ID:               req.ID,
		ServerName:       strings.TrimSpace(req.ServerName),
		Version:          strings.TrimSpace(req.Version),
		Status:           "deploying",
		Env:              req.Env,
		ProviderConfig:   req.ProviderConfig,
		ProviderMetadata: req.ProviderMetadata,
		PreferRemote:     req.PreferRemote,
		ResourceType:     req.ResourceType,
		ProviderID:       req.ProviderID,
		Origin:           req.Origin,
		DeployedAt:       now,
		UpdatedAt:        now,
	}
	if deployment.ServerName == "" || deployment.Version == "" {
		return nil, fmt.Errorf("%w: serverName and version are required", database.ErrInvalidInput)
	}
	if deployment.Env == nil {
		deployment.Env = map[string]string{}
	}

	switch deployment.ResourceType {
	case resourceTypeMCP:
		serverResp, err := s.db.GetServerByNameAndVersion(ctx, nil, deployment.ServerName, deployment.Version)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, fmt.Errorf("server %s not found in registry: %w", deployment.ServerName, database.ErrNotFound)
			}
			return nil, fmt.Errorf("failed to verify server: %w", err)
		}
		deployment.Version = serverResp.Server.Version
	case resourceTypeAgent:
		agentResp, err := s.db.GetAgentByNameAndVersion(ctx, nil, deployment.ServerName, deployment.Version)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, fmt.Errorf("agent %s not found in registry: %w", deployment.ServerName, database.ErrNotFound)
			}
			return nil, fmt.Errorf("failed to verify agent: %w", err)
		}
		deployment.Version = agentResp.Agent.Version
	default:
		return nil, fmt.Errorf("%w: invalid resource type %q", database.ErrInvalidInput, deployment.ResourceType)
	}

	if err := s.db.CreateDeployment(ctx, nil, deployment); err != nil {
		return nil, err
	}

	created, err := s.db.GetDeploymentByID(ctx, nil, deployment.ID)
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *registryServiceImpl) applyDeploymentActionResult(ctx context.Context, deploymentID string, result *models.DeploymentActionResult) error {
	status := "deployed"
	if result != nil {
		if trimmedStatus := strings.TrimSpace(result.Status); trimmedStatus != "" {
			status = trimmedStatus
		}
	}

	errorText := ""
	patch := &models.DeploymentStatePatch{
		Status: &status,
		Error:  &errorText,
	}
	if result != nil {
		errorText = strings.TrimSpace(result.Error)
		patch.Error = &errorText
		if result.ProviderConfig != nil {
			cfg := result.ProviderConfig
			patch.ProviderConfig = &cfg
		}
		if result.ProviderMetadata != nil {
			meta := result.ProviderMetadata
			patch.ProviderMetadata = &meta
		}
	}

	return s.db.UpdateDeploymentState(auth.WithSystemContext(ctx), nil, deploymentID, patch)
}

func (s *registryServiceImpl) applyFailedDeploymentAction(
	ctx context.Context,
	deploymentID string,
	deployErr error,
	result *models.DeploymentActionResult,
) error {
	status := "failed"
	if result != nil {
		if trimmedStatus := strings.TrimSpace(result.Status); trimmedStatus != "" {
			status = trimmedStatus
		}
	}
	errorText := strings.TrimSpace(deployErr.Error())
	if result != nil && strings.TrimSpace(result.Error) != "" {
		errorText = strings.TrimSpace(result.Error)
	}

	patch := &models.DeploymentStatePatch{
		Status: &status,
		Error:  &errorText,
	}
	if result != nil {
		if result.ProviderConfig != nil {
			cfg := result.ProviderConfig
			patch.ProviderConfig = &cfg
		}
		if result.ProviderMetadata != nil {
			meta := result.ProviderMetadata
			patch.ProviderMetadata = &meta
		}
	}
	return s.db.UpdateDeploymentState(auth.WithSystemContext(ctx), nil, deploymentID, patch)
}

// GetDeploymentLogs dispatches logs retrieval to the platform adapter.
func (s *registryServiceImpl) GetDeploymentLogs(ctx context.Context, deployment *models.Deployment) ([]string, error) {
	if deployment == nil {
		return nil, database.ErrNotFound
	}
	adapter, err := s.resolveDeploymentAdapterByProviderID(ctx, deployment.ProviderID)
	if err != nil {
		return nil, err
	}
	return adapter.GetLogs(ctx, deployment)
}

// CancelDeployment dispatches cancellation to the platform adapter.
func (s *registryServiceImpl) CancelDeployment(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil {
		return database.ErrNotFound
	}
	adapter, err := s.resolveDeploymentAdapterByProviderID(ctx, deployment.ProviderID)
	if err != nil {
		return err
	}
	return adapter.Cancel(ctx, deployment)
}

// ResolveAgentManifestSkills resolves registry-type skill references from the
// agent manifest into concrete skill refs (Docker images or GitHub repos) that
// can be passed to the runtime translator and ultimately to the Agent CRD.
func (s *registryServiceImpl) ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]api.AgentSkillRef, error) {
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

func (s *registryServiceImpl) resolveSkillRef(ctx context.Context, skill models.SkillRef) (api.AgentSkillRef, error) {
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

	// Prefer Docker/OCI image if available.
	for _, pkg := range skillResp.Skill.Packages {
		typ := strings.ToLower(strings.TrimSpace(pkg.RegistryType))
		if (typ == "docker" || typ == "oci") && strings.TrimSpace(pkg.Identifier) != "" {
			return api.AgentSkillRef{Name: skill.Name, Image: strings.TrimSpace(pkg.Identifier)}, nil
		}
	}

	// Fall back to git repository.
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

// ListPrompts returns registry entries for prompts with pagination and filtering
func (s *registryServiceImpl) ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	prompts, next, err := s.db.ListPrompts(ctx, nil, filter, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	return prompts, next, nil
}

// GetPromptByName retrieves the latest version of a prompt by its name
func (s *registryServiceImpl) GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return s.db.GetPromptByName(ctx, nil, promptName)
}

// GetPromptByNameAndVersion retrieves a specific version of a prompt by name and version
func (s *registryServiceImpl) GetPromptByNameAndVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	return s.db.GetPromptByNameAndVersion(ctx, nil, promptName, version)
}

// GetAllVersionsByPromptName retrieves all versions for a prompt
func (s *registryServiceImpl) GetAllVersionsByPromptName(ctx context.Context, promptName string) ([]*models.PromptResponse, error) {
	return s.db.GetAllVersionsByPromptName(ctx, nil, promptName)
}

// CreatePrompt creates a new prompt version
func (s *registryServiceImpl) CreatePrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error) {
	return database.InTransactionT(ctx, s.db, func(ctx context.Context, tx pgx.Tx) (*models.PromptResponse, error) {
		return s.createPromptInTransaction(ctx, tx, req)
	})
}

func (s *registryServiceImpl) createPromptInTransaction(ctx context.Context, tx pgx.Tx, req *models.PromptJSON) (*models.PromptResponse, error) {
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid prompt payload: name and version are required")
	}

	publishTime := time.Now()
	promptJSON := *req

	versionCount, err := s.db.CountPromptVersions(ctx, tx, promptJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxServerVersionsPerServer {
		return nil, database.ErrMaxVersionsReached
	}

	exists, err := s.db.CheckPromptVersionExists(ctx, tx, promptJSON.Name, promptJSON.Version)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, database.ErrInvalidVersion
	}

	currentLatest, err := s.db.GetCurrentLatestPromptVersion(ctx, tx, promptJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	isNewLatest := true
	if currentLatest != nil {
		var existingPublishedAt time.Time
		if currentLatest.Meta.Official != nil {
			existingPublishedAt = currentLatest.Meta.Official.PublishedAt
		}
		if CompareVersions(promptJSON.Version, currentLatest.Prompt.Version, publishTime, existingPublishedAt) <= 0 {
			isNewLatest = false
		}
	}

	if isNewLatest && currentLatest != nil {
		if err := s.db.UnmarkPromptAsLatest(ctx, tx, promptJSON.Name); err != nil {
			return nil, err
		}
	}

	officialMeta := &models.PromptRegistryExtensions{
		Status:      string(model.StatusActive),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	return s.db.CreatePrompt(ctx, tx, &promptJSON, officialMeta)
}

// DeletePrompt permanently removes a prompt version from the registry
func (s *registryServiceImpl) DeletePrompt(ctx context.Context, promptName, version string) error {
	return s.db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		return s.db.DeletePrompt(txCtx, tx, promptName, version)
	})
}
