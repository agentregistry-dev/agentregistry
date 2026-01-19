package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/agentregistry-dev/agentregistry/pkg/cli/config"
	"github.com/spf13/cobra"
)

var (
	// Flags for mcp publish command
	dockerUrl               string
	dockerTag               string
	pushFlag                bool
	dryRunFlag              bool
	publishPlatform         string
	publishVersion          string
	publishGithubRepository string
)

var PublishCmd = &cobra.Command{
	Use:   "publish <mcp-server-folder-path|server-name>",
	Short: "Build and publish an MCP Server or re-publish an existing server",
	Long: `Publish an MCP Server to the registry.

This command supports two modes:
1. Build and publish from local folder: Provide a path to a folder containing mcp.yaml
2. Re-publish existing server: Provide a server name from the registry to change its status to published

Examples:
  # Build and publish from local folder
  arctl mcp publish ./my-server --docker-url docker.io/myorg --push

  # Re-publish an existing server from the registry
  arctl mcp publish io.github.example/my-server`,
	Args: cobra.ExactArgs(1),
	RunE: runPublish,
}

func runPublish(cmd *cobra.Command, args []string) error {
	input := args[0]

	// Check if input is a local path with mcp.yaml
	absPath, err := filepath.Abs(input)
	isLocalPath := false
	if err == nil {
		if stat, err := os.Stat(absPath); err == nil && stat.IsDir() {
			manifestManager := manifest.NewManager(absPath)
			if manifestManager.Exists() {
				isLocalPath = true
			}
		}
	}

	// If it's a local path, build and publish
	if isLocalPath {
		return buildAndPublishLocal(absPath)
	}

	if publishVersion == "" {
		return fmt.Errorf("version is required")
	}

	// Otherwise, treat it as a server name from the registry
	return publishExistingServer(input, publishVersion)
}

func publishExistingServer(serverName string, version string) error {
	// We need to check get all servers with the same name and version from the registry.
	// If the specific version is not found, we should return an error.
	// Once found, we need to check if it's already published.

	isPublished, err := isServerPublished(serverName, version)
	if err != nil {
		return fmt.Errorf("failed to check if server is published: %w", err)
	}
	if isPublished {
		return fmt.Errorf("server %s version %s is already published", serverName, version)
	}

	servers, err := apiClient.GetAllServers()
	if err != nil {
		return fmt.Errorf("failed to get servers: %w", err)
	}

	for _, server := range servers {
		if server.Server.Name == serverName && server.Server.Version == version {
			// We found the entry, it's not published yet, so we can publish it.
			fmt.Printf("Publishing server: %s, Version: %s\n", server.Server.Name, server.Server.Version)
			err = apiClient.PublishMCPServerStatus(serverName, version)
			if err != nil {
				return fmt.Errorf("failed to publish server: %w", err)
			}
			return nil
		}
	}

	return fmt.Errorf("server %s version %s not found in registry", serverName, version)
}

func buildAndPublishLocal(absPath string) error {
	printer.PrintInfo(fmt.Sprintf("Publishing MCP server from: %s", absPath))

	serverJSON, err := buildAndPushDockerLocal(absPath, dryRunFlag, pushFlag, publishGithubRepository)
	if err != nil {
		return fmt.Errorf("failed to build and push mcp server: %w", err)
	}

	// Publish to registry
	if dryRunFlag {
		j, _ := json.Marshal(serverJSON)
		printer.PrintInfo("[DRY RUN] Would publish mcp server to registry " + apiClient.BaseURL + ": " + string(j))
	} else {
		// Push to registry (unpublished)
		_, err = apiClient.PushMCPServer(serverJSON)
		if err != nil {
			return fmt.Errorf("failed to push mcp server to registry: %w", err)
		}

		// auto-approve the server
		if config.GetAutoApprove() {
			if err := apiClient.ApproveMCPServerStatus(serverJSON.Name, serverJSON.Version, "Auto-approved via publish command"); err != nil {
				return fmt.Errorf("failed to approve mcp server: %w", err)
			}
		}

		// Publish the server
		if err := apiClient.PublishMCPServerStatus(serverJSON.Name, serverJSON.Version); err != nil {
			return fmt.Errorf("failed to publish mcp server to registry: %w", err)
		}

		printer.PrintSuccess("MCP Server publishing complete!")
	}

	return nil
}

func init() {
	// Flags for publish command
	PublishCmd.Flags().StringVar(&dockerUrl, "docker-url", "", "Docker registry URL (required for local builds). For example: docker.io/myorg. The final image name will be <docker-url>/<mcp-server-name>:<tag>")
	PublishCmd.Flags().BoolVar(&pushFlag, "push", false, "Automatically push to Docker and agent registries (for local builds)")
	PublishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	PublishCmd.Flags().StringVar(&dockerTag, "tag", "latest", "Docker image tag to use (for local builds)")
	PublishCmd.Flags().StringVar(&publishPlatform, "platform", "", "Target platform (e.g., linux/amd64,linux/arm64)")
	PublishCmd.Flags().StringVar(&publishVersion, "version", "", "Specify the version to publish (for re-publishing existing servers, skips interactive selection)")
	PublishCmd.Flags().StringVar(&publishGithubRepository, "github", "", "Specify the GitHub repository URL for the MCP server")
}
