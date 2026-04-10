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
	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/versionutil"
	"github.com/agentregistry-dev/agentregistry/internal/registry/validators"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const maxVersionsPerServer = 10000

type Dependencies struct {
	StoreDB            database.Store
	Servers            database.ServerStore
	Tx                 database.Transactor
	Config             *config.Config
	EmbeddingsProvider embeddings.Provider
	Logger             *slog.Logger
}

type Registry interface {
	database.ServerReader
	PublishServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error)
	SetServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error
	DeleteServer(ctx context.Context, serverName, version string) error
	SetServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error
}

type registry struct {
	database.ServerStore
	tx                 database.Transactor
	cfg                *config.Config
	embeddingsProvider embeddings.Provider
	logger             *slog.Logger
}

var _ Registry = (*registry)(nil)

func New(deps Dependencies) Registry {
	if deps.Servers == nil && deps.StoreDB != nil {
		deps.Servers = deps.StoreDB.Servers()
	}
	if deps.Tx == nil {
		deps.Tx = deps.StoreDB
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default().With("component", "registry.server")
	}

	return &registry{
		ServerStore:        deps.Servers,
		tx:                 deps.Tx,
		cfg:                deps.Config,
		embeddingsProvider: deps.EmbeddingsProvider,
		logger:             logger,
	}
}

func (s *registry) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}

	if filter != nil {
		if err := embeddingutil.EnsureQueryEmbedding(ctx, s.cfg, s.embeddingsProvider, filter.Semantic); err != nil {
			return nil, "", err
		}
	}

	return s.ServerStore.ListServers(ctx, filter, cursor, limit)
}

func (s *registry) PublishServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return database.InTransactionT(ctx, s.tx, func(txCtx context.Context, scope database.Scope) (*apiv0.ServerResponse, error) {
		return s.createServerInTransaction(txCtx, scope.Servers(), req)
	})
}

func (s *registry) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	return database.InTransactionT(ctx, s.tx, func(txCtx context.Context, scope database.Scope) (*apiv0.ServerResponse, error) {
		return s.updateServerInTransaction(txCtx, scope.Servers(), serverName, version, req, newStatus)
	})
}

func (s *registry) SetServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
	if len(content) == 0 {
		return nil
	}
	if contentType == "" {
		contentType = "text/markdown"
	}

	return database.InTransaction(ctx, s.tx, func(txCtx context.Context, scope database.Scope) error {
		servers := scope.Servers()
		if _, err := servers.GetServerVersion(txCtx, serverName, version); err != nil {
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

		return servers.UpsertServerReadme(txCtx, readme)
	})
}

func (s *registry) DeleteServer(ctx context.Context, serverName, version string) error {
	return database.InTransaction(ctx, s.tx, func(txCtx context.Context, scope database.Scope) error {
		return scope.Servers().DeleteServer(txCtx, serverName, version)
	})
}

func (s *registry) SetServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	return database.InTransaction(ctx, s.tx, func(txCtx context.Context, scope database.Scope) error {
		return scope.Servers().SetServerEmbedding(txCtx, serverName, version, embedding)
	})
}

func (s *registry) validateNoDuplicateRemoteURLs(ctx context.Context, servers database.ServerStore, serverDetail apiv0.ServerJSON) error {
	for _, remote := range serverDetail.Remotes {
		remoteURL := remote.URL
		filter := &database.ServerFilter{RemoteURL: &remoteURL}
		cursor := ""

		for {
			conflictingServers, nextCursor, err := servers.ListServers(ctx, filter, cursor, 1000)
			if err != nil {
				return fmt.Errorf("failed to check remote URL conflict: %w", err)
			}
			for _, conflictingServer := range conflictingServers {
				if conflictingServer.Server.Name != serverDetail.Name {
					return fmt.Errorf("remote URL %s is already used by server %s", remoteURL, conflictingServer.Server.Name)
				}
			}
			if nextCursor == "" {
				break
			}
			cursor = nextCursor
		}
	}

	return nil
}

func (s *registry) createServerInTransaction(ctx context.Context, servers database.ServerStore, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request body is required", database.ErrInvalidInput)
	}
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

	currentLatest, err := servers.GetLatestServer(ctx, serverJSON.Name)
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
				if err := s.SetServerEmbedding(bgCtx, serverJSON.Name, serverJSON.Version, embedding); err != nil {
					s.logger.Warn("failed to store embedding for server", "name", serverJSON.Name, "version", serverJSON.Version, "error", err)
				}
			}
		}()
	}

	return result, nil
}

func (s *registry) updateServerInTransaction(ctx context.Context, servers database.ServerStore, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request body is required", database.ErrInvalidInput)
	}
	currentServer, err := servers.GetServerVersion(ctx, serverName, version)
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

func (s *registry) validateUpdateRequest(ctx context.Context, req apiv0.ServerJSON, skipRegistryValidation bool) error {
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
