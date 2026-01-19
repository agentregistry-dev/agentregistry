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
	"github.com/agentregistry-dev/agentregistry/pkg/cli/config"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/spf13/cobra"
)

var (
	// Flags for mcp push command
	pushDockerUrl string
	pushDockerTag string
	pushPlatform  string
	// Should push to docker registry
	pushDockerPushFlag   bool
	pushDryRunFlag       bool
	pushGithubRepository string
)

var PushCmd = &cobra.Command{
	Use:   "push <mcp-server-folder-path>",
	Short: "Build and push an MCP Server to the registry with auto-approval",
	Long: `Push an MCP Server to the registry and automatically approve it.

This command builds the MCP server, pushes it to the registry (unpublished), and then
automatically approves it, making it ready for publishing. This is useful for automated
workflows where approval is automatic.

Examples:
  # Build and push from local folder
  arctl mcp push ./my-server --docker-url docker.io/myorg`,
	Args: cobra.ExactArgs(1),
	RunE: runPush,
}

func runPush(cmd *cobra.Command, args []string) error {
	input := args[0]

	// Check if input is a local path with mcp.yaml
	absPath, err := filepath.Abs(input)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	if stat, err := os.Stat(absPath); err != nil || !stat.IsDir() {
		return fmt.Errorf("path must be a directory containing mcp.yaml: %s", absPath)
	}

	manifestManager := manifest.NewManager(absPath)
	if !manifestManager.Exists() {
		return fmt.Errorf("mcp.yaml not found in %s. Run 'arctl mcp init' first", absPath)
	}

	serverJSON, err := buildAndPushDockerLocal(absPath, pushDryRunFlag, pushDockerPushFlag, pushGithubRepository)
	if err != nil {
		return fmt.Errorf("failed to build and push mcp server: %w", err)
	}

	if pushDryRunFlag {
		j, _ := json.Marshal(serverJSON)
		printer.PrintInfo("[DRY RUN] Would push MCP server to registry " + apiClient.BaseURL + ": " + string(j))
	} else {
		// Push to registry (unpublished)
		if _, err := apiClient.PushMCPServer(serverJSON); err != nil {
			return fmt.Errorf("failed to push mcp server to registry: %w", err)
		}

		// Auto-approve the server (if configured)
		if config.GetAutoApprove() {
			if err := apiClient.ApproveMCPServerStatus(serverJSON.Name, serverJSON.Version, "Auto-approved via push command"); err != nil {
				return fmt.Errorf("failed to approve mcp server: %w", err)
			}
		}
	}

	return nil
}

func buildDockerImage(absPath, githubRepository string) (string, *apiv0.ServerJSON, error) {
	// 1. Load mcp.yaml manifest
	manifestManager := manifest.NewManager(absPath)
	if !manifestManager.Exists() {
		return "", nil, fmt.Errorf("mcp.yaml not found in %s. Run 'arctl mcp init' first", absPath)
	}

	projectManifest, err := manifestManager.Load()
	if err != nil {
		return "", nil, fmt.Errorf("failed to load project manifest: %w", err)
	}

	version := projectManifest.Version
	if version == "" {
		version = "latest"
	}

	repoName := sanitizeRepoName(projectManifest.Name)
	if pushDockerUrl == "" {
		return "", nil, fmt.Errorf("docker url is required for local build and push (use --docker-url flag)")
	}
	imageRef := fmt.Sprintf("%s/%s:%s", strings.TrimSuffix(pushDockerUrl, "/"), repoName, version)

	printer.PrintInfo(fmt.Sprintf("Processing mcp server: %s", projectManifest.Name))
	var serverJSON *apiv0.ServerJSON
	serverJSON, err = translateServerJSON(projectManifest, imageRef, version, githubRepository)
	if err != nil {
		return "", nil, fmt.Errorf("failed to build server JSON for '%v': %w", projectManifest, err)
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
		return "", nil, fmt.Errorf("build failed: %w", err)
	}

	return imageRef, serverJSON, nil
}

func pushDockerImage(imageRef string) error {
	printer.PrintInfo("Pushing Docker image: docker push " + imageRef)
	pushCmd := exec.Command("docker", "push", imageRef)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("docker push failed for %s: %w", imageRef, err)
	}

	return nil
}

func buildAndPushDockerLocal(absPath string, dryRun bool, pushDocker bool, githubRepository string) (*apiv0.ServerJSON, error) {
	printer.PrintInfo(fmt.Sprintf("Building and pushing MCP server Docker image from: %s", absPath))

	// build the docker image
	imageRef, serverJSON, err := buildDockerImage(absPath, githubRepository)
	if err != nil {
		return nil, fmt.Errorf("failed to build docker image: %w", err)
	}

	// push the docker image if --push-docker flag is set
	if pushDocker {
		if dryRun {
			printer.PrintInfo("[DRY RUN] Would push Docker image: " + imageRef)
		} else {
			if err := pushDockerImage(imageRef); err != nil {
				return nil, fmt.Errorf("failed to push docker image: %w", err)
			}
			printer.PrintSuccess("MCP Server pushed successfully!")
		}
	}

	return serverJSON, nil
}

func init() {
	// Flags for push command
	PushCmd.Flags().StringVar(&pushDockerUrl, "docker-url", "", "Docker registry URL (required). For example: docker.io/myorg. The final image name will be <docker-url>/<mcp-server-name>:<tag>")
	PushCmd.Flags().StringVar(&pushDockerTag, "tag", "latest", "Docker image tag to use")
	PushCmd.Flags().StringVar(&pushPlatform, "platform", "", "Target platform (e.g., linux/amd64,linux/arm64)")
	PushCmd.Flags().BoolVar(&pushDryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	PushCmd.Flags().BoolVar(&pushDockerPushFlag, "push-docker", false, "Push to Docker registry")
	PushCmd.Flags().StringVar(&pushGithubRepository, "github", "", "Specify the GitHub repository URL for the MCP server")
	_ = PushCmd.MarkFlagRequired("docker-url")
}

// sanitizeRepoName converts a skill name to a docker-friendly repo name
func sanitizeRepoName(name string) string {
	n := strings.TrimSpace(strings.ToLower(name))
	// replace any non-alphanum or separator with dash
	// also convert path separators to dashes
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-")
	n = replacer.Replace(n)
	// collapse consecutive dashes
	for strings.Contains(n, "--") {
		n = strings.ReplaceAll(n, "--", "-")
	}
	n = strings.Trim(n, "-")
	if n == "" {
		n = "skill"
	}
	return n
}

func translateServerJSON(
	projectManifest *manifest.ProjectManifest,
	imageRef string,
	version string,
	githubRepo string,
) (*apiv0.ServerJSON, error) {
	author := "user"
	if projectManifest.Author != "" {
		author = projectManifest.Author
	}
	name := fmt.Sprintf("%s/%s", strings.ToLower(author), strings.ToLower(projectManifest.Name))

	var repository *model.Repository
	if githubRepo != "" {
		repository = &model.Repository{
			URL:    githubRepo,
			Source: "github",
		}
	}

	return &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        name,
		Description: projectManifest.Description,
		Title:       projectManifest.Name,
		Repository:  repository,
		Version:     version,
		WebsiteURL:  "",
		Icons:       nil,
		Packages: []model.Package{{
			RegistryType:    "oci",
			RegistryBaseURL: "",
			Identifier:      imageRef,
			Version:         version,
			FileSHA256:      "",
			RunTimeHint:     "",
			Transport: model.Transport{
				Type: "stdio",
			},
			RuntimeArguments:     nil,
			PackageArguments:     nil,
			EnvironmentVariables: nil,
		}},
		Remotes: nil,
		Meta:    nil,
	}, nil
}
