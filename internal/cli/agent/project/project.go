package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/adk/python"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
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
func RegenerateDockerCompose(projectDir string, manifest *common.AgentManifest, verbose bool) error {
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

	rendered, err := gen.RenderTemplate(string(templateBytes), struct {
		Name          string
		Image         string
		ModelProvider string
		ModelName     string
		EnvVars       []string
		McpServers    []common.McpServerType
	}{
		Name:          manifest.Name,
		Image:         image,
		ModelProvider: manifest.ModelProvider,
		ModelName:     manifest.ModelName,
		EnvVars:       envVars,
		McpServers:    manifest.McpServers,
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
	sort.Strings(envs)
	return envs
}
