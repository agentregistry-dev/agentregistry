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
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/spf13/cobra"
)

var (
	// Flags for mcp publish command
	dockerUrl           string
	dockerTag           string
	pushFlag            bool
	dryRunFlag          bool
	publishPlatform     string
	publishVersion      string
	githubRepository    string
	publishTransport    string
	publishTransportURL string

	// Flags for package reference publishing (NPM/PyPI/OCI)
	registryType   string
	packageID      string
	packageVersion string
	publishDesc    string
	publishArgs    []string
)

var PublishCmd = &cobra.Command{
	Use:   "publish <mcp-server-folder-path|server-name>",
	Short: "Build and publish an MCP Server or re-publish an existing server",
	Long: `Publish an MCP Server to the registry.

This command supports three modes:
1. Build and publish from local folder: Provide a path to a folder containing mcp.yaml
2. Re-publish existing server: Provide a server name from the registry to change its status to published
3. Publish package reference: Use --registry-type flag to publish NPM/PyPI/OCI package references

Examples:
  # Build and publish from local folder
  arctl mcp publish ./my-server --docker-url docker.io/myorg --push
  
  # Build and publish from local folder and include a repository reference
  arctl mcp publish ./my-server --docker-url docker.io/myorg --push --github https://github.com/repo/user

  # Re-publish an existing server from the registry
  arctl mcp publish io.github.example/my-server --version 1.0.0

  # Publish an NPM package reference (name must be namespace/name format)
  arctl mcp publish myorg/filesystem-server \
    --registry-type npm \
    --package-id @modelcontextprotocol/server-filesystem \
    --version 1.0.0 \
    --description "Filesystem MCP server" \
    --arg /path/to/directory`,

	Args: cobra.ExactArgs(1),
	RunE: runMCPServerPublish,
}

func runMCPServerPublish(cmd *cobra.Command, args []string) error {
	input := args[0]

	// If registry type is provided, we're in package reference mode
	if registryType != "" {
		return publishPackageReference(input)
	}

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
		return fmt.Errorf("version is required for re-publishing existing server, otherwise provide a valid path to a folder containing mcp.yaml")
	}

	// Otherwise, treat it as a server name from the registry
	return publishExistingServer(input, publishVersion)
}

// publishPackageReference publishes an NPM/PyPI/OCI package reference to the registry
func publishPackageReference(serverName string) error {
	// Validate required flags
	if packageID == "" {
		return fmt.Errorf("--package-id is required for package reference publishing")
	}
	if publishDesc == "" {
		return fmt.Errorf("--description is required for package reference publishing")
	}
	if publishVersion == "" {
		return fmt.Errorf("--version is required for package reference publishing")
	}

	// Server name must contain a slash (e.g., "author/server-name")
	if !strings.Contains(serverName, "/") {
		return fmt.Errorf("server name must be in format 'namespace/name' (e.g., 'myorg/my-server')")
	}

	normalizedType := strings.ToLower(registryType)
	if normalizedType != "npm" && normalizedType != "pypi" && normalizedType != "oci" {
		return fmt.Errorf("--registry-type must be one of: npm, pypi, oci (got: %s)", registryType)
	}

	pkgVersion := packageVersion
	if pkgVersion == "" {
		pkgVersion = publishVersion
	}

	// Set runtime hint based on registry type
	var runtimeHint string
	switch normalizedType {
	case "npm":
		runtimeHint = "npx"
	case "pypi":
		runtimeHint = "uvx"
	case "oci":
		runtimeHint = ""
	}

	// Default transport
	transportType := publishTransport
	if transportType == "" {
		transportType = string(model.TransportTypeStdio)
	}

	transportURL := publishTransportURL
	if transportType == string(model.TransportTypeStreamableHTTP) && transportURL == "" {
		transportURL = "http://localhost:3000/mcp"
	}

	// Build repository info if GitHub URL provided (optional and not used for building)
	var repository *model.Repository
	if githubRepository != "" {
		repository = &model.Repository{
			URL:    githubRepository,
			Source: "github",
		}
	}

	// Package arguments from --arg flags are passed to the server binary/package
	var packageArguments []model.Argument
	for _, arg := range publishArgs {
		packageArguments = append(packageArguments, model.Argument{
			InputWithVariables: model.InputWithVariables{
				Input: model.Input{
					Value: arg,
				},
			},
			Type: model.ArgumentTypePositional,
		})
	}

	// Construct ServerJSON
	serverJSON := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        strings.ToLower(serverName),
		Description: publishDesc,
		Title:       serverName,
		Repository:  repository,
		Version:     publishVersion,
		Packages: []model.Package{{
			RegistryType:     normalizedType,
			Identifier:       packageID,
			Version:          pkgVersion,
			RunTimeHint:      runtimeHint,
			PackageArguments: packageArguments,
			Transport: model.Transport{
				Type: transportType,
				URL:  transportURL,
			},
		}},
	}

	// Publish to registry
	if dryRunFlag {
		j, _ := json.Marshal(serverJSON)
		printer.PrintInfo("[DRY RUN] Would publish package reference to registry " + apiClient.BaseURL + ": " + string(j))
		return nil
	}

	printer.PrintInfo(fmt.Sprintf("Publishing %s reference: %s", normalizedType, serverJSON.Name))
	_, err := apiClient.PublishMCPServer(serverJSON)
	if err != nil {
		return fmt.Errorf("failed to publish package reference: %w", err)
	}

	printer.PrintSuccess(fmt.Sprintf("Published: %s (v%s)", serverJSON.Name, publishVersion))
	return nil
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

	version := publishVersion
	if version == "" {
		version = projectManifest.Version
	}
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
	// Flags for publish command
	PublishCmd.Flags().StringVar(&dockerUrl, "docker-url", "", "Docker registry URL (required for local builds). For example: docker.io/myorg. The final image name will be <docker-url>/<mcp-server-name>:<tag>")
	PublishCmd.Flags().BoolVar(&pushFlag, "push", false, "Automatically push to Docker and agent registries (for local builds)")
	PublishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	PublishCmd.Flags().StringVar(&dockerTag, "tag", "latest", "Docker image tag to use (for local builds)")
	PublishCmd.Flags().StringVar(&publishPlatform, "platform", "", "Target platform (e.g., linux/amd64,linux/arm64)")
	PublishCmd.Flags().StringVar(&publishVersion, "version", "", "Specify the version to publish (for re-publishing existing servers, skips interactive selection)")
	PublishCmd.Flags().StringVar(&githubRepository, "github", "", "Specify the GitHub repository URL for the MCP server")
	PublishCmd.Flags().StringVar(&publishTransport, "transport", "", "Transport type: stdio or streamable-http (reads from mcp.yaml if not specified)")
	PublishCmd.Flags().StringVar(&publishTransportURL, "transport-url", "", "Transport URL for streamable-http transport (default: http://localhost:3000/mcp when transport=streamable-http)")

	// Flags for package reference publishing (NPM/PyPI)
	PublishCmd.Flags().StringVar(&registryType, "registry-type", "", "Package registry type: npm, pypi, or oci (enables package reference mode)")
	PublishCmd.Flags().StringVar(&packageID, "package-id", "", "Package identifier (e.g., @modelcontextprotocol/server-filesystem for npm)")
	PublishCmd.Flags().StringVar(&packageVersion, "package-version", "", "Package version (defaults to --version if not specified)")
	PublishCmd.Flags().StringVar(&publishDesc, "description", "", "Server description (required for package reference publishing)")
	PublishCmd.Flags().StringArrayVar(&publishArgs, "arg", nil, "Package argument to pass when running (repeatable, e.g., --arg /path/to/dir)")
}
