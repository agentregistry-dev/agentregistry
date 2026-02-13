package common

import (
	"fmt"
	"net/url"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/manifest"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

const ManifestFileName = "agent.yaml"

// AgentManifestValidator validates agent manifests.
type AgentManifestValidator struct{}

// Validate checks if the agent manifest is valid.
func (v *AgentManifestValidator) Validate(m *models.AgentManifest) error {
	if m.Name == "" {
		return fmt.Errorf("agent name is required")
	}
	if m.Language == "" {
		return fmt.Errorf("language is required")
	}
	if m.Framework == "" {
		return fmt.Errorf("framework is required")
	}

	for i, srv := range m.McpServers {
		if err := validateMcpServer(i, srv); err != nil {
			return err
		}
	}
	return nil
}

func validateMcpServer(i int, srv models.McpServerType) error {
	if srv.Type == "" {
		return fmt.Errorf("mcpServers[%d]: type is required", i)
	}
	if srv.Name == "" {
		return fmt.Errorf("mcpServers[%d]: name is required", i)
	}
	if srv.Image != "" && srv.Build != "" {
		return fmt.Errorf("mcpServers[%d]: only one of image or build may be set", i)
	}

	switch srv.Type {
	case "remote":
		return validateRemoteMcpServer(i, srv)
	case "command":
		return validateCommandMcpServer(i, srv)
	case "registry":
		return validateRegistryMcpServer(i, srv)
	default:
		return fmt.Errorf("mcpServers[%d]: unsupported type '%s'", i, srv.Type)
	}
}

func validateRemoteMcpServer(i int, srv models.McpServerType) error {
	if srv.URL == "" {
		return fmt.Errorf("mcpServers[%d]: url is required for type 'remote'", i)
	}
	parsed, err := url.Parse(srv.URL)
	if err != nil {
		return fmt.Errorf("mcpServers[%d]: url is not a valid URL: %v", i, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("mcpServers[%d]: url scheme must be http or https", i)
	}
	if parsed.Host == "" {
		return fmt.Errorf("mcpServers[%d]: url is missing host", i)
	}
	return nil
}

func validateCommandMcpServer(i int, srv models.McpServerType) error {
	if srv.Command == "" && srv.Image == "" && srv.Build == "" {
		return fmt.Errorf("mcpServers[%d]: at least one of command, image, or build is required for type 'command'", i)
	}
	return nil
}

func validateRegistryMcpServer(i int, srv models.McpServerType) error {
	if srv.RegistryURL == "" {
		return fmt.Errorf("mcpServers[%d]: registryURL is required for type 'registry'", i)
	}
	if srv.RegistryServerName == "" {
		return fmt.Errorf("mcpServers[%d]: registryServerName is required for type 'registry'", i)
	}
	return nil
}

// Manager wraps the generic manifest manager for agent manifests.
type Manager struct {
	*manifest.Manager[*models.AgentManifest]
}

// NewManifestManager creates a new agent manifest manager.
func NewManifestManager(projectRoot string) *Manager {
	return &Manager{
		Manager: manifest.NewManager(
			projectRoot,
			ManifestFileName,
			&AgentManifestValidator{},
		),
	}
}

// Save updates the timestamp and saves the manifest.
func (m *Manager) Save(man *models.AgentManifest) error {
	man.UpdatedAt = time.Now()
	return m.Manager.Save(man)
}

// NewProjectManifest creates a new AgentManifest with the given values.
func NewProjectManifest(agentName, language, framework, modelProvider, modelName, description string, mcpServers []models.McpServerType) *models.AgentManifest {
	return &models.AgentManifest{
		Name:          agentName,
		Language:      language,
		Framework:     framework,
		ModelProvider: modelProvider,
		ModelName:     modelName,
		Description:   description,
		UpdatedAt:     time.Now(),
		McpServers:    mcpServers,
	}
}
