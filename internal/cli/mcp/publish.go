package mcp

import (
	"encoding/json"
	"fmt"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/build"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/spf13/cobra"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	// Flags for mcp publish command
	dockerUrl       string
	dockerTag       string
	pushFlag        bool
	dryRunFlag      bool
	publishPlatform string
)

var publishCmd = &cobra.Command{
	Use:   "publish <mcp-server-folder-path>",
	Short: "Build and publish an MCP Server as a Docker image",
	Long: `Wrap an MCP Server in a Docker image and publish it to both Docker registry and agent registry.
	
The mcp server folder must contain an mcp.yaml file.`,
	Args: cobra.ExactArgs(1),
	RunE: runMCPServerPublish,
}

func runMCPServerPublish(cmd *cobra.Command, args []string) error {
	serverPath := args[0]

	// Validate path exists
	absPath, err := filepath.Abs(serverPath)
	if err != nil {
		return fmt.Errorf("failed to resolve server path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("server path does not exist: %s", absPath)
	}

	printer.PrintInfo(fmt.Sprintf("Publishing MCP server from: %s", absPath))

	// 1. Load mcp.yaml manifest
	manifestManager := manifest.NewManager(absPath)
	if !manifestManager.Exists() {
		return fmt.Errorf(
			"mcp.yaml not found in %s. Run 'arctl mcp init' first or specify a valid path with --project-dir",
			serverPath,
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
		return fmt.Errorf("docker url is required")
	}
	imageRef := fmt.Sprintf("%s/%s:%s", strings.TrimSuffix(dockerUrl, "/"), repoName, version)

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
) (*apiv0.ServerJSON, error) {
	author := "user"
	if projectManifest.Author != "" {
		author = projectManifest.Author
	}
	name := fmt.Sprintf("%s/%s", strings.ToLower(author), strings.ToLower(projectManifest.Name))
	return &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        name,
		Description: projectManifest.Description,
		Title:       projectManifest.Name,
		Repository:  nil,
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

func init() {
	// Add subcommands to mcp command
	McpCmd.AddCommand(publishCmd)

	// Flags for publish command
	publishCmd.Flags().StringVar(&dockerUrl, "docker-url", "", "Docker registry URL. For example: docker.io/myorg. The final image name will be <docker-url>/<mcp-server-name>:<tag>")
	publishCmd.Flags().BoolVar(&pushFlag, "push", false, "Automatically push to Docker and agent registries")
	publishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	publishCmd.Flags().StringVar(&dockerTag, "tag", "latest", "Docker image tag to use")
	publishCmd.Flags().StringVar(&publishPlatform, "platform", "", "Target platform (e.g., linux/amd64,linux/arm64)")

	_ = publishCmd.MarkFlagRequired("docker-url")
}
