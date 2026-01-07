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
	embeddingsBatchSize   int
	embeddingsForceUpdate bool
	embeddingsDryRun      bool
)

// EmbeddingsCmd hosts semantic embedding maintenance subcommands.
var EmbeddingsCmd = &cobra.Command{
	Use:   "embeddings",
	Short: "Manage semantic embeddings stored in the registry database",
}

var embeddingsGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate embeddings for existing servers (backfill or refresh)",
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

	var (
		cursor        string
		total         int
		updated       int
		skipped       int
		failures      int
		lastBatchSize int
	)

	for {
		servers, nextCursor, err := registrySvc.ListServers(ctx, nil, cursor, limit)
		if err != nil {
			return fmt.Errorf("failed to list servers: %w", err)
		}

		lastBatchSize = len(servers)
		if lastBatchSize == 0 {
			break
		}

		for _, server := range servers {
			total++
			name := server.Server.Name
			version := server.Server.Version
			payload := regembeddings.BuildServerEmbeddingPayload(&server.Server)

			if strings.TrimSpace(payload) == "" {
				log.Printf("Skipping %s@%s: empty embedding payload", name, version)
				skipped++
				continue
			}

			payloadChecksum := regembeddings.PayloadChecksum(payload)
			meta, err := registrySvc.GetServerEmbeddingMetadata(ctx, name, version)
			if err != nil && !errors.Is(err, database.ErrNotFound) {
				log.Printf("Failed to read embedding metadata for %s@%s: %v", name, version, err)
				failures++
				continue
			}
			if errors.Is(err, database.ErrNotFound) {
				meta = &database.SemanticEmbeddingMetadata{}
			}

			hasEmbedding := meta != nil && meta.HasEmbedding
			needsUpdate := embeddingsForceUpdate || !hasEmbedding || meta.Checksum != payloadChecksum
			if !needsUpdate {
				skipped++
				continue
			}

			if embeddingsDryRun {
				fmt.Printf("[DRY RUN] Would upsert embedding for %s@%s (existing=%v checksum=%s)\n", name, version, hasEmbedding, meta.Checksum)
				updated++
				continue
			}

			record, err := regembeddings.GenerateSemanticEmbedding(ctx, embeddingProvider, payload)
			if err != nil {
				log.Printf("Failed to generate embedding for %s@%s: %v", name, version, err)
				failures++
				continue
			}

			if err := registrySvc.UpsertServerEmbedding(ctx, name, version, record); err != nil {
				log.Printf("Failed to persist embedding for %s@%s: %v", name, version, err)
				failures++
				continue
			}
			updated++
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	fmt.Printf("Embedding backfill complete: processed=%d updated=%d skipped=%d failures=%d\n", total, updated, skipped, failures)
	if failures > 0 {
		return fmt.Errorf("%d embedding(s) failed; see logs for details", failures)
	}
	return nil
}
