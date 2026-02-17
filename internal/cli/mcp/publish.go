package mcp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/spf13/cobra"
)

const defaultTransportURL = "http://localhost:3000/mcp"

// registryTypeRuntimeHints maps valid registry types to their default runtime hints.
var registryTypeRuntimeHints = map[string]string{
	"npm":  "npx",
	"pypi": "uvx",
	"oci":  "",
}

var (
	// Flags for mcp publish command
	dryRunFlag          bool
	overwriteFlag       bool
	publishVersion      string
	githubRepository    string
	publishTransport    string
	publishTransportURL string

	// Flags for package reference publishing
	registryType   string
	packageID      string
	packageVersion string
	publishDesc    string
	publishArgs    []string
)

func init() {
	PublishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	PublishCmd.Flags().BoolVar(&overwriteFlag, "overwrite", false, "Overwrite if the version is already published")
	PublishCmd.Flags().StringVar(&publishVersion, "version", "", "Server version")
	PublishCmd.Flags().StringVar(&githubRepository, "github", "", "GitHub repository URL")
	PublishCmd.Flags().StringVar(&publishTransport, "transport", "", "Transport type: stdio or streamable-http")
	PublishCmd.Flags().StringVar(&publishTransportURL, "transport-url", "", "Transport URL for streamable-http transport")

	PublishCmd.Flags().StringVar(&registryType, "type", "", "Package registry type: npm, pypi, or oci")
	PublishCmd.Flags().StringVar(&packageID, "package-id", "", "Package identifier (e.g., docker.io/org/image:tag, @mcp/server)")
	PublishCmd.Flags().StringVar(&packageVersion, "package-version", "", "Package version (defaults to --version)")
	PublishCmd.Flags().StringVar(&publishDesc, "description", "", "Server description")
	PublishCmd.Flags().StringArrayVar(&publishArgs, "arg", nil, "Package argument (repeatable)")

	_ = PublishCmd.MarkFlagRequired("package-id")
	_ = PublishCmd.MarkFlagRequired("type")
}

var PublishCmd = &cobra.Command{
	Use:   "publish [server-name|local-path]",
	Short: "Publish an MCP server to the registry",
	Long: `Publish an MCP server package reference to the registry.

Required flags:
  --type        Package registry type (oci, npm, or pypi)
  --package-id  Package identifier (Docker image, npm package, or PyPI package)

If no argument is provided and mcp.yaml exists in the current directory, metadata is read from it.
If a local path is provided, metadata (name, version, description) is read from mcp.yaml.
Otherwise, --version and --description are required.

Examples:
  # Publish from current folder (reads metadata from mcp.yaml)
  arctl mcp publish \
    --type oci \
    --package-id docker.io/myorg/my-server:1.0.0

  # Publish an OCI image with explicit server name
  arctl mcp publish myorg/my-server \
    --type oci \
    --package-id docker.io/myorg/my-server:1.0.0 \
    --version 1.0.0 \
    --description "My MCP server"

  # Publish an NPM package
  arctl mcp publish myorg/filesystem-server \
    --type npm \
    --package-id @modelcontextprotocol/server-filesystem \
    --version 1.0.0 \
    --description "Filesystem MCP server" \
    --arg /path/to/directory

  # Publish a PyPI package
  arctl mcp publish myorg/server \
    --type pypi \
    --package-id mcp-server-package \
    --version 1.0.0 \
    --description "Python MCP server"

  # Publish from specific local folder
  arctl mcp publish ./my-server \
    --type oci \
    --package-id docker.io/myorg/my-server:1.0.0`,

	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,  // Don't show usage on removal errors
	SilenceErrors: false, // Still show error messages
	RunE:          runMCPServerPublish,
}

func runMCPServerPublish(cmd *cobra.Command, args []string) error {
	// Default to current directory if no argument provided
	input := "."
	if len(args) > 0 {
		input = args[0]
	}

	var serverName, description, version string
	var runtimeArgs []string
	var runtimeHint string

	// Check if input is a local path with mcp.yaml
	absPath, _ := filepath.Abs(input)
	manifestManager := manifest.NewManager(absPath)

	if manifestManager.Exists() {
		// Load metadata from mcp.yaml
		projectManifest, err := manifestManager.Load()
		if err != nil {
			return fmt.Errorf("failed to load project manifest: %w", err)
		}

		serverName = common.BuildMCPServerRegistryName(projectManifest.Author, projectManifest.Name)
		description = projectManifest.Description
		version = common.ResolveVersion(publishVersion, projectManifest.Version)
		runtimeArgs = projectManifest.RuntimeArgs
		runtimeHint = projectManifest.RuntimeHint
	} else {
		// Use command line arguments
		serverName = strings.ToLower(input)
		description = publishDesc
		version = publishVersion

		// Validate required flags when not using local path
		if !strings.Contains(serverName, "/") {
			return fmt.Errorf("server name must be in format 'namespace/name' (e.g., 'myorg/my-server')")
		}
		if description == "" {
			return fmt.Errorf("--description is required when not publishing from a local folder")
		}
		if version == "" {
			return fmt.Errorf("--version is required when not publishing from a local folder")
		}
	}

	// Validate registry type
	regType, err := validateRegistryType(registryType)
	if err != nil {
		return err
	}

	// Check if server already exists
	if err := checkAndHandleExistingServer(serverName, version); err != nil {
		return err
	}

	pkgVersion := packageVersion
	if pkgVersion == "" {
		pkgVersion = version
	}

	transportType, transportURL, err := resolveTransport(publishTransport, publishTransportURL)
	if err != nil {
		return err
	}

	serverJSON := buildServerJSON(ServerJSONParams{
		Name:             serverName,
		Description:      description,
		Title:            serverName,
		Version:          version,
		GithubURL:        githubRepository,
		RegistryType:     regType,
		Identifier:       packageID,
		PackageVersion:   pkgVersion,
		PackageArguments: publishArgs,
		RuntimeHint:      runtimeHint,
		RuntimeArguments: runtimeArgs,
		TransportType:    transportType,
		TransportURL:     transportURL,
	})

	return publishToRegistry(serverJSON, dryRunFlag)
}

// validateRegistryType validates the registry type.
func validateRegistryType(specifiedType string) (string, error) {
	normalized := strings.ToLower(specifiedType)
	if _, valid := registryTypeRuntimeHints[normalized]; !valid {
		return "", fmt.Errorf("--type must be one of: npm, pypi, oci (got: %s)", specifiedType)
	}
	return normalized, nil
}

// resolveTransport returns the transport type and URL with defaults applied.
func resolveTransport(transportType, transportURL string) (string, string, error) {
	if transportType != "" && transportType != string(model.TransportTypeStdio) && transportType != string(model.TransportTypeStreamableHTTP) {
		return "", "", fmt.Errorf("invalid transport type: %s. Must be 'stdio' or 'streamable-http'", transportType)
	}

	if transportType == "" {
		transportType = string(model.TransportTypeStdio)
		return transportType, "", nil
	}

	if transportType == string(model.TransportTypeStreamableHTTP) && transportURL == "" {
		printer.PrintInfo(fmt.Sprintf("Transport type '%s' specified but no URL provided, defaulting to %s",
			model.TransportTypeStreamableHTTP, defaultTransportURL))
		transportURL = defaultTransportURL
	}
	return transportType, transportURL, nil
}

// buildRepository creates a Repository from a GitHub URL, or nil if empty.
func buildRepository(githubURL string) *model.Repository {
	if githubURL == "" {
		return nil
	}
	return &model.Repository{
		URL:    githubURL,
		Source: "github",
	}
}

// buildArguments converts a slice of strings to model.Argument slice.
func buildArguments(args []string) []model.Argument {
	if len(args) == 0 {
		return nil
	}
	arguments := make([]model.Argument, 0, len(args))
	for _, arg := range args {
		arguments = append(arguments, model.Argument{
			InputWithVariables: model.InputWithVariables{
				Input: model.Input{
					Value: arg,
				},
			},
			Type: model.ArgumentTypePositional,
		})
	}
	return arguments
}

// checkAndHandleExistingServer checks if a server version already exists in the registry
// and handles the overwrite logic if needed.
func checkAndHandleExistingServer(serverName, version string) error {
	printer.PrintInfo(fmt.Sprintf("Publishing MCP server: %s (v%s)", serverName, version))

	isPublished, err := isServerPublished(serverName, version)
	if err != nil {
		return fmt.Errorf("error querying registry: %w", err)
	}
	if isPublished {
		if !overwriteFlag {
			return fmt.Errorf("server %s version %s already exists in the registry. Use --overwrite to replace it", serverName, version)
		}
		printer.PrintInfo(fmt.Sprintf("Overwriting existing server %s version %s", serverName, version))
		if err := apiClient.DeleteMCPServer(serverName, version); err != nil {
			return fmt.Errorf("failed to delete existing server: %w", err)
		}
	}
	return nil
}

// publishToRegistry handles the actual publish or dry-run output.
func publishToRegistry(serverJSON *apiv0.ServerJSON, dryRun bool) error {
	if dryRun {
		j, _ := json.MarshalIndent(serverJSON, "", "  ")
		printer.PrintInfo(fmt.Sprintf("[DRY RUN] Would publish to registry %s:\n%s", apiClient.BaseURL, string(j)))
		return nil
	}

	_, err := apiClient.CreateMCPServer(serverJSON)
	if err != nil {
		return fmt.Errorf("failed to publish to registry: %w", err)
	}
	printer.PrintSuccess(fmt.Sprintf("Published: %s (v%s)", serverJSON.Name, serverJSON.Version))
	return nil
}

// ServerJSONParams contains all parameters needed to build a ServerJSON.
type ServerJSONParams struct {
	Name        string
	Description string
	Title       string
	Version     string
	GithubURL   string

	// Package info
	RegistryType     string
	Identifier       string
	PackageVersion   string
	RuntimeHint      string
	RuntimeArguments []string
	PackageArguments []string
	TransportType    string
	TransportURL     string
}

// buildServerJSON constructs a ServerJSON from parameters.
func buildServerJSON(p ServerJSONParams) *apiv0.ServerJSON {
	return &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        p.Name,
		Description: p.Description,
		Title:       p.Title,
		Repository:  buildRepository(p.GithubURL),
		Version:     p.Version,
		Packages: []model.Package{{
			RegistryType:     p.RegistryType,
			Identifier:       p.Identifier,
			Version:          p.PackageVersion,
			RunTimeHint:      p.RuntimeHint,
			RuntimeArguments: buildArguments(p.RuntimeArguments),
			PackageArguments: buildArguments(p.PackageArguments),
			Transport: model.Transport{
				Type: p.TransportType,
				URL:  p.TransportURL,
			},
		}},
	}
}
