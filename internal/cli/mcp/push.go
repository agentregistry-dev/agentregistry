package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/build"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/spf13/cobra"
)

var (
	// Flags for mcp push command
	pushDockerUrl  string
	pushDockerTag  string
	pushPushFlag   bool
	pushDryRunFlag bool
	pushPlatform   string
	pushVersion    string
)

var PushCmd = &cobra.Command{
	Use:   "push <mcp-server-folder-path>",
	Short: "Build and push an MCP Server to the registry without publishing",
	Long: `Push an MCP Server to the registry without publishing.
The server will be created in the registry but will not be marked as published.

This command builds and pushes from a local folder containing mcp.yaml.

Examples:
  # Build and push from local folder
  arctl mcp push ./my-server --docker-url docker.io/myorg --push`,
	Args: cobra.ExactArgs(1),
	RunE: runMCPServerPush,
}

func runMCPServerPush(cmd *cobra.Command, args []string) error {
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

	if !isLocalPath {
		return fmt.Errorf("mcp.yaml not found in %s. Run 'arctl mcp init' first", absPath)
	}

	return buildAndPushLocal(absPath)
}

func buildAndPushLocal(absPath string) error {
	// 1. Load mcp.yaml manifest
	manifestManager := manifest.NewManager(absPath)
	if !manifestManager.Exists() {
		return fmt.Errorf(
			"mcp.yaml not found in %s. Run 'arctl mcp init' first",
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

	repoName := sanitizeRepoName(projectManifest.Name)
	if pushDockerUrl == "" {
		return fmt.Errorf("docker url is required for local build and push (use --docker-url flag)")
	}
	imageRef := fmt.Sprintf("%s/%s:%s", strings.TrimSuffix(pushDockerUrl, "/"), repoName, version)

	printer.PrintInfo(fmt.Sprintf("Processing mcp server: %s", projectManifest.Name))
	serverJSON, err := translateServerJSON(projectManifest, imageRef, version)
	if err != nil {
		return fmt.Errorf("failed to build server JSON for '%v': %w", projectManifest, err)
	}

	// 2. Build Docker image
	builder := build.New()
	opts := build.Options{
		ProjectDir: absPath,
		Tag:        imageRef,
		Platform:   pushPlatform,
		Verbose:    verbose,
	}

	if err := builder.Build(opts); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	// 3. Push to Docker registry (if --push flag)
	if pushPushFlag {
		if pushDryRunFlag {
			printer.PrintInfo("[DRY RUN] Would push Docker image: " + imageRef)
		} else {
			printer.PrintInfo("Pushing Docker image: docker push " + imageRef)
			pushCmd := exec.Command("docker", "push", imageRef)
			pushCmd.Stdout = os.Stdout
			pushCmd.Stderr = os.Stderr
			if err := pushCmd.Run(); err != nil {
				return fmt.Errorf("docker push failed for %s: %w", imageRef, err)

			}
		}
	}

	// 4. Push to agent registry (without publishing)
	if pushDryRunFlag {
		j, _ := json.Marshal(serverJSON)
		printer.PrintInfo("[DRY RUN] Would push mcp server to registry " + apiClient.BaseURL + ": " + string(j))
	} else {
		_, err = apiClient.PushMCPServer(serverJSON)
		if err != nil {
			return fmt.Errorf("failed to push mcp server to registry: %w", err)
		}
		printer.PrintSuccess("MCP Server pushed successfully")
	}

	return nil
}

func init() {
	// Flags for push command
	PushCmd.Flags().StringVar(&pushDockerUrl, "docker-url", "", "Docker registry URL (required for local builds). For example: docker.io/myorg. The final image name will be <docker-url>/<mcp-server-name>:<tag>")
	PushCmd.Flags().BoolVar(&pushPushFlag, "push", false, "Automatically push to Docker and agent registries (for local builds)")
	PushCmd.Flags().BoolVar(&pushDryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	PushCmd.Flags().StringVar(&pushDockerTag, "tag", "latest", "Docker image tag to use (for local builds)")
	PushCmd.Flags().StringVar(&pushPlatform, "platform", "", "Target platform (e.g., linux/amd64,linux/arm64)")
	PushCmd.Flags().StringVar(&pushVersion, "version", "", "Specify the version to push")
}
