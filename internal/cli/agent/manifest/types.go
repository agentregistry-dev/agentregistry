package manifest

import (
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// RegistryRef is the workflow-local reference shape used by the agent runtime.
type RegistryRef struct {
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
}

// AgentManifest is the workflow/project manifest consumed by the local agent CLI.
// It intentionally stays internal to the CLI even while the registry API itself
// speaks v1alpha1 envelopes end-to-end.
type AgentManifest struct {
	Name              string          `yaml:"agentName" json:"name"`
	Image             string          `yaml:"image" json:"image"`
	Language          string          `yaml:"language" json:"language"`
	Framework         string          `yaml:"framework" json:"framework"`
	ModelProvider     string          `yaml:"modelProvider" json:"modelProvider"`
	ModelName         string          `yaml:"modelName" json:"modelName"`
	Description       string          `yaml:"description" json:"description"`
	Version           string          `yaml:"version,omitempty" json:"version,omitempty"`
	TelemetryEndpoint string          `yaml:"telemetryEndpoint,omitempty" json:"telemetryEndpoint,omitempty"`
	McpServers        []McpServerType `yaml:"mcpServers,omitempty" json:"mcpServers,omitempty"`
	Skills            []SkillRef      `yaml:"skills,omitempty" json:"skills,omitempty"`
	Prompts           []PromptRef     `yaml:"prompts,omitempty" json:"prompts,omitempty"`
	UpdatedAt         time.Time       `yaml:"updatedAt,omitempty" json:"updatedAt,omitempty"`
}

type SkillRef struct {
	Name                 string `yaml:"name" json:"name"`
	Image                string `yaml:"image,omitempty" json:"image,omitempty"`
	RegistryURL          string `yaml:"registryURL,omitempty" json:"registryURL,omitempty"`
	RegistrySkillName    string `yaml:"registrySkillName,omitempty" json:"registrySkillName,omitempty"`
	RegistrySkillVersion string `yaml:"registrySkillVersion,omitempty" json:"registrySkillVersion,omitempty"`
}

type PromptRef struct {
	Name                  string `yaml:"name" json:"name"`
	RegistryURL           string `yaml:"registryURL,omitempty" json:"registryURL,omitempty"`
	RegistryPromptName    string `yaml:"registryPromptName,omitempty" json:"registryPromptName,omitempty"`
	RegistryPromptVersion string `yaml:"registryPromptVersion,omitempty" json:"registryPromptVersion,omitempty"`
}

// McpServerType represents a single runtime MCP server configuration.
// New declarative manifests use the compact Name+Version reference; the legacy
// command/remote/registry fields remain only as an internal workflow shape.
type McpServerType struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version,omitempty" json:"version,omitempty"`

	Type    string            `yaml:"type,omitempty" json:"type,omitempty"`
	Image   string            `yaml:"image,omitempty" json:"image,omitempty"`
	Build   string            `yaml:"build,omitempty" json:"build,omitempty"`
	Command string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env     []string          `yaml:"env,omitempty" json:"env,omitempty"`
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`

	RegistryURL                string `yaml:"registryURL,omitempty" json:"registryURL,omitempty"`
	RegistryServerName         string `yaml:"registryServerName,omitempty" json:"registryServerName,omitempty"`
	RegistryServerVersion      string `yaml:"registryServerVersion,omitempty" json:"registryServerVersion,omitempty"`
	RegistryServerPreferRemote bool   `yaml:"registryServerPreferRemote,omitempty" json:"registryServerPreferRemote,omitempty"`
}

func (m *McpServerType) IsLegacyFormat() bool {
	return m.Type != "" || m.RegistryServerName != ""
}

func (m *McpServerType) ToRegistryRef() *RegistryRef {
	if m.IsLegacyFormat() {
		return nil
	}
	return &RegistryRef{Name: m.Name, Version: m.Version}
}

func (am *AgentManifest) ExtractMCPServerRefs() []RegistryRef {
	var refs []RegistryRef
	for _, server := range am.McpServers {
		if ref := server.ToRegistryRef(); ref != nil {
			refs = append(refs, *ref)
		}
	}
	return refs
}

// FromV1Alpha1Agent projects a registry Agent resource onto the workflow-local
// manifest shape used by the agent runtime.
func FromV1Alpha1Agent(agent *v1alpha1.Agent) AgentManifest {
	if agent == nil {
		return AgentManifest{}
	}

	manifest := AgentManifest{
		Name:              agent.Metadata.Name,
		Image:             agent.Spec.Image,
		Language:          agent.Spec.Language,
		Framework:         agent.Spec.Framework,
		ModelProvider:     agent.Spec.ModelProvider,
		ModelName:         agent.Spec.ModelName,
		Description:       agent.Spec.Description,
		Version:           agent.Metadata.Version,
		TelemetryEndpoint: agent.Spec.TelemetryEndpoint,
	}

	for _, ref := range agent.Spec.MCPServers {
		manifest.McpServers = append(manifest.McpServers, McpServerType{
			Name:                       localRefName(ref.Name),
			Version:                    ref.Version,
			Type:                       "registry",
			RegistryServerName:         ref.Name,
			RegistryServerVersion:      ref.Version,
			RegistryServerPreferRemote: false,
		})
	}
	for _, ref := range agent.Spec.Skills {
		manifest.Skills = append(manifest.Skills, SkillRef{
			Name:                 localRefName(ref.Name),
			RegistrySkillName:    ref.Name,
			RegistrySkillVersion: ref.Version,
		})
	}
	for _, ref := range agent.Spec.Prompts {
		manifest.Prompts = append(manifest.Prompts, PromptRef{
			Name:                  localRefName(ref.Name),
			RegistryPromptName:    ref.Name,
			RegistryPromptVersion: ref.Version,
		})
	}

	return manifest
}

func localRefName(name string) string {
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}
