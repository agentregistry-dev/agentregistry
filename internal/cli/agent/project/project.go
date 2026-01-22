package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/adk/python"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/internal/version"
)

// LoadManifest loads the agent manifest from the project directory.
func LoadManifest(projectDir string) (*common.AgentManifest, error) {
	manager := common.NewManifestManager(projectDir)
	return manager.Load()
}

// AgentNameFromManifest attempts to read the agent name, falling back to directory name.
func AgentNameFromManifest(projectDir string) string {
	manager := common.NewManifestManager(projectDir)
	manifest, err := manager.Load()
	if err == nil && manifest != nil && manifest.Name != "" {
		return manifest.Name
	}
	return filepath.Base(projectDir)
}

// ConstructImageName builds an image reference using defaults when not provided.
func ConstructImageName(configuredImage, agentName string) string {
	if configuredImage != "" {
		return configuredImage
	}
	return fmt.Sprintf("%s/%s:latest", defaultRegistry(), agentName)
}

// ConstructMCPServerImageName builds the image name for a command MCP server.
func ConstructMCPServerImageName(agentName, serverName string) string {
	if agentName == "" {
		agentName = "agent"
	}
	image := fmt.Sprintf("%s-%s", agentName, serverName)
	return fmt.Sprintf("%s/%s:latest", defaultRegistry(), image)
}

func defaultRegistry() string {
	registry := strings.TrimSuffix(version.DockerRegistry, "/")
	if registry == "" {
		return "localhost:5001"
	}
	return registry
}

// RegenerateMcpTools updates the generated mcp_tools.py file based on manifest state.
func RegenerateMcpTools(projectDir string, manifest *common.AgentManifest, verbose bool) error {
	if manifest == nil || manifest.Name == "" {
		return fmt.Errorf("manifest missing name")
	}

	agentPackageDir := filepath.Join(projectDir, manifest.Name)
	if _, err := os.Stat(agentPackageDir); err != nil {
		// Not an ADK layout; nothing to do.
		return nil
	}

	gen := python.NewPythonGenerator()
	templateBytes, err := gen.ReadTemplateFile("agent/mcp_tools.py.tmpl")
	if err != nil {
		return fmt.Errorf("failed to read mcp_tools template: %w", err)
	}

	rendered, err := gen.RenderTemplate(string(templateBytes), struct {
		McpServers []common.McpServerType
	}{
		McpServers: manifest.McpServers,
	})
	if err != nil {
		return fmt.Errorf("failed to render mcp_tools template: %w", err)
	}

	target := filepath.Join(agentPackageDir, "mcp_tools.py")
	if err := os.WriteFile(target, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", target, err)
	}
	if verbose {
		fmt.Printf("Regenerated %s\n", target)
	}
	return nil
}

// RegenerateDockerCompose rewrites docker-compose.yaml using the embedded template.
func RegenerateDockerCompose(projectDir string, manifest *common.AgentManifest, version string, verbose bool) error {
	if manifest == nil {
		return fmt.Errorf("manifest is required")
	}

	envVars := EnvVarsFromManifest(manifest)
	image := manifest.Image
	if image == "" {
		image = ConstructImageName("", manifest.Name)
	}
	gen := python.NewPythonGenerator()
	templateBytes, err := gen.ReadTemplateFile("docker-compose.yaml.tmpl")
	if err != nil {
		return fmt.Errorf("failed to read docker-compose template: %w", err)
	}

	// Sanitize version for filesystem use in template
	sanitizedVersion := utils.SanitizeVersion(version)

	rendered, err := gen.RenderTemplate(string(templateBytes), struct {
		Name              string
		Version           string
		Image             string
		ModelProvider     string
		ModelName         string
		TelemetryEndpoint string
		EnvVars           []string
		McpServers        []common.McpServerType
	}{
		Name:              manifest.Name,
		Version:           sanitizedVersion,
		Image:             image,
		ModelProvider:     manifest.ModelProvider,
		ModelName:         manifest.ModelName,
		TelemetryEndpoint: manifest.TelemetryEndpoint,
		EnvVars:           envVars,
		McpServers:        manifest.McpServers,
	})
	if err != nil {
		return fmt.Errorf("failed to render docker-compose: %w", err)
	}

	target := filepath.Join(projectDir, "docker-compose.yaml")
	if err := os.WriteFile(target, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yaml: %w", err)
	}

	if verbose {
		fmt.Printf("Updated %s\n", target)
	}
	return nil
}

// EnvVarsFromManifest extracts environment variables referenced in MCP headers.
func EnvVarsFromManifest(manifest *common.AgentManifest) []string {
	return extractEnvVarsFromHeaders(manifest.McpServers)
}

func extractEnvVarsFromHeaders(servers []common.McpServerType) []string {
	envSet := map[string]struct{}{}
	re := regexp.MustCompile(`\$\{([^}]+)\}`)

	for _, srv := range servers {
		if srv.Type != "remote" || srv.Headers == nil {
			continue
		}
		for _, value := range srv.Headers {
			for _, match := range re.FindAllStringSubmatch(value, -1) {
				if len(match) > 1 {
					envSet[match[1]] = struct{}{}
				}
			}
		}
	}

	if len(envSet) == 0 {
		return nil
	}

	var envs []string
	for name := range envSet {
		envs = append(envs, name)
	}
	slices.Sort(envs)
	return envs
}

// mcpTarget represents an MCP server target for config.yaml template.
type mcpTarget struct {
	Name  string
	Cmd   string
	Args  []string
	Env   []string
	Image string
	Build string
}

// EnsureMcpServerDirectories creates config.yaml and Dockerfile for command-type MCP servers.
// For registry-resolved servers, srv.Build contains the folder path (e.g., "registry/<name>").
// For locally-defined servers, srv.Build is empty and srv.Name is used as the folder.
func EnsureMcpServerDirectories(projectDir string, manifest *common.AgentManifest, verbose bool) error {
	if manifest == nil {
		return nil
	}

	// Clean up registry/ folder to ensure fresh state for registry-resolved servers.
	// This prevents stale configs from previous runs with different resolved registries.
	if err := CleanupRegistryDir(projectDir, verbose); err != nil {
		return err
	}

	gen := python.NewPythonGenerator()

	for _, srv := range manifest.McpServers {
		// Skip remote type servers as they don't need local directories
		if srv.Type != "command" {
			continue
		}

		// Determine the directory path:
		// - For registry-resolved servers: srv.Build contains the path (e.g., "registry/pokemon")
		// - For locally-defined servers: use srv.Name as the folder name
		folderPath := srv.Name
		if srv.Build != "" {
			folderPath = srv.Build
		}

		mcpServerDir := filepath.Join(projectDir, folderPath)
		if err := os.MkdirAll(mcpServerDir, 0o755); err != nil {
			return fmt.Errorf("failed to create %s directory: %w", folderPath, err)
		}

		// Transform this specific server into a target for config.yaml template
		targets := []mcpTarget{
			{
				Name:  srv.Name,
				Cmd:   srv.Command,
				Args:  srv.Args,
				Env:   srv.Env,
				Image: srv.Image,
				Build: srv.Build,
			},
		}

		// Render and write config.yaml
		templateData := struct {
			Targets []mcpTarget
		}{
			Targets: targets,
		}

		configTemplateBytes, err := gen.ReadTemplateFile("mcp_server/config.yaml.tmpl")
		if err != nil {
			return fmt.Errorf("failed to read config.yaml template for %s: %w", srv.Name, err)
		}

		renderedContent, err := gen.RenderTemplate(string(configTemplateBytes), templateData)
		if err != nil {
			return fmt.Errorf("failed to render config.yaml template for %s: %w", srv.Name, err)
		}

		configPath := filepath.Join(mcpServerDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte(renderedContent), 0o644); err != nil {
			return fmt.Errorf("failed to write config.yaml for %s: %w", srv.Name, err)
		}

		if verbose {
			fmt.Printf("Created/updated %s\n", configPath)
		}

		// Copy Dockerfile if it doesn't exist (always overwrite for registry-resolved servers to ensure fresh state)
		dockerfilePath := filepath.Join(mcpServerDir, "Dockerfile")
		isRegistryServer := srv.Build != "" && strings.HasPrefix(srv.Build, "registry/")
		if isRegistryServer || !fileExists(dockerfilePath) {
			dockerfileBytes, err := gen.ReadTemplateFile("mcp_server/Dockerfile")
			if err != nil {
				return fmt.Errorf("failed to read Dockerfile template for %s: %w", srv.Name, err)
			}

			if err := os.WriteFile(dockerfilePath, dockerfileBytes, 0o644); err != nil {
				return fmt.Errorf("failed to write Dockerfile for %s: %w", srv.Name, err)
			}

			if verbose {
				fmt.Printf("Created %s\n", dockerfilePath)
			}
		}
	}

	return nil
}

// fileExists checks if a file exists at the given path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// CleanupRegistryDir removes the generated registry directory if it exists.
// This keeps registry-resolved MCP server artifacts from sticking around across runs.
func CleanupRegistryDir(projectDir string, verbose bool) error {
	registryDir := filepath.Join(projectDir, "registry")

	// If the directory does not exist, nothing to do.
	if _, err := os.Stat(registryDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat registry directory: %w", err)
	}

	if err := os.RemoveAll(registryDir); err != nil {
		return fmt.Errorf("failed to clean up registry directory: %w", err)
	}

	if verbose {
		fmt.Println("Cleaned up registry/ folder for fresh server configs")
	}
	return nil
}

// ResolveProjectDir resolves the project directory path
func ResolveProjectDir(projectDir string) (string, error) {
	if projectDir == "" {
		projectDir = "."
	}
	absPath, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve project directory: %w", err)
	}
	return absPath, nil
}
