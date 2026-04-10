package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// IndexOptions configures an indexing operation.
type IndexOptions struct {
	BatchSize      int  `json:"batchSize"`
	Force          bool `json:"force"`
	DryRun         bool `json:"dryRun"`
	IncludeServers bool `json:"includeServers"`
	IncludeAgents  bool `json:"includeAgents"`
}

// IndexStats tracks progress for a resource type.
type IndexStats struct {
	Processed int `json:"processed"`
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Failures  int `json:"failures"`
}

// IndexResult contains the final result of an indexing operation.
type IndexResult struct {
	Servers IndexStats `json:"servers"`
	Agents  IndexStats `json:"agents"`
}

// IndexProgressCallback is called with progress updates during indexing.
// resource is "servers" or "agents".
type IndexProgressCallback func(resource string, stats IndexStats)

type ServerEmbeddingSource interface {
	ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error)
	SetServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error
}

type AgentEmbeddingSource interface {
	ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error)
	SetAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error
}

type Indexer interface {
	Run(ctx context.Context, opts IndexOptions, onProgress IndexProgressCallback) (*IndexResult, error)
}

type indexerImpl struct {
	servers    ServerEmbeddingSource
	agents     AgentEmbeddingSource
	provider   embeddings.Provider
	dimensions int
	logger     *slog.Logger
}

func NewIndexer(servers ServerEmbeddingSource, agents AgentEmbeddingSource, provider embeddings.Provider, dimensions int) Indexer {
	return &indexerImpl{
		servers:    servers,
		agents:     agents,
		provider:   provider,
		dimensions: dimensions,
		logger:     slog.Default().With("component", "indexer"),
	}
}

func (s *indexerImpl) Run(ctx context.Context, opts IndexOptions, onProgress IndexProgressCallback) (*IndexResult, error) {
	if s.provider == nil {
		return nil, errors.New("embedding provider is not configured")
	}

	if !opts.IncludeServers && !opts.IncludeAgents {
		return nil, errors.New("no targets selected; enable includeServers or includeAgents")
	}

	if opts.BatchSize <= 0 {
		opts.BatchSize = 100
	}

	result := &IndexResult{}

	if opts.IncludeServers {
		stats, err := s.indexServers(ctx, opts, onProgress)
		if err != nil {
			return nil, err
		}
		result.Servers = stats
	}

	if opts.IncludeAgents {
		stats, err := s.indexAgents(ctx, opts, onProgress)
		if err != nil {
			return nil, err
		}
		result.Agents = stats
	}

	return result, nil
}

func (s *indexerImpl) indexServers(ctx context.Context, opts IndexOptions, onProgress IndexProgressCallback) (IndexStats, error) {
	var (
		stats  IndexStats
		cursor string
	)

	const progressInterval = 100

	for {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		servers, nextCursor, err := s.servers.ListServers(ctx, nil, cursor, opts.BatchSize)
		if err != nil {
			return stats, err
		}
		if len(servers) == 0 {
			break
		}

		for _, server := range servers {
			select {
			case <-ctx.Done():
				return stats, ctx.Err()
			default:
			}

			stats.Processed++
			name := server.Server.Name
			version := server.Server.Version
			payload := embeddings.BuildServerEmbeddingPayload(&server.Server)

			if strings.TrimSpace(payload) == "" {
				s.logger.Info("skipping server: empty embedding payload", "name", name, "version", version)
				stats.Skipped++
				continue
			}

			payloadChecksum := embeddings.PayloadChecksum(payload)
			meta, err := s.servers.GetServerEmbeddingMetadata(ctx, name, version)
			if err != nil && !errors.Is(err, database.ErrNotFound) {
				s.logger.Error("failed to read server embedding metadata", "name", name, "version", version, "error", err)
				stats.Failures++
				continue
			}
			if errors.Is(err, database.ErrNotFound) {
				meta = &database.SemanticEmbeddingMetadata{}
			}

			hasEmbedding := meta != nil && meta.HasEmbedding
			needsUpdate := opts.Force || !hasEmbedding || meta.Checksum != payloadChecksum
			if !needsUpdate {
				stats.Skipped++
				continue
			}

			if opts.DryRun {
				s.logger.Info("dry run: would upsert server embedding", "name", name, "version", version, "existing", hasEmbedding, "checksum", meta.Checksum)
				stats.Updated++
				continue
			}

			record, err := embeddings.GenerateSemanticEmbedding(ctx, s.provider, payload, s.dimensions)
			if err != nil {
				s.logger.Error("failed to generate server embedding", "name", name, "version", version, "error", err)
				stats.Failures++
				continue
			}

			if err := s.servers.SetServerEmbedding(ctx, name, version, record); err != nil {
				s.logger.Error("failed to persist server embedding", "name", name, "version", version, "error", err)
				stats.Failures++
				continue
			}
			stats.Updated++
		}

		if stats.Processed%progressInterval == 0 && onProgress != nil {
			onProgress("servers", stats)
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	// Final progress callback
	if onProgress != nil {
		onProgress("servers", stats)
	}

	return stats, nil
}

func (s *indexerImpl) indexAgents(ctx context.Context, opts IndexOptions, onProgress IndexProgressCallback) (IndexStats, error) {
	var (
		stats  IndexStats
		cursor string
	)

	const progressInterval = 100

	for {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		agents, nextCursor, err := s.agents.ListAgents(ctx, nil, cursor, opts.BatchSize)
		if err != nil {
			return stats, err
		}
		if len(agents) == 0 {
			break
		}

		for _, agent := range agents {
			select {
			case <-ctx.Done():
				return stats, ctx.Err()
			default:
			}

			stats.Processed++
			name := agent.Agent.Name
			version := agent.Agent.Version
			payload := embeddings.BuildAgentEmbeddingPayload(&agent.Agent)

			if strings.TrimSpace(payload) == "" {
				s.logger.Info("skipping agent: empty embedding payload", "name", name, "version", version)
				stats.Skipped++
				continue
			}

			payloadChecksum := embeddings.PayloadChecksum(payload)
			meta, err := s.agents.GetAgentEmbeddingMetadata(ctx, name, version)
			if err != nil && !errors.Is(err, database.ErrNotFound) {
				s.logger.Error("failed to read agent embedding metadata", "name", name, "version", version, "error", err)
				stats.Failures++
				continue
			}
			if errors.Is(err, database.ErrNotFound) {
				meta = &database.SemanticEmbeddingMetadata{}
			}

			hasEmbedding := meta != nil && meta.HasEmbedding
			needsUpdate := opts.Force || !hasEmbedding || meta.Checksum != payloadChecksum
			if !needsUpdate {
				stats.Skipped++
				continue
			}

			if opts.DryRun {
				s.logger.Info("dry run: would upsert agent embedding", "name", name, "version", version, "existing", hasEmbedding, "checksum", meta.Checksum)
				stats.Updated++
				continue
			}

			record, err := embeddings.GenerateSemanticEmbedding(ctx, s.provider, payload, s.dimensions)
			if err != nil {
				s.logger.Error("failed to generate agent embedding", "name", name, "version", version, "error", err)
				stats.Failures++
				continue
			}

			if err := s.agents.SetAgentEmbedding(ctx, name, version, record); err != nil {
				s.logger.Error("failed to persist agent embedding", "name", name, "version", version, "error", err)
				stats.Failures++
				continue
			}
			stats.Updated++
		}

		if stats.Processed%progressInterval == 0 && onProgress != nil {
			onProgress("agents", stats)
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	// Final progress callback
	if onProgress != nil {
		onProgress("agents", stats)
	}

	return stats, nil
}
