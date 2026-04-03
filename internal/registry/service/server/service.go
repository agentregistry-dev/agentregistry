package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/embeddingutil"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/txutil"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/versionutil"
	"github.com/agentregistry-dev/agentregistry/internal/registry/validators"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const maxVersionsPerServer = 10000

// Registry defines the server operations exposed to other packages.
type Registry interface {
	ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error)
	GetServerByNameAndVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error)
	GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error)
	CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error)
	StoreServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error
	GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error)
	GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error)
	DeleteServer(ctx context.Context, serverName, version string) error
	UpsertServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error
	GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error)
}

type Dependencies struct {
	StoreDB            database.ServiceDatabase
	Servers            database.ServerStore
	Config             *config.Config
	EmbeddingsProvider embeddings.Provider
	Logger             *slog.Logger
}

type Service struct {
	storeDB            database.ServiceDatabase
	servers            database.ServerStore
	cfg                *config.Config
	embeddingsProvider embeddings.Provider
	logger             *slog.Logger
}

func New(deps Dependencies) *Service {
	servers := deps.Servers
	if servers == nil {
		servers = deps.StoreDB
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default().With("component", "registry.server")
	}

	return &Service{
		storeDB:            deps.StoreDB,
		servers:            servers,
		cfg:                deps.Config,
		embeddingsProvider: deps.EmbeddingsProvider,
		logger:             logger,
	}
}

func (s *Service) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}

	if filter != nil {
		if err := embeddingutil.EnsureQueryEmbedding(ctx, s.cfg, s.embeddingsProvider, filter.Semantic); err != nil {
			return nil, "", err
		}
	}

	return s.servers.ListServers(ctx, filter, cursor, limit)
}

func (s *Service) GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	return s.servers.GetServerByName(ctx, serverName)
}

func (s *Service) GetServerByNameAndVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	return s.servers.GetServerByNameAndVersion(ctx, serverName, version)
}

func (s *Service) GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	return s.servers.GetAllVersionsByServerName(ctx, serverName)
}

func (s *Service) CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return txutil.RunT(ctx, s.storeDB, func(txCtx context.Context, store database.Store) (*apiv0.ServerResponse, error) {
		return s.createServerInTransaction(txCtx, store, req)
	})
}

func (s *Service) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	return txutil.RunT(ctx, s.storeDB, func(txCtx context.Context, store database.Store) (*apiv0.ServerResponse, error) {
		return s.updateServerInTransaction(txCtx, store, serverName, version, req, newStatus)
	})
}

func (s *Service) StoreServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
	if len(content) == 0 {
		return nil
	}
	if contentType == "" {
		contentType = "text/markdown"
	}

	return txutil.Run(ctx, s.storeDB, func(txCtx context.Context, store database.Store) error {
		if _, err := store.GetServerByNameAndVersion(txCtx, serverName, version); err != nil {
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

		return store.UpsertServerReadme(txCtx, readme)
	})
}

func (s *Service) GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	return s.servers.GetLatestServerReadme(ctx, serverName)
}

func (s *Service) GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	return s.servers.GetServerReadme(ctx, serverName, version)
}

func (s *Service) DeleteServer(ctx context.Context, serverName, version string) error {
	return txutil.Run(ctx, s.storeDB, func(txCtx context.Context, store database.Store) error {
		return store.DeleteServer(txCtx, serverName, version)
	})
}

func (s *Service) UpsertServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	return txutil.Run(ctx, s.storeDB, func(txCtx context.Context, store database.Store) error {
		return store.SetServerEmbedding(txCtx, serverName, version, embedding)
	})
}

func (s *Service) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.servers.GetServerEmbeddingMetadata(ctx, serverName, version)
}

func (s *Service) ValidateNoDuplicateRemoteURLs(ctx context.Context, servers database.ServerStore, serverDetail apiv0.ServerJSON) error {
	return s.validateNoDuplicateRemoteURLs(ctx, servers, serverDetail)
}

func (s *Service) validateNoDuplicateRemoteURLs(ctx context.Context, servers database.ServerStore, serverDetail apiv0.ServerJSON) error {
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

func (s *Service) createServerInTransaction(ctx context.Context, servers database.ServerStore, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
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
	if versionCount >= maxVersionsPerServer {
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
		isNewLatest = versionutil.CompareVersions(
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

	if embeddingutil.EnabledOnPublish(s.cfg, s.embeddingsProvider) { //nolint:nestif
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

func (s *Service) updateServerInTransaction(ctx context.Context, servers database.ServerStore, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
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
		return servers.SetServerStatus(ctx, serverName, version, *newStatus)
	}

	return updatedServerResponse, nil
}

func (s *Service) validateUpdateRequest(ctx context.Context, req apiv0.ServerJSON, skipRegistryValidation bool) error {
	if err := validators.ValidateServerJSON(&req); err != nil {
		return err
	}

	if skipRegistryValidation || s.cfg == nil || !s.cfg.EnableRegistryValidation {
		return nil
	}

	for idx, pkg := range req.Packages {
		if err := validators.ValidatePackage(ctx, pkg, req.Name); err != nil {
			return fmt.Errorf("registry validation failed for package %d (%s): %w", idx, pkg.Identifier, err)
		}
	}

	return nil
}
