package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	skillmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/validators"
	"github.com/jackc/pgx/v5"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const maxServerVersionsPerServer = 10000

// registryServiceImpl implements the RegistryService interface using our Database
type registryServiceImpl struct {
	db  database.Database
	cfg *config.Config
}

// NewRegistryService creates a new registry service with the provided database
func NewRegistryService(db database.Database, cfg *config.Config) RegistryService {
	return &registryServiceImpl{
		db:  db,
		cfg: cfg,
	}
}

// ListServers returns registry entries with cursor-based pagination and optional filtering
func (s *registryServiceImpl) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	// If limit is not set or negative, use a default limit
	if limit <= 0 {
		limit = 30
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
func (s *registryServiceImpl) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*skillmodels.SkillResponse, string, error) {
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
func (s *registryServiceImpl) GetSkillByName(ctx context.Context, skillName string) (*skillmodels.SkillResponse, error) {
	return s.db.GetSkillByName(ctx, nil, skillName)
}

// GetSkillByNameAndVersion retrieves a specific version of a skill by name and version
func (s *registryServiceImpl) GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*skillmodels.SkillResponse, error) {
	return s.db.GetSkillByNameAndVersion(ctx, nil, skillName, version)
}

// GetAllVersionsBySkillName retrieves all versions for a skill
func (s *registryServiceImpl) GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*skillmodels.SkillResponse, error) {
	return s.db.GetAllVersionsBySkillName(ctx, nil, skillName)
}

// CreateSkill creates a new skill version
func (s *registryServiceImpl) CreateSkill(ctx context.Context, req *skillmodels.SkillJSON) (*skillmodels.SkillResponse, error) {
	return database.InTransactionT(ctx, s.db, func(ctx context.Context, tx pgx.Tx) (*skillmodels.SkillResponse, error) {
		return s.createSkillInTransaction(ctx, tx, req)
	})
}

func (s *registryServiceImpl) createSkillInTransaction(ctx context.Context, tx pgx.Tx, req *skillmodels.SkillJSON) (*skillmodels.SkillResponse, error) {
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

	officialMeta := &skillmodels.SkillRegistryExtensions{
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
func (s *registryServiceImpl) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*agentmodels.AgentResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	agents, next, err := s.db.ListAgents(ctx, nil, filter, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	return agents, next, nil
}

// GetAgentByName retrieves the latest version of an agent by its name
func (s *registryServiceImpl) GetAgentByName(ctx context.Context, agentName string) (*agentmodels.AgentResponse, error) {
	return s.db.GetAgentByName(ctx, nil, agentName)
}

// GetAgentByNameAndVersion retrieves a specific version of an agent by name and version
func (s *registryServiceImpl) GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*agentmodels.AgentResponse, error) {
	return s.db.GetAgentByNameAndVersion(ctx, nil, agentName, version)
}

// GetAllVersionsByAgentName retrieves all versions for an agent
func (s *registryServiceImpl) GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*agentmodels.AgentResponse, error) {
	return s.db.GetAllVersionsByAgentName(ctx, nil, agentName)
}

// CreateAgent creates a new agent version
func (s *registryServiceImpl) CreateAgent(ctx context.Context, req *agentmodels.AgentJSON) (*agentmodels.AgentResponse, error) {
	return database.InTransactionT(ctx, s.db, func(ctx context.Context, tx pgx.Tx) (*agentmodels.AgentResponse, error) {
		return s.createAgentInTransaction(ctx, tx, req)
	})
}

func (s *registryServiceImpl) createAgentInTransaction(ctx context.Context, tx pgx.Tx, req *agentmodels.AgentJSON) (*agentmodels.AgentResponse, error) {
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

	officialMeta := &agentmodels.AgentRegistryExtensions{
		Status:      string(model.StatusActive),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	return s.db.CreateAgent(ctx, tx, &agentJSON, officialMeta)
}
