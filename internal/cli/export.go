package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/exporter"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/spf13/cobra"
)

var (
	exportOutput       string
	exportReadmeOutput string
)

var exportCmd = &cobra.Command{
	Use:    "export",
	Hidden: true,
	Short:  "Export servers from the registry database",
	Long:   "Exports all MCP server entries from the local registry database into a JSON seed file compatible with arctl import.",
	RunE: func(cmd *cobra.Command, args []string) error {
		outputPath := strings.TrimSpace(exportOutput)
		if outputPath == "" {
			return errors.New("--output is required (destination seed file path)")
		}

		cfg := config.NewConfig()

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
		exporterService := exporter.NewService(registryService)

		exportCtx := cmd.Context()
		if exportCtx == nil {
			exportCtx = context.Background()
		}

		exporterService.SetReadmeOutputPath(exportReadmeOutput)

		count, err := exporterService.ExportToPath(exportCtx, outputPath)
		if err != nil {
			return fmt.Errorf("failed to export servers: %w", err)
		}

		fmt.Printf("✓ Exported %d servers to %s\n", count, outputPath)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringVar(&exportOutput, "output", "", "Destination seed file path (required)")
	exportCmd.Flags().StringVar(&exportReadmeOutput, "readme-output", "", "Optional README seed output path")
	_ = exportCmd.MarkFlagRequired("output")
}
