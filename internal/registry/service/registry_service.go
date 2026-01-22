package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	models "github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/validators"
	"github.com/agentregistry-dev/agentregistry/internal/runtime"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/dockercompose"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"
	"github.com/jackc/pgx/v5"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const maxServerVersionsPerServer = 10000

// registryServiceImpl implements the RegistryService interface using our Database
// It also implements the Reconciler interface for server-side container management
type registryServiceImpl struct {
	db                 database.Database
	cfg                *config.Config
	embeddingsProvider embeddings.Provider
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
	}
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
func (s *registryServiceImpl) GetServerByNameAndVersion(ctx context.Context, serverName string, version string, publishedOnly bool) (*apiv0.ServerResponse, error) {
	serverRecord, err := s.db.GetServerByNameAndVersion(ctx, nil, serverName, version, publishedOnly)
	if err != nil {
		return nil, err
	}

	return serverRecord, nil
}

// GetAllVersionsByServerName retrieves all versions of a server by server name
func (s *registryServiceImpl) GetAllVersionsByServerName(ctx context.Context, serverName string, publishedOnly bool) ([]*apiv0.ServerResponse, error) {
	serverRecords, err := s.db.GetAllVersionsByServerName(ctx, nil, serverName, publishedOnly)
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

	// Acquire advisory lock to prevent concurrent publishes of the same server
	if err := s.db.AcquirePublishLock(ctx, tx, serverJSON.Name); err != nil {
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
		return nil, database.ErrMaxServersReached
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
	return s.db.CreateServer(ctx, tx, &serverJSON, officialMeta)
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

	// Acquire advisory lock per skill name
	if err := s.db.AcquirePublishLock(ctx, tx, skillJSON.Name); err != nil {
		return nil, err
	}

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
		return nil, database.ErrMaxServersReached
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

// PublishSkill marks a skill as published
func (s *registryServiceImpl) PublishSkill(ctx context.Context, skillName, version string) error {
	return s.db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		return s.db.PublishSkill(txCtx, tx, skillName, version)
	})
}

// UnpublishSkill marks a skill as unpublished
func (s *registryServiceImpl) UnpublishSkill(ctx context.Context, skillName, version string) error {
	return s.db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		return s.db.UnpublishSkill(txCtx, tx, skillName, version)
	})
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
	currentServer, err := s.db.GetServerByNameAndVersion(ctx, tx, serverName, version, false)
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

	// Acquire advisory lock to prevent concurrent edits of servers with same name
	if err := s.db.AcquirePublishLock(ctx, tx, serverName); err != nil {
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
		if _, err := s.db.GetServerByNameAndVersion(txCtx, tx, serverName, version, false); err != nil {
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

// PublishServer marks a server as published
func (s *registryServiceImpl) PublishServer(ctx context.Context, serverName, version string) error {
	return s.db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		return s.db.PublishServer(txCtx, tx, serverName, version)
	})
}

// UnpublishServer marks a server as unpublished
func (s *registryServiceImpl) UnpublishServer(ctx context.Context, serverName, version string) error {
	return s.db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		// Check if the server is currently deployed
		deployment, err := s.db.GetDeploymentByNameAndVersion(txCtx, tx, serverName, version)
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			return fmt.Errorf("failed to check deployment status: %w", err)
		}

		// If deployed (record exists) and it's the same version being unpublished, prevent unpublish
		if deployment != nil && deployment.Version == version {
			return fmt.Errorf("cannot unpublish deployed server %s (version %s): server must be removed from deployment first", serverName, version)
		}

		return s.db.UnpublishServer(txCtx, tx, serverName, version)
	})
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

	// Acquire advisory lock per agent name
	if err := s.db.AcquirePublishLock(ctx, tx, agentJSON.Name); err != nil {
		return nil, err
	}

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
		return nil, database.ErrMaxServersReached
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

	return s.db.CreateAgent(ctx, tx, &agentJSON, officialMeta)
}

// PublishAgent marks an agent as published
func (s *registryServiceImpl) PublishAgent(ctx context.Context, agentName, version string) error {
	return s.db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		return s.db.PublishAgent(txCtx, tx, agentName, version)
	})
}

// UnpublishAgent marks an agent as unpublished
func (s *registryServiceImpl) UnpublishAgent(ctx context.Context, agentName, version string) error {
	return s.db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		return s.db.UnpublishAgent(txCtx, tx, agentName, version)
	})
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

// GetDeployments retrieves all deployed servers
func (s *registryServiceImpl) GetDeployments(ctx context.Context) ([]*models.Deployment, error) {
	return s.db.GetDeployments(ctx, nil)
}

// GetDeploymentByName retrieves a specific deployment
func (s *registryServiceImpl) GetDeploymentByNameAndVersion(ctx context.Context, serverName string, version string) (*models.Deployment, error) {
	return s.db.GetDeploymentByNameAndVersion(ctx, nil, serverName, version)
}

func (s *registryServiceImpl) IsServerPublished(ctx context.Context, serverName, version string) (bool, error) {
	return s.db.IsServerPublished(ctx, nil, serverName, version)
}

// DeployServer deploys a server with configuration
func (s *registryServiceImpl) DeployServer(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool) (*models.Deployment, error) {
	serverResp, err := s.db.GetServerByNameAndVersion(ctx, nil, serverName, version, true)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, fmt.Errorf("server %s not found in registry: %w", serverName, database.ErrNotFound)
		}
		return nil, fmt.Errorf("failed to verify server: %w", err)
	}

	deployment := &models.Deployment{
		ServerName:   serverName,
		Version:      serverResp.Server.Version,
		Status:       "active",
		Config:       config,
		PreferRemote: preferRemote,
		ResourceType: "mcp",
		DeployedAt:   time.Now(),
		UpdatedAt:    time.Now(),
	}

	if config == nil {
		deployment.Config = make(map[string]string)
	}

	fmt.Println("creating deployment", deployment)
	err = s.db.CreateDeployment(ctx, nil, deployment)
	if err != nil {
		return nil, err
	}

	if err := s.ReconcileAll(ctx); err != nil {
		return nil, fmt.Errorf("deployment created but reconciliation failed: %w", err)
	}

	// Return the created deployment
	return s.db.GetDeploymentByNameAndVersion(ctx, nil, serverName, version)
}

// DeployAgent deploys an agent with configuration
func (s *registryServiceImpl) DeployAgent(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool) (*models.Deployment, error) {
	agentResp, err := s.db.GetAgentByNameAndVersion(ctx, nil, agentName, version)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, fmt.Errorf("agent %s not found in registry: %w", agentName, database.ErrNotFound)
		}
		return nil, fmt.Errorf("failed to verify agent: %w", err)
	}

	deployment := &models.Deployment{
		ServerName:   agentName,
		Version:      agentResp.Agent.Version,
		Status:       "active",
		Config:       config,
		PreferRemote: preferRemote,
		ResourceType: "agent",
		DeployedAt:   time.Now(),
		UpdatedAt:    time.Now(),
	}

	if config == nil {
		deployment.Config = make(map[string]string)
	}

	if err := s.db.CreateDeployment(ctx, nil, deployment); err != nil {
		return nil, err
	}

	if err := s.ReconcileAll(ctx); err != nil {
		return nil, fmt.Errorf("deployment created but reconciliation failed: %w", err)
	}

	return s.db.GetDeploymentByNameAndVersion(ctx, nil, agentName, version)
}

// UpdateDeploymentConfig updates the configuration for a deployment
func (s *registryServiceImpl) UpdateDeploymentConfig(ctx context.Context, serverName string, version string, config map[string]string) (*models.Deployment, error) {
	_, err := s.db.GetDeploymentByNameAndVersion(ctx, nil, serverName, version)
	if err != nil {
		return nil, err
	}

	err = s.db.UpdateDeploymentConfig(ctx, nil, serverName, config)
	if err != nil {
		return nil, err
	}

	// Trigger reconciliation to apply the config changes
	if err := s.ReconcileAll(ctx); err != nil {
		return nil, fmt.Errorf("config updated but reconciliation failed: %w", err)
	}

	return s.db.GetDeploymentByNameAndVersion(ctx, nil, serverName, version)
}

// RemoveServer removes a deployment
func (s *registryServiceImpl) RemoveServer(ctx context.Context, serverName string, version string) error {
	err := s.db.RemoveDeployment(ctx, nil, serverName, version)
	if err != nil {
		return err
	}

	if err := s.ReconcileAll(ctx); err != nil {
		return fmt.Errorf("deployment removed but reconciliation failed: %w", err)
	}

	return nil
}

// ReconcileAll fetches all deployments from database and reconciles containers
// This implements the Reconciler interface
func (s *registryServiceImpl) ReconcileAll(ctx context.Context) error {
	// Get all deployments from database
	deployments, err := s.GetDeployments(ctx)
	if err != nil {
		return fmt.Errorf("failed to get deployments: %w", err)
	}

	log.Printf("Reconciling %d deployment(s)", len(deployments))

	var (
		serverRunRequests []*registry.MCPServerRunRequest
		agentRunRequests  []*registry.AgentRunRequest
	)

	for _, dep := range deployments {
		switch dep.ResourceType {
		case "mcp":
			depServer, err := s.GetServerByNameAndVersion(ctx, dep.ServerName, dep.Version, true)
			if err != nil {
				log.Printf("Warning: Failed to get server %s v%s: %v", dep.ServerName, dep.Version, err)
				continue
			}

			depEnvValues := make(map[string]string)
			depArgValues := make(map[string]string)
			depHeaderValues := make(map[string]string)

			for k, v := range dep.Config {
				if len(k) > 7 && k[:7] == "HEADER_" {
					depHeaderValues[k[7:]] = v
				} else if len(k) > 4 && k[:4] == "ARG_" {
					depArgValues[k[4:]] = v
				} else {
					depEnvValues[k] = v
				}
			}

			serverRunRequests = append(serverRunRequests, &registry.MCPServerRunRequest{
				RegistryServer: &depServer.Server,
				PreferRemote:   dep.PreferRemote,
				EnvValues:      depEnvValues,
				ArgValues:      depArgValues,
				HeaderValues:   depHeaderValues,
			})

		case "agent":
			depAgent, err := s.GetAgentByNameAndVersion(ctx, dep.ServerName, dep.Version)
			if err != nil {
				log.Printf("Warning: Failed to get agent %s v%s: %v", dep.ServerName, dep.Version, err)
				continue
			}

			depEnvValues := make(map[string]string)
			maps.Copy(depEnvValues, dep.Config)

			agentRunRequests = append(agentRunRequests, &registry.AgentRunRequest{
				RegistryAgent: &depAgent.Agent,
				EnvValues:     depEnvValues,
			})
		default:
			log.Printf("Warning: Unknown resource type %q for deployment %s v%s", dep.ResourceType, dep.ServerName, dep.Version)
		}
	}

	regTranslator := registry.NewTranslator()
	composeTranslator := dockercompose.NewAgentGatewayTranslator(s.cfg.RuntimeDir, s.cfg.AgentGatewayPort)
	agentRuntime := runtime.NewAgentRegistryRuntime(
		regTranslator,
		composeTranslator,
		s.cfg.RuntimeDir,
		s.cfg.Verbose,
	)

	// Resolve registry-type MCP servers from agent manifests to add them to serverRunRequests
	for _, agentReq := range agentRunRequests {
		resolvedServers, err := s.resolveAgentManifestMCPServers(ctx, &agentReq.RegistryAgent.AgentManifest)
		if err != nil {
			return fmt.Errorf("failed to resolve MCP servers for agent %s: %w", agentReq.RegistryAgent.Name, err)
		}

		// Store resolved mcp servers so they can be written to the agent mcp server injection config
		agentReq.ResolvedMCPServers = resolvedServers
		// Add resolved servers to serverRunRequests so they get deployed
		serverRunRequests = append(serverRunRequests, resolvedServers...)
		if s.cfg.Verbose && len(resolvedServers) > 0 {
			log.Printf("Resolved %d MCP server(s) of type 'registry' for agent %s", len(resolvedServers), agentReq.RegistryAgent.Name)
		}
	}

	if err := agentRuntime.ReconcileAll(ctx, serverRunRequests, agentRunRequests); err != nil {
		return fmt.Errorf("failed reconciliation: %w", err)
	}

	return nil
}

// resolveAgentManifestMCPServers extracts and resolves registry-type MCP servers from an agent manifest
// This follows the same logic as the CLI-side resolveRegistryServer
// TODO: Should we also be resolving the other types (i.e. command)? I didn't see my command server configured in the agent-gateway yaml, unsure if expected or a bug.
// cat /tmp/arctl-runtime/agent-gateway.yaml only had an mcp route for the registry-resolved (since we added it to the run requests).
func (s *registryServiceImpl) resolveAgentManifestMCPServers(ctx context.Context, manifest *common.AgentManifest) ([]*registry.MCPServerRunRequest, error) {
	var resolvedServers []*registry.MCPServerRunRequest

	for _, mcpServer := range manifest.McpServers {
		// Only process registry-type servers (non-registry servers are baked into the image)
		if mcpServer.Type != "registry" {
			continue
		}

		// Determine registry URL
		registryURL := mcpServer.RegistryURL
		if registryURL == "" {
			registryURL = "http://127.0.0.1:12121"
		}

		version := mcpServer.RegistryServerVersion
		if version == "" {
			version = "latest"
		}

		serverEntry, err := fetchServerFromRegistry(registryURL, mcpServer.RegistryServerName, version)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch server %q from registry %s: %w", mcpServer.RegistryServerName, registryURL, err)
		}

		// Convert registry.ServerSpec to apiv0.ServerJSON
		serverJSON := convertServerSpecToServerJSON(&serverEntry.Server)

		// Create MCPServerRunRequest so that this resolved server is ran/deployed
		resolvedServers = append(resolvedServers, &registry.MCPServerRunRequest{
			RegistryServer: serverJSON,
			PreferRemote:   mcpServer.RegistryServerPreferRemote,
			EnvValues:      make(map[string]string),
			ArgValues:      make(map[string]string),
			HeaderValues:   make(map[string]string),
		})
	}

	return resolvedServers, nil
}

// fetchServerFromRegistry fetches a server from a registry via HTTP
func fetchServerFromRegistry(baseURL string, name string, version string) (*types.ServerEntry, error) {
	// Construct the endpoint: /v0/servers/{serverName}/versions/{version}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v0/servers") {
		baseURL = baseURL + "/v0/servers"
	}

	if version == "" {
		version = "latest"
	}

	encodedName := url.PathEscape(name)
	fetchURL := fmt.Sprintf("%s/%s/versions/%s", baseURL, encodedName, version)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Get(fetchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch server by name: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var registryResp types.RegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&registryResp); err != nil {
		return nil, fmt.Errorf("failed to decode server list response: %w", err)
	}

	if len(registryResp.Servers) != 1 {
		return nil, fmt.Errorf("expected 1 server, got %d: %s with version %s", len(registryResp.Servers), name, version)
	}

	return &registryResp.Servers[0], nil
}

// convertServerSpecToServerJSON converts a types.ServerSpec to apiv0.ServerJSON
func convertServerSpecToServerJSON(spec *types.ServerSpec) *apiv0.ServerJSON {
	// Convert Repository - apiv0.ServerJSON uses model.Repository
	var repo *model.Repository
	if spec.Repository.URL != "" || spec.Repository.Source != "" {
		repo = &model.Repository{
			URL:    spec.Repository.URL,
			Source: spec.Repository.Source, // Source is a string in model.Repository
		}
	}

	return &apiv0.ServerJSON{
		// ServerSpec doesn't include schema
		// TODO(infocus7): Should we use model.CurrentSchemaURL? Or should we return the schema from the ServerEntry?
		// In raw JSON, it's "$schema": "https://static.modelcontextprotocol.io/schemas/2025-10-17/server.schema.json", so would maybe need to parse it.
		Schema:      "",
		Name:        spec.Name,
		Title:       spec.Title,
		Description: spec.Description,
		Version:     spec.Version,
		WebsiteURL:  spec.WebsiteURL,
		Repository:  repo,
		Packages:    spec.Packages,
		Remotes:     spec.Remotes,
		// ServerSpec doesn't include meta
		Icons: nil,
		Meta:  nil,
	}
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
