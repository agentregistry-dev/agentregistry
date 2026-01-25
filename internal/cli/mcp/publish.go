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
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	dockerUrl           string
	dockerTag           string
	pushFlag            bool
	dryRunFlag          bool
	publishPlatform     string
	publishVersion      string
	githubRepository    string
	publishTransport    string
	publishTransportURL string
	fromGitHub          string
	gitBranch           string
)

var PublishCmd = &cobra.Command{
	Use:   "publish <mcp-server-folder-path|server-name>",
	Short: "Build and publish an MCP Server or re-publish an existing server",
	Long: `Publish an MCP Server to the registry.

This command supports three modes:
1. Build and publish from local folder: Provide a path to a folder containing mcp.yaml
2. Re-publish existing server: Provide a server name from the registry to change its status to published
3. Publish from GitHub: Use --from-github to publish directly from a GitHub repository

Examples:
  # Build and publish from local folder
  arctl mcp publish ./my-server --docker-url docker.io/myorg --push

  # Re-publish an existing server from the registry
  arctl mcp publish io.github.example/my-server --version 1.0.0

  # Publish from GitHub repository (metadata only)
  arctl mcp publish --from-github https://github.com/myorg/my-mcp-server

  # Publish from GitHub with pre-built Docker image
  arctl mcp publish --from-github https://github.com/myorg/my-mcp-server --docker-url docker.io/myorg/my-server:latest`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMCPServerPublish,
}

func runMCPServerPublish(cmd *cobra.Command, args []string) error {
	if fromGitHub != "" {
		return publishMCPFromGitHub(fromGitHub, gitBranch)
	}

	if len(args) == 0 {
		return cmd.Help()
	}

	input := args[0]

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

	if isLocalPath {
		return buildAndPublishLocal(absPath)
	}

	if publishVersion == "" {
		return fmt.Errorf("version is required")
	}

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
	if dockerUrl == "" {
		return fmt.Errorf("docker url is required for local build and publish (use --docker-url flag)")
	}
	imageRef := fmt.Sprintf("%s/%s:%s", strings.TrimSuffix(dockerUrl, "/"), repoName, version)

	printer.PrintInfo(fmt.Sprintf("Processing mcp server: %s", projectManifest.Name))

	// Determine transport type and URL from manifest or flags (flags take precedence)
	transportType := publishTransport
	transportURL := publishTransportURL

	if projectManifest.Transport != nil {
		// Use manifest values if flags are not set
		if transportType == "" {
			transportType = projectManifest.Transport.Type
		}
		if transportURL == "" {
			transportURL = projectManifest.Transport.URL
		}
	}

	serverJSON, err := translateServerJSON(projectManifest, imageRef, version, githubRepository, transportType, transportURL)
	if err != nil {
		return fmt.Errorf("failed to build server JSON for '%v': %w", projectManifest, err)
	}

	// 2. Build Docker image
	builder := build.New()
	opts := build.Options{
		ProjectDir: absPath,
		Tag:        imageRef,
		Platform:   publishPlatform,
		Verbose:    verbose,
	}

	if err := builder.Build(opts); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	// 3. Push to Docker registry (if --push flag)
	if pushFlag {
		if dryRunFlag {
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

	// 4. Publish to agent registry
	if dryRunFlag {
		j, _ := json.Marshal(serverJSON)
		printer.PrintInfo("[DRY RUN] Would publish mcp server to registry " + apiClient.BaseURL + ": " + string(j))
	} else {
		_, err = apiClient.PublishMCPServer(serverJSON)
		if err != nil {
			return fmt.Errorf("failed to publish mcp server to registry: %w", err)
		}
		printer.PrintSuccess("MCP Server publishing complete!")
	}

	return nil
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
	transportType string,
	transportURL string,
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

	// Default to stdio if not specified
	if transportType == "" {
		transportType = string(model.TransportTypeStdio)
	}

	// If streamable-http transport is specified but no URL, use default
	if transportType == string(model.TransportTypeStreamableHTTP) && transportURL == "" {
		transportURL = "http://localhost:3000/mcp"
	}

	var runtimeArguments []model.Argument
	for _, arg := range projectManifest.RuntimeArgs {
		runtimeArguments = append(runtimeArguments, model.Argument{
			InputWithVariables: model.InputWithVariables{
				Input: model.Input{
					Value: arg,
				},
			},
			Type: model.ArgumentTypePositional,
		})
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
			RunTimeHint:     projectManifest.RuntimeHint,
			Transport: model.Transport{
				Type: transportType,
				URL:  transportURL,
			},
			RuntimeArguments:     runtimeArguments,
			PackageArguments:     nil,
			EnvironmentVariables: nil,
		}},
		Remotes: nil,
		Meta:    nil,
	}, nil
}

func init() {
	PublishCmd.Flags().StringVar(&dockerUrl, "docker-url", "", "Docker registry URL (required for local builds). For example: docker.io/myorg. The final image name will be <docker-url>/<mcp-server-name>:<tag>")
	PublishCmd.Flags().BoolVar(&pushFlag, "push", false, "Automatically push to Docker and agent registries (for local builds)")
	PublishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	PublishCmd.Flags().StringVar(&dockerTag, "tag", "latest", "Docker image tag to use (for local builds)")
	PublishCmd.Flags().StringVar(&publishPlatform, "platform", "", "Target platform (e.g., linux/amd64,linux/arm64)")
	PublishCmd.Flags().StringVar(&publishVersion, "version", "", "Specify the version to publish (for re-publishing existing servers, skips interactive selection)")
	PublishCmd.Flags().StringVar(&githubRepository, "github", "", "Specify the GitHub repository URL for the MCP server")
	PublishCmd.Flags().StringVar(&publishTransport, "transport", "", "Transport type: stdio or streamable-http (reads from mcp.yaml if not specified)")
	PublishCmd.Flags().StringVar(&publishTransportURL, "transport-url", "", "Transport URL for streamable-http transport (default: http://localhost:3000/mcp when transport=streamable-http)")
	PublishCmd.Flags().StringVar(&fromGitHub, "from-github", "", "Publish MCP server directly from a GitHub repository URL")
	PublishCmd.Flags().StringVar(&gitBranch, "branch", "main", "Branch to use when publishing from GitHub")
}

func publishMCPFromGitHub(repoURL, branch string) error {
	repoInfo, err := utils.ParseGitHubURL(repoURL)
	if err != nil {
		return fmt.Errorf("invalid GitHub URL: %w", err)
	}

	if branch != "" {
		repoInfo.Branch = branch
	}

	printer.PrintInfo(fmt.Sprintf("Fetching mcp.yaml from %s (branch: %s)...", repoInfo.GetGitHubRepoURL(), repoInfo.Branch))

	content, err := utils.FetchGitHubRawFile(repoInfo, "mcp.yaml")
	if err != nil {
		return fmt.Errorf("failed to fetch mcp.yaml: %w", err)
	}

	var projectManifest manifest.ProjectManifest
	if err := yaml.Unmarshal(content, &projectManifest); err != nil {
		return fmt.Errorf("failed to parse mcp.yaml: %w", err)
	}

	version := projectManifest.Version
	if publishVersion != "" {
		version = publishVersion
	}
	if version == "" {
		version = "latest"
	}

	transportType := publishTransport
	transportURL := publishTransportURL

	if projectManifest.Transport != nil {
		if transportType == "" {
			transportType = projectManifest.Transport.Type
		}
		if transportURL == "" {
			transportURL = projectManifest.Transport.URL
		}
	}

	var imageRef string
	if dockerUrl != "" {
		repoName := sanitizeRepoName(projectManifest.Name)
		imageRef = fmt.Sprintf("%s/%s:%s", strings.TrimSuffix(dockerUrl, "/"), repoName, version)
	}

	serverJSON, err := translateServerJSON(&projectManifest, imageRef, version, repoInfo.GetGitHubRepoURL(), transportType, transportURL)
	if err != nil {
		return fmt.Errorf("failed to build server JSON: %w", err)
	}

	if dryRunFlag {
		j, _ := json.Marshal(serverJSON)
		printer.PrintInfo("[DRY RUN] Would publish mcp server to registry: " + string(j))
		return nil
	}

	_, err = apiClient.PublishMCPServer(serverJSON)
	if err != nil {
		return fmt.Errorf("failed to publish MCP server: %w", err)
	}

	printer.PrintSuccess(fmt.Sprintf("MCP Server '%s' version %s published from GitHub: %s", serverJSON.Name, version, repoInfo.GetGitHubRepoURL()))

	return nil
}
