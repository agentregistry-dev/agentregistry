package manifest

import (
	"fmt"
	"slices"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/manifest"
)

const ManifestFileName = "mcp.yaml"

// ManifestLoader interface for loading manifests.
type ManifestLoader interface {
	Exists() bool
	Load() (*ProjectManifest, error)
}

// MCPManifestValidator validates MCP project manifests.
type MCPManifestValidator struct{}

// Validate checks if the manifest is valid.
func (v *MCPManifestValidator) Validate(m *ProjectManifest) error {
	if m.Name == "" {
		return fmt.Errorf("project name is required")
	}

	if m.Framework == "" {
		return fmt.Errorf("framework is required")
	}

	if !isValidFramework(m.Framework) {
		return fmt.Errorf("unsupported framework: %s", m.Framework)
	}

	for toolName, tool := range m.Tools {
		if err := validateTool(toolName, tool); err != nil {
			return fmt.Errorf("invalid tool %s: %w", toolName, err)
		}
	}

	if err := validateSecrets(m.Secrets); err != nil {
		return fmt.Errorf("invalid secrets config: %w", err)
	}

	return nil
}

func validateTool(_ string, tool ToolConfig) error {
	if tool.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	return nil
}

func validateSecrets(secrets SecretsConfig) error {
	for env, config := range secrets {
		if config.Provider != "" && !isValidSecretProvider(config.Provider) {
			return fmt.Errorf("invalid secret provider for environment %s: %s", env, config.Provider)
		}
	}
	return nil
}

func isValidFramework(framework string) bool {
	validFrameworks := []string{
		FrameworkFastMCPPython,
		FrameworkMCPGo,
		FrameworkTypeScript,
		FrameworkJava,
	}
	return slices.Contains(validFrameworks, framework)
}

func isValidSecretProvider(provider string) bool {
	validProviders := []string{
		SecretProviderEnv,
		SecretProviderKubernetes,
	}
	return slices.Contains(validProviders, provider)
}

// Manager wraps the generic manifest manager for MCP project manifests.
type Manager struct {
	*manifest.Manager[*ProjectManifest]
}

// NewManager creates a new MCP manifest manager.
func NewManager(projectRoot string) *Manager {
	return &Manager{
		Manager: manifest.NewManager(
			projectRoot,
			ManifestFileName,
			&MCPManifestValidator{},
		),
	}
}

// Save updates the timestamp and saves the manifest.
func (m *Manager) Save(man *ProjectManifest) error {
	man.UpdatedAt = time.Now()
	return m.Manager.Save(man)
}

// AddTool adds a new tool to the manifest.
func (m *Manager) AddTool(man *ProjectManifest, name string, config ToolConfig) error {
	if name == "" {
		return fmt.Errorf("tool name is required")
	}

	if err := validateTool(name, config); err != nil {
		return err
	}

	if man.Tools == nil {
		man.Tools = make(map[string]ToolConfig)
	}

	man.Tools[name] = config
	return nil
}

// RemoveTool removes a tool from the manifest.
func (m *Manager) RemoveTool(man *ProjectManifest, name string) error {
	if man.Tools == nil {
		return fmt.Errorf("tool %s not found", name)
	}

	if _, exists := man.Tools[name]; !exists {
		return fmt.Errorf("tool %s not found", name)
	}

	delete(man.Tools, name)
	return nil
}

// GetDefault returns a new ProjectManifest with default values.
func GetDefault(name, framework, description, author, email, version string) *ProjectManifest {
	if description == "" {
		description = fmt.Sprintf("MCP server built with %s", framework)
	}

	runtimeHint, runtimeArgs := getFrameworkRuntimeConfig(framework)

	return &ProjectManifest{
		Name:        name,
		Framework:   framework,
		Version:     version,
		Description: description,
		Author:      author,
		Email:       email,
		Tools:       make(map[string]ToolConfig),
		Secrets: SecretsConfig{
			"local": {
				Enabled:  false,
				Provider: SecretProviderEnv,
				File:     ".env.local",
			},
		},
		RuntimeHint: runtimeHint,
		RuntimeArgs: runtimeArgs,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

func getFrameworkRuntimeConfig(framework string) (string, []string) {
	switch framework {
	case FrameworkFastMCPPython:
		return "python", []string{"src/main.py"}
	case FrameworkMCPGo:
		return "/app/server", nil
	case FrameworkTypeScript:
		return "node", []string{"dist/index.js"}
	default:
		return "", nil
	}
}
