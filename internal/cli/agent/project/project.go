package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/adk/python"
	agentmanifest "github.com/agentregistry-dev/agentregistry/internal/cli/agent/manifest"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// LoadAgent decodes the on-disk v1alpha1.Agent envelope at
// <projectDir>/agent.yaml. The file must carry apiVersion: ar.dev/v1alpha1
// and kind: Agent.
//
// LoadAgent is offline-only; the registry-side resolution of MCP server
// refs into runnable form is done by manifest.Resolve.
func LoadAgent(projectDir string) (*v1alpha1.Agent, error) {
	path := filepath.Join(projectDir, "agent.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("agent.yaml not found in %s", projectDir)
		}
		return nil, fmt.Errorf("reading agent.yaml: %w", err)
	}
	if !scheme.IsEnvelopeYAML(data) {
		return nil, fmt.Errorf("agent.yaml in %s is not a v1alpha1 envelope (expected apiVersion: ar.dev/v1alpha1, kind: Agent)", projectDir)
	}
	var agent v1alpha1.Agent
	if err := v1alpha1.Default.DecodeInto(data, &agent); err != nil {
		return nil, fmt.Errorf("parsing envelope agent.yaml: %w", err)
	}
	return &agent, nil
}

// AgentNameFromManifest attempts to read the agent name, falling back to directory name.
func AgentNameFromManifest(projectDir string) string {
	agent, err := LoadAgent(projectDir)
	if err == nil && agent != nil && agent.Metadata.Name != "" {
		return agent.Metadata.Name
	}
	return filepath.Base(projectDir)
}

// ConstructImageName builds an image reference using defaults when not provided.
func ConstructImageName(flagImage, manifestImage, agentName string) string {
	if flagImage != "" {
		return flagImage
	}
	if manifestImage != "" {
		return manifestImage
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

// RegenerateMcpTools updates the generated mcp_tools.py file based on the
// resolved MCP server set on the runtime manifest.
func RegenerateMcpTools(projectDir string, resolved *agentmanifest.ResolvedAgent, verbose bool) error {
	if resolved == nil || resolved.Agent == nil || resolved.Agent.Metadata.Name == "" {
		return fmt.Errorf("manifest missing name")
	}

	agentPackageDir := filepath.Join(projectDir, resolved.Agent.Metadata.Name)
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
		McpServers []agentmanifest.ResolvedMCPServer
	}{
		McpServers: resolved.MCPServers,
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

// RegeneratePromptsLoader updates the generated prompts_loader.py file.
func RegeneratePromptsLoader(projectDir string, resolved *agentmanifest.ResolvedAgent, verbose bool) error {
	if resolved == nil || resolved.Agent == nil || resolved.Agent.Metadata.Name == "" {
		return fmt.Errorf("manifest missing name")
	}

	agentPackageDir := filepath.Join(projectDir, resolved.Agent.Metadata.Name)
	if _, err := os.Stat(agentPackageDir); err != nil {
		// Not an ADK layout; nothing to do.
		return nil
	}

	gen := python.NewPythonGenerator()
	templateBytes, err := gen.ReadTemplateFile("agent/prompts_loader.py.tmpl")
	if err != nil {
		return fmt.Errorf("failed to read prompts_loader template: %w", err)
	}

	// The template is static (no Go template directives), just write it as-is
	target := filepath.Join(agentPackageDir, "prompts_loader.py")
	if err := os.WriteFile(target, templateBytes, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", target, err)
	}
	if verbose {
		fmt.Printf("Regenerated %s\n", target)
	}
	return nil
}

// RegenerateDockerCompose rewrites docker-compose.yaml using the embedded template.
func RegenerateDockerCompose(projectDir string, resolved *agentmanifest.ResolvedAgent, version string, verbose bool) error {
	if resolved == nil || resolved.Agent == nil {
		return fmt.Errorf("resolved agent is required")
	}
	agent := resolved.Agent

	envVars := EnvVarsFromMCPServers(resolved.MCPServers)
	var specImage string
	if agent.Spec.Source != nil {
		specImage = agent.Spec.Source.Image
	}
	image := ConstructImageName("", specImage, agent.Metadata.Name)
	gen := python.NewPythonGenerator()
	templateBytes, err := gen.ReadTemplateFile("docker-compose.yaml.tmpl")
	if err != nil {
		return fmt.Errorf("failed to read docker-compose template: %w", err)
	}

	// Sanitize version for filesystem use in template
	sanitizedVersion := utils.SanitizeVersion(version)

	rendered, err := gen.RenderTemplate(string(templateBytes), struct {
		Name          string
		Version       string
		Image         string
		Port          int
		ModelProvider string
		ModelName     string
		HasSkills     bool
		EnvVars       []string
		McpServers    []agentmanifest.ResolvedMCPServer
	}{
		Name:          agent.Metadata.Name,
		Version:       sanitizedVersion,
		Image:         image,
		Port:          8080,
		ModelProvider: agent.Spec.ModelProvider,
		ModelName:     agent.Spec.ModelName,
		HasSkills:     len(agent.Spec.Skills) > 0,
		EnvVars:       envVars,
		McpServers:    resolved.MCPServers,
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

// EnvVarsFromMCPServers extracts ${VAR} references from remote-MCP-server
// headers so the runtime can pass them through to docker-compose.
func EnvVarsFromMCPServers(servers []agentmanifest.ResolvedMCPServer) []string {
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
func EnsureMcpServerDirectories(projectDir string, servers []agentmanifest.ResolvedMCPServer, verbose bool) error {
	// Clean up registry/ folder to ensure fresh state for registry-resolved servers.
	// This prevents stale configs from previous runs with different resolved registries.
	if err := CleanupRegistryDir(projectDir, verbose); err != nil {
		return err
	}

	gen := python.NewPythonGenerator()

	for _, srv := range servers {
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
