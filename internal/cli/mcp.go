package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage MCP servers",
	Long:  `Manage MCP servers.`,
	Args:  cobra.ExactArgs(1),
}

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.AddCommand(mcp.AddToolCmd, mcp.BuildCmd, mcp.InitCmd, publishCmd)
}

var publishCmd = &cobra.Command{
	Use:   "publish [mcp-folder-path]",
	Short: "Publish an MCP server to the registry",
	Long:  `Publish an MCP server to the registry.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runPublish,
}

func runPublish(cmd *cobra.Command, args []string) error {
	mcpPath := args[0]

	// Validate path exists
	absPath, err := filepath.Abs(mcpPath)
	if err != nil {
		return fmt.Errorf("failed to resolve MCP path: %w", err)
	}

	manifestManager := manifest.NewManager(absPath)
	if !manifestManager.Exists() {
		return fmt.Errorf(
			"mcp.yaml not found in %s. Run 'arctl mcp init' first or specify a valid path as your first argument",
			absPath,
		)
	}

	projectManifest, err := manifestManager.Load()
	if err != nil {
		return fmt.Errorf("failed to load project manifest: %w", err)
	}
	version := projectManifest.Version
	if version == "" {
		version = "latest"
	}

	printer.PrintInfo(fmt.Sprintf("Publishing MCP server from: %s", absPath))

	author := "user"
	if projectManifest.Author != "" {
		author = projectManifest.Author
	}

	resp, err := APIClient.PublishServer(&v0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        fmt.Sprintf("%s/%s", strings.ToLower(author), strings.ToLower(projectManifest.Name)),
		Description: projectManifest.Description,
		Version:     version,
	})
	if err != nil {
		return fmt.Errorf("failed to publish server: %w", err)
	}

	printer.PrintInfo(fmt.Sprintf("Server published successfully: %s", resp.Server.Name))

	return nil
}
