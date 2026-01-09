package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	regembeddings "github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/spf13/cobra"
)

var (
	embeddingsBatchSize      int
	embeddingsForceUpdate    bool
	embeddingsDryRun         bool
	embeddingsIncludeServers bool
	embeddingsIncludeAgents  bool
)

// EmbeddingsCmd hosts semantic embedding maintenance subcommands.
var EmbeddingsCmd = &cobra.Command{
	Use:   "embeddings",
	Short: "Manage semantic embeddings stored in the registry database",
}

var embeddingsGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate embeddings for existing servers and agents (backfill or refresh)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		return runEmbeddingsGenerate(ctx)
	},
}

func init() {
	embeddingsGenerateCmd.Flags().IntVar(&embeddingsBatchSize, "batch-size", 100, "Number of server versions processed per batch")
	embeddingsGenerateCmd.Flags().BoolVar(&embeddingsForceUpdate, "update", false, "Regenerate embeddings even when the stored checksum matches")
	embeddingsGenerateCmd.Flags().BoolVar(&embeddingsDryRun, "dry-run", false, "Print planned changes without calling the embedding provider or writing to the database")
	embeddingsGenerateCmd.Flags().BoolVar(&embeddingsIncludeServers, "servers", true, "Include MCP servers when generating embeddings")
	embeddingsGenerateCmd.Flags().BoolVar(&embeddingsIncludeAgents, "agents", true, "Include agents when generating embeddings")
	EmbeddingsCmd.AddCommand(embeddingsGenerateCmd)
}

func runEmbeddingsGenerate(ctx context.Context) error {
	cfg := config.NewConfig()
	if !cfg.Embeddings.Enabled {
		return fmt.Errorf("embeddings are disabled (set AGENT_REGISTRY_EMBEDDINGS_ENABLED=true)")
	}

	db, err := database.NewPostgreSQL(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			log.Printf("Warning: failed to close database: %v", cerr)
		}
	}()

	httpClient := &http.Client{Timeout: 60 * time.Second}
	embeddingProvider, err := regembeddings.Factory(&cfg.Embeddings, httpClient)
	if err != nil {
		return fmt.Errorf("failed to initialize embeddings provider: %w", err)
	}

	registrySvc := service.NewRegistryService(db, cfg, embeddingProvider)

	limit := embeddingsBatchSize
	if limit <= 0 {
		limit = 100
	}

	opts := backfillOptions{
		limit:  limit,
		force:  embeddingsForceUpdate,
		dryRun: embeddingsDryRun,
	}

	processServers := embeddingsIncludeServers
	processAgents := embeddingsIncludeAgents
	if !processServers && !processAgents {
		// Default to both targets when no explicit selection was provided.
		processServers = true
		processAgents = true
	}

	var (
		totalFailures int
		summaries     []string
	)

	if processServers {
		stats, err := backfillServers(ctx, registrySvc, embeddingProvider, opts)
		if err != nil {
			return err
		}
		totalFailures += stats.failures
		summaries = append(summaries, fmt.Sprintf("Servers: processed=%d updated=%d skipped=%d failures=%d", stats.processed, stats.updated, stats.skipped, stats.failures))
	}

	if processAgents {
		stats, err := backfillAgents(ctx, registrySvc, embeddingProvider, opts)
		if err != nil {
			return err
		}
		totalFailures += stats.failures
		summaries = append(summaries, fmt.Sprintf("Agents: processed=%d updated=%d skipped=%d failures=%d", stats.processed, stats.updated, stats.skipped, stats.failures))
	}

	fmt.Println("Embedding backfill complete.")
	for _, summary := range summaries {
		fmt.Printf("  %s\n", summary)
	}

	if totalFailures > 0 {
		return fmt.Errorf("%d embedding(s) failed; see logs for details", totalFailures)
	}
	return nil
}

type backfillOptions struct {
	limit  int
	force  bool
	dryRun bool
}

type embeddingStats struct {
	processed int
	updated   int
	skipped   int
	failures  int
}

func backfillServers(ctx context.Context, registrySvc service.RegistryService, provider regembeddings.Provider, opts backfillOptions) (embeddingStats, error) {
	var (
		stats  embeddingStats
		cursor string
		limit  = opts.limit
	)

	const progressInterval = 100

	for {
		servers, nextCursor, err := registrySvc.ListServers(ctx, nil, cursor, limit)
		if err != nil {
			return stats, fmt.Errorf("failed to list servers: %w", err)
		}
		if len(servers) == 0 {
			break
		}

		for _, server := range servers {
			stats.processed++
			name := server.Server.Name
			version := server.Server.Version
			payload := regembeddings.BuildServerEmbeddingPayload(&server.Server)

			if strings.TrimSpace(payload) == "" {
				log.Printf("Skipping server %s@%s: empty embedding payload", name, version)
				stats.skipped++
				continue
			}

			payloadChecksum := regembeddings.PayloadChecksum(payload)
			meta, err := registrySvc.GetServerEmbeddingMetadata(ctx, name, version)
			if err != nil && !errors.Is(err, database.ErrNotFound) {
				log.Printf("Failed to read server embedding metadata for %s@%s: %v", name, version, err)
				stats.failures++
				continue
			}
			if errors.Is(err, database.ErrNotFound) {
				meta = &database.SemanticEmbeddingMetadata{}
			}

			hasEmbedding := meta != nil && meta.HasEmbedding
			needsUpdate := opts.force || !hasEmbedding || meta.Checksum != payloadChecksum
			if !needsUpdate {
				stats.skipped++
				continue
			}

			if opts.dryRun {
				fmt.Printf("[DRY RUN] Would upsert server embedding for %s@%s (existing=%v checksum=%s)\n", name, version, hasEmbedding, meta.Checksum)
				stats.updated++
				continue
			}

			record, err := regembeddings.GenerateSemanticEmbedding(ctx, provider, payload, cfg.Embeddings.Dimensions)
			if err != nil {
				log.Printf("Failed to generate server embedding for %s@%s: %v", name, version, err)
				stats.failures++
				continue
			}

			if err := registrySvc.UpsertServerEmbedding(ctx, name, version, record); err != nil {
				log.Printf("Failed to persist server embedding for %s@%s: %v", name, version, err)
				stats.failures++
				continue
			}
			stats.updated++
		}

		if stats.processed%progressInterval == 0 {
			logProgress("servers", stats)
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return stats, nil
}

func backfillAgents(ctx context.Context, registrySvc service.RegistryService, provider regembeddings.Provider, opts backfillOptions) (embeddingStats, error) {
	var (
		stats  embeddingStats
		cursor string
		limit  = opts.limit
	)

	const progressInterval = 100

	for {
		agents, nextCursor, err := registrySvc.ListAgents(ctx, nil, cursor, limit)
		if err != nil {
			return stats, fmt.Errorf("failed to list agents: %w", err)
		}
		if len(agents) == 0 {
			break
		}

		for _, agent := range agents {
			stats.processed++
			name := agent.Agent.Name
			version := agent.Agent.Version
			payload := regembeddings.BuildAgentEmbeddingPayload(&agent.Agent)

			if strings.TrimSpace(payload) == "" {
				log.Printf("Skipping agent %s@%s: empty embedding payload", name, version)
				stats.skipped++
				continue
			}

			payloadChecksum := regembeddings.PayloadChecksum(payload)
			meta, err := registrySvc.GetAgentEmbeddingMetadata(ctx, name, version)
			if err != nil && !errors.Is(err, database.ErrNotFound) {
				log.Printf("Failed to read agent embedding metadata for %s@%s: %v", name, version, err)
				stats.failures++
				continue
			}
			if errors.Is(err, database.ErrNotFound) {
				meta = &database.SemanticEmbeddingMetadata{}
			}

			hasEmbedding := meta != nil && meta.HasEmbedding
			needsUpdate := opts.force || !hasEmbedding || meta.Checksum != payloadChecksum
			if !needsUpdate {
				stats.skipped++
				continue
			}

			if opts.dryRun {
				fmt.Printf("[DRY RUN] Would upsert agent embedding for %s@%s (existing=%v checksum=%s)\n", name, version, hasEmbedding, meta.Checksum)
				stats.updated++
				continue
			}

			record, err := regembeddings.GenerateSemanticEmbedding(ctx, provider, payload, cfg.Embeddings.Dimensions)
			if err != nil {
				log.Printf("Failed to generate agent embedding for %s@%s: %v", name, version, err)
				stats.failures++
				continue
			}

			if err := registrySvc.UpsertAgentEmbedding(ctx, name, version, record); err != nil {
				log.Printf("Failed to persist agent embedding for %s@%s: %v", name, version, err)
				stats.failures++
				continue
			}
			stats.updated++
		}

		if stats.processed%progressInterval == 0 {
			logProgress("agents", stats)
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return stats, nil
}

func logProgress(resource string, stats embeddingStats) {
	if stats.processed == 0 {
		return
	}
	fmt.Printf("[%s] progress: processed=%d updated=%d skipped=%d failures=%d\n", resource, stats.processed, stats.updated, stats.skipped, stats.failures)
}
