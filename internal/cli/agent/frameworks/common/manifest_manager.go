package common

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"gopkg.in/yaml.v3"
)

const ManifestFileName = "agent.yaml"

// Manager handles loading and saving of agent manifests.
type Manager struct {
	projectRoot string
}

// NewManifestManager creates a new manifest manager for the given project root.
func NewManifestManager(projectRoot string) *Manager {
	return &Manager{
		projectRoot: projectRoot,
	}
}

// Load reads and parses the agent.yaml file.
func (m *Manager) Load() (*models.AgentManifest, error) {
	manifestPath := filepath.Join(m.projectRoot, ManifestFileName)

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("agent.yaml not found in %s", m.projectRoot)
		}
		return nil, fmt.Errorf("failed to read agent.yaml: %w", err)
	}

	var manifest models.AgentManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse agent.yaml: %w", err)
	}

	if err := m.Validate(&manifest); err != nil {
		return nil, fmt.Errorf("invalid agent.yaml: %w", err)
	}

	return &manifest, nil
}

// Save writes the manifest to agent.yaml.
func (m *Manager) Save(manifest *models.AgentManifest) error {
	manifest.UpdatedAt = time.Now()

	if err := m.Validate(manifest); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	data, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	manifestPath := filepath.Join(m.projectRoot, ManifestFileName)
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write agent.yaml: %w", err)
	}

	return nil
}

// Validate checks if the manifest is valid.
func (m *Manager) Validate(manifest *models.AgentManifest) error {
	if manifest.Name == "" {
		return fmt.Errorf("agent name is required")
	}
	if manifest.Language == "" {
		return fmt.Errorf("language is required")
	}
	if manifest.Framework == "" {
		return fmt.Errorf("framework is required")
	}

	for i, srv := range manifest.McpServers {
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
		case "command":
			if srv.Command == "" && srv.Image == "" && srv.Build == "" {
				return fmt.Errorf("mcpServers[%d]: at least one of command, image, or build is required for type 'command'", i)
			}
		case "registry":
			if srv.RegistryURL == "" {
				return fmt.Errorf("mcpServers[%d]: registryURL is required for type 'registry'", i)
			}
			if srv.RegistryServerName == "" {
				return fmt.Errorf("mcpServers[%d]: registryServerName is required for type 'registry'", i)
			}
		default:
			return fmt.Errorf("mcpServers[%d]: unsupported type '%s'", i, srv.Type)
		}
	}

	for i, skill := range manifest.Skills {
		if skill.Name == "" {
			return fmt.Errorf("skills[%d]: name is required", i)
		}
		hasImage := skill.Image != ""
		hasPath := skill.Path != ""
		hasRegistry := skill.RegistrySkillName != ""
		count := 0
		if hasImage {
			count++
		}
		if hasPath {
			count++
		}
		if hasRegistry {
			count++
		}
		if count == 0 {
			return fmt.Errorf("skills[%d]: one of image, path, or registrySkillName is required", i)
		}
		if count > 1 {
			return fmt.Errorf("skills[%d]: only one of image, path, or registrySkillName may be set", i)
		}
	}
	return nil
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
