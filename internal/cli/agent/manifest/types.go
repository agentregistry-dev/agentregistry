package manifest

import "strings"

// AgentManifest is the in-memory runtime projection of a v1alpha1.Agent
// envelope, produced by Resolve. It is never serialized back to disk —
// the on-disk shape is always the v1alpha1.Agent envelope, decoded
// upstream by project.LoadAgent.
//
// Most fields mirror v1alpha1.AgentSpec directly. McpServers carries
// terminal-form runtime entries (Type="command" or "remote"); Skills /
// Prompts carry registry-side identity for late resolution at materialize
// time (image extract, prompt content fetch).
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
// depends on. Resolved against the registry at materialize time.
type PromptRef struct {
	Name                  string
	RegistryURL           string
	RegistryPromptName    string
	RegistryPromptVersion string
}

// McpServerType is one terminal-form MCP server entry on the runtime
// manifest. Resolve always populates entries with Type="command" or
// Type="remote"; no intermediate "registry" state is ever exposed.
//
//   - Type="command": runnable container (Image/Build/Command/Args/Env
//     populated). Build is "registry/<name>" for npm/PyPI packages that
//     must be built into a Docker image at run time; empty for OCI images
//     that are pulled directly.
//   - Type="remote":  remote MCP endpoint (URL/Headers populated).
//
// Fields are mutually exclusive by Type. This struct is never user-serialized.
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
}

func localRefName(name string) string {
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}
