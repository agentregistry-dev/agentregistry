package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/validators"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// ServerService defines server catalog and mutation operations.
type ServerService interface {
	// ListServers retrieve all servers with optional filtering
	ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	// GetServerByName retrieve latest version of a server by server name
	GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error)
	// GetServerByNameAndVersion retrieve specific version of a server by server name and version
	GetServerByNameAndVersion(ctx context.Context, serverName string, version string) (*apiv0.ServerResponse, error)
	// GetAllVersionsByServerName retrieve all versions of a server by server name
	GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error)
	// CreateServer creates a new server version
	CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	// UpdateServer updates an existing server and optionally its status
	UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error)
	// StoreServerReadme stores or updates the README for a server version
	StoreServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error
	// GetServerReadmeLatest retrieves the README for the latest server version
	GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error)
	// GetServerReadmeByVersion retrieves the README for a specific server version
	GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error)
	// DeleteServer permanently removes a server version from the registry
	DeleteServer(ctx context.Context, serverName, version string) error
	// UpsertServerEmbedding stores semantic embedding metadata for a server version
	UpsertServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error
	// GetServerEmbeddingMetadata retrieves the embedding metadata for a server version
	GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error)
}

// ListServers returns registry entries with cursor-based pagination and optional filtering.
func (s *serverServiceImpl) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}

	if filter != nil {
		if err := s.ensureSemanticEmbedding(ctx, filter.Semantic); err != nil {
			return nil, "", err
		}
	}

	serverRecords, nextCursor, err := s.readStores().servers.ListServers(ctx, filter, cursor, limit)
	if err != nil {
		return nil, "", err
	}

	return serverRecords, nextCursor, nil
}

// GetServerByName retrieves the latest version of a server by its server name.
func (s *serverServiceImpl) GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	serverRecord, err := s.readStores().servers.GetServerByName(ctx, serverName)
	if err != nil {
		return nil, err
	}

	return serverRecord, nil
}

// GetServerByNameAndVersion retrieves a specific version of a server by server name and version.
func (s *serverServiceImpl) GetServerByNameAndVersion(ctx context.Context, serverName string, version string) (*apiv0.ServerResponse, error) {
	serverRecord, err := s.readStores().servers.GetServerByNameAndVersion(ctx, serverName, version)
	if err != nil {
		return nil, err
	}

	return serverRecord, nil
}

// GetAllVersionsByServerName retrieves all versions of a server by server name.
func (s *serverServiceImpl) GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	serverRecords, err := s.readStores().servers.GetAllVersionsByServerName(ctx, serverName)
	if err != nil {
		return nil, err
	}

	return serverRecords, nil
}

// CreateServer creates a new server version.
func (s *serverServiceImpl) CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return inTransactionT(ctx, s, func(ctx context.Context, stores storeBundle) (*apiv0.ServerResponse, error) {
		return s.createServerInTransaction(ctx, stores.servers, req)
	})
}

func (s *serverServiceImpl) createServerInTransaction(ctx context.Context, servers database.ServerStore, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if err := validators.ValidatePublishRequest(ctx, *req, s.cfg); err != nil {
		return nil, err
	}

	publishTime := time.Now()
	serverJSON := *req

	if err := servers.AcquireServerCreateLock(ctx, serverJSON.Name); err != nil {
		return nil, err
	}

	if err := s.validateNoDuplicateRemoteURLs(ctx, servers, serverJSON); err != nil {
		return nil, err
	}

	versionCount, err := servers.CountServerVersions(ctx, serverJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxServerVersionsPerServer {
		return nil, database.ErrMaxVersionsReached
	}

	versionExists, err := servers.CheckVersionExists(ctx, serverJSON.Name, serverJSON.Version)
	if err != nil {
		return nil, err
	}
	if versionExists {
		return nil, database.ErrInvalidVersion
	}

	currentLatest, err := servers.GetCurrentLatestVersion(ctx, serverJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

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

	if isNewLatest && currentLatest != nil {
		if err := servers.UnmarkAsLatest(ctx, serverJSON.Name); err != nil {
			return nil, err
		}
	}

	officialMeta := &apiv0.RegistryExtensions{
		Status:      model.StatusActive,
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	result, err := servers.CreateServer(ctx, &serverJSON, officialMeta)
	if err != nil {
		return nil, err
	}

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

// validateNoDuplicateRemoteURLs checks that no other server is using the same remote URLs.
func (s *serverServiceImpl) validateNoDuplicateRemoteURLs(ctx context.Context, servers database.ServerStore, serverDetail apiv0.ServerJSON) error {
	for _, remote := range serverDetail.Remotes {
		filter := &database.ServerFilter{RemoteURL: &remote.URL}

		conflictingServers, _, err := servers.ListServers(ctx, filter, "", 1000)
		if err != nil {
			return fmt.Errorf("failed to check remote URL conflict: %w", err)
		}

		for _, conflictingServer := range conflictingServers {
			if conflictingServer.Server.Name != serverDetail.Name {
				return fmt.Errorf("remote URL %s is already used by server %s", remote.URL, conflictingServer.Server.Name)
			}
		}
	}

	return nil
}

// UpdateServer updates an existing server with new details.
func (s *serverServiceImpl) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	return inTransactionT(ctx, s, func(ctx context.Context, stores storeBundle) (*apiv0.ServerResponse, error) {
		return s.updateServerInTransaction(ctx, stores.servers, serverName, version, req, newStatus)
	})
}

func (s *serverServiceImpl) updateServerInTransaction(ctx context.Context, servers database.ServerStore, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	currentServer, err := servers.GetServerByNameAndVersion(ctx, serverName, version)
	if err != nil {
		return nil, err
	}

	currentlyDeleted := currentServer.Meta.Official != nil && currentServer.Meta.Official.Status == model.StatusDeleted
	beingDeleted := newStatus != nil && *newStatus == string(model.StatusDeleted)
	skipRegistryValidation := currentlyDeleted || beingDeleted

	if err := s.validateUpdateRequest(ctx, *req, skipRegistryValidation); err != nil {
		return nil, err
	}

	updatedServer := *req

	if err := s.validateNoDuplicateRemoteURLs(ctx, servers, updatedServer); err != nil {
		return nil, err
	}

	updatedServerResponse, err := servers.UpdateServer(ctx, serverName, version, &updatedServer)
	if err != nil {
		return nil, err
	}

	if newStatus != nil {
		updatedWithStatus, err := servers.SetServerStatus(ctx, serverName, version, *newStatus)
		if err != nil {
			return nil, err
		}
		return updatedWithStatus, nil
	}

	return updatedServerResponse, nil
}

func (s *serverServiceImpl) StoreServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
	if len(content) == 0 {
		return nil
	}
	if contentType == "" {
		contentType = "text/markdown"
	}

	return s.inTransaction(ctx, func(txCtx context.Context, stores storeBundle) error {
		if _, err := stores.servers.GetServerByNameAndVersion(txCtx, serverName, version); err != nil {
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

		if err := stores.servers.UpsertServerReadme(txCtx, readme); err != nil {
			return err
		}

		return nil
	})
}

func (s *serverServiceImpl) GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	return s.readStores().servers.GetLatestServerReadme(ctx, serverName)
}

func (s *serverServiceImpl) GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	return s.readStores().servers.GetServerReadme(ctx, serverName, version)
}

// DeleteServer permanently removes a server version from the registry.
func (s *serverServiceImpl) DeleteServer(ctx context.Context, serverName, version string) error {
	return s.inTransaction(ctx, func(txCtx context.Context, stores storeBundle) error {
		return stores.servers.DeleteServer(txCtx, serverName, version)
	})
}

// validateUpdateRequest validates an update request with optional registry validation skipping.
func (s *serverServiceImpl) validateUpdateRequest(ctx context.Context, req apiv0.ServerJSON, skipRegistryValidation bool) error {
	if err := validators.ValidateServerJSON(&req); err != nil {
		return err
	}

	if skipRegistryValidation || !s.cfg.EnableRegistryValidation {
		return nil
	}

	for i, pkg := range req.Packages {
		if err := validators.ValidatePackage(ctx, pkg, req.Name); err != nil {
			return fmt.Errorf("registry validation failed for package %d (%s): %w", i, pkg.Identifier, err)
		}
	}

	return nil
}

func (s *serverServiceImpl) UpsertServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	return s.inTransaction(ctx, func(txCtx context.Context, stores storeBundle) error {
		return stores.servers.SetServerEmbedding(txCtx, serverName, version, embedding)
	})
}

func (s *serverServiceImpl) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.readStores().servers.GetServerEmbeddingMetadata(ctx, serverName, version)
}
