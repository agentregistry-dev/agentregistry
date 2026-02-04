package service

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
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

// Indexer handles embedding indexing operations.
type Indexer struct {
	registry   RegistryService
	provider   embeddings.Provider
	dimensions int
}

// NewIndexer creates a new embeddings indexer.
func NewIndexer(registry RegistryService, provider embeddings.Provider, dimensions int) *Indexer {
	return &Indexer{
		registry:   registry,
		provider:   provider,
		dimensions: dimensions,
	}
}

// Run executes the indexing operation with progress callbacks.
func (s *Indexer) Run(ctx context.Context, opts IndexOptions, onProgress IndexProgressCallback) (*IndexResult, error) {
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

func (s *Indexer) indexServers(ctx context.Context, opts IndexOptions, onProgress IndexProgressCallback) (IndexStats, error) {
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

		servers, nextCursor, err := s.registry.ListServers(ctx, nil, cursor, opts.BatchSize)
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
				log.Printf("Skipping server %s@%s: empty embedding payload", name, version)
				stats.Skipped++
				continue
			}

			payloadChecksum := embeddings.PayloadChecksum(payload)
			meta, err := s.registry.GetServerEmbeddingMetadata(ctx, name, version)
			if err != nil && !errors.Is(err, database.ErrNotFound) {
				log.Printf("Failed to read server embedding metadata for %s@%s: %v", name, version, err)
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
				log.Printf("[DRY RUN] Would upsert server embedding for %s@%s (existing=%v checksum=%s)", name, version, hasEmbedding, meta.Checksum)
				stats.Updated++
				continue
			}

			record, err := embeddings.GenerateSemanticEmbedding(ctx, s.provider, payload, s.dimensions)
			if err != nil {
				log.Printf("Failed to generate server embedding for %s@%s: %v", name, version, err)
				stats.Failures++
				continue
			}

			if err := s.registry.UpsertServerEmbedding(ctx, name, version, record); err != nil {
				log.Printf("Failed to persist server embedding for %s@%s: %v", name, version, err)
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

func (s *Indexer) indexAgents(ctx context.Context, opts IndexOptions, onProgress IndexProgressCallback) (IndexStats, error) {
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

		agents, nextCursor, err := s.registry.ListAgents(ctx, nil, cursor, opts.BatchSize)
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
				log.Printf("Skipping agent %s@%s: empty embedding payload", name, version)
				stats.Skipped++
				continue
			}

			payloadChecksum := embeddings.PayloadChecksum(payload)
			meta, err := s.registry.GetAgentEmbeddingMetadata(ctx, name, version)
			if err != nil && !errors.Is(err, database.ErrNotFound) {
				log.Printf("Failed to read agent embedding metadata for %s@%s: %v", name, version, err)
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
				log.Printf("[DRY RUN] Would upsert agent embedding for %s@%s (existing=%v checksum=%s)", name, version, hasEmbedding, meta.Checksum)
				stats.Updated++
				continue
			}

			record, err := embeddings.GenerateSemanticEmbedding(ctx, s.provider, payload, s.dimensions)
			if err != nil {
				log.Printf("Failed to generate agent embedding for %s@%s: %v", name, version, err)
				stats.Failures++
				continue
			}

			if err := s.registry.UpsertAgentEmbedding(ctx, name, version, record); err != nil {
				log.Printf("Failed to persist agent embedding for %s@%s: %v", name, version, err)
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
