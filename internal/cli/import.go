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
	"github.com/agentregistry-dev/agentregistry/internal/registry/importer"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/spf13/cobra"
)

var (
	importSource         string
	importSkipValidation bool
	importHeaders        []string
	importTimeout        time.Duration
	importGithubToken    string
	importUpdate         bool
	importReadmeSeed     string
	importProgressCache  string
	enrichServerData     bool
)

var importCmd = &cobra.Command{
	Use:    "import",
	Hidden: true,
	Short:  "Import servers into the registry database",
	Long:   "Imports MCP server entries from a JSON seed file or a registry /v0/servers endpoint into the local registry database.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(importSource) == "" {
			return errors.New("--source is required (file path, HTTP URL, or /v0/servers endpoint)")
		}

		// Load config and optionally override validation
		cfg := config.NewConfig()
		if importSkipValidation {
			cfg.EnableRegistryValidation = false
		}

		// Connect to PostgreSQL with a short timeout for the connection
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		db, err := database.NewPostgreSQL(ctx, cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("failed to connect to database: %w", err)
		}
		defer func() {
			if closeErr := db.Close(); closeErr != nil {
				log.Printf("Warning: failed to close database: %v", closeErr)
			}
		}()

		registryService := service.NewRegistryService(db, cfg)

		// Build HTTP client and headers for importer
		httpClient := &http.Client{Timeout: importTimeout}
		headerMap := make(map[string]string)
		for _, h := range importHeaders {
			// split only on first '=' to allow values containing '=' or ':'
			parts := strings.SplitN(h, "=", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid --request-header, expected key=value: %s", h)
			}
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key == "" {
				return fmt.Errorf("invalid --request-header, empty key: %s", h)
			}
			headerMap[key] = value
		}

		importerService := importer.NewService(registryService)
		importerService.SetHTTPClient(httpClient)
		importerService.SetRequestHeaders(headerMap)
		importerService.SetUpdateIfExists(importUpdate)
		importerService.SetGitHubToken(importGithubToken)
		importerService.SetReadmeSeedPath(importReadmeSeed)
		importerService.SetProgressCachePath(importProgressCache)

		if err := importerService.ImportFromPath(context.Background(), importSource, enrichServerData); err != nil {
			// Importer already logged failures and summary; return error to exit non-zero
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(importCmd)
	importCmd.Flags().StringVar(&importSource, "source", "", "Seed file path, HTTP URL, or registry /v0/servers URL (required)")
	importCmd.Flags().BoolVar(&importSkipValidation, "skip-validation", false, "Disable registry validation for this import run")
	importCmd.Flags().StringArrayVar(&importHeaders, "request-header", nil, "Additional request header in key=value form (repeatable)")
	importCmd.Flags().DurationVar(&importTimeout, "timeout", 30*time.Second, "HTTP request timeout")
	importCmd.Flags().StringVar(&importGithubToken, "github-token", "", "GitHub token for higher rate limits when enriching metadata")
	importCmd.Flags().BoolVar(&importUpdate, "update", false, "Update existing entries if name/version already exists")
	importCmd.Flags().StringVar(&importReadmeSeed, "readme-seed", "", "Optional README seed file path or URL")
	importCmd.Flags().StringVar(&importProgressCache, "progress-cache", "", "Optional path to store import progress for resuming interrupted runs")
	importCmd.Flags().BoolVar(&enrichServerData, "enrich-server-data", false, "Enrich server data during import (may increase import time)")
	_ = importCmd.MarkFlagRequired("source")
}
