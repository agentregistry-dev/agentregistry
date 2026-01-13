package mcp

import (
	"context"
	"fmt"
	"time"

	registryserver "github.com/agentregistry-dev/agentregistry/internal/mcp/registryserver"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Run an MCP bridge exposing registry discovery APIs (stdio transport)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := context.Background()
		cfg := config.NewConfig()

		dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		db, err := database.NewPostgreSQL(dbCtx, cfg.DatabaseURL)
		cancel()
		if err != nil {
			return fmt.Errorf("connect database: %w", err)
		}
		defer func() { _ = db.Close() }()

		registrySvc := service.NewRegistryService(db, cfg)
		server := registryserver.NewServer(registrySvc)

		cmd.PrintErrln("Starting registry MCP bridge on stdio...")
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			return fmt.Errorf("mcp server exited: %w", err)
		}
		return nil
	},
}

func init() {
	McpCmd.AddCommand(registryCmd)
}
