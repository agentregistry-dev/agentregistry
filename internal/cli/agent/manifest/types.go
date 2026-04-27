package manifest

import (
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// AgentManifest is the in-memory runtime projection of a v1alpha1.Agent
// envelope. It is built from the on-disk envelope (see project.LoadManifest)
// and then handed to the agent CLI's docker-compose / template renderers.
//
// The on-disk shape is always the v1alpha1.Agent envelope; this struct is
// never serialized back to disk. Most fields mirror v1alpha1.AgentSpec
// directly, but the MCPServer / Skill / Prompt slices carry additional
// runtime-resolution state that the registry shape doesn't:
//   - McpServers initially carries Type="registry" entries projected from
//     v1alpha1.AgentSpec.MCPServers ResourceRefs; the resolver later
//     translates each into Type="command" or Type="remote" with the
//     runnable bits filled in (Image/Build/Command/URL/Headers/...).
type AgentManifest struct {
	Name              string
	Image             string
	Language          string
	Framework         string
	ModelProvider     string
	ModelName         string
	Description       string
	Version           string
	TelemetryEndpoint string
	McpServers        []McpServerType
	Skills            []SkillRef
	Prompts           []PromptRef
}

// SkillRef is the runtime-side reference for a v1alpha1.Skill the agent
// depends on. Either Image (a pre-built OCI image — the "local image"
// shortcut) or RegistrySkillName (registry lookup) must be set; the
// resolver fills in the rest at run time.
type SkillRef struct {
	Name                 string
	Image                string
	RegistryURL          string
	RegistrySkillName    string
	RegistrySkillVersion string
}

// PromptRef is the runtime-side reference for a v1alpha1.Prompt the agent
// depends on. Resolved against the registry at run time.
type PromptRef struct {
	Name                  string
	RegistryURL           string
	RegistryPromptName    string
	RegistryPromptVersion string
}

// McpServerType represents one MCP server entry on the runtime manifest.
// The Type field is the runtime discriminator:
//   - "registry" — initial state right after FromV1Alpha1Agent; the resolver
//     fetches v1alpha1.MCPServer for (RegistryServerName, RegistryServerVersion)
//     and converts the entry into one of the two terminal forms below.
//   - "command" — runnable container (Image/Build/Command/Args/Env populated).
//   - "remote"  — remote MCP endpoint (URL/Headers populated).
//
// Templates render the terminal forms; the resolver always replaces
// "registry" entries before render. Fields are mutually exclusive by Type
// and not enforced via tag (this struct is never user-serialized).
type McpServerType struct {
	Name    string
	Version string

	Type    string
	Image   string
	Build   string
	Command string
	Args    []string
	Env     []string
	URL     string
	Headers map[string]string

	RegistryURL                string
	RegistryServerName         string
	RegistryServerVersion      string
	RegistryServerPreferRemote bool
}

// FromV1Alpha1Agent projects a v1alpha1.Agent envelope onto the runtime
// AgentManifest. MCPServer / Skill / Prompt ResourceRefs are turned into
// the registry-typed runtime entries that the resolver later expands into
// fully-runnable form.
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
