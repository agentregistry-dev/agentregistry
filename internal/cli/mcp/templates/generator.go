package templates

import "github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"

// ProjectConfig contains all the information needed to generate a project
type ProjectConfig struct {
	ProjectName  string
	Framework    string
	Version      string
	Description  string
	Author       string
	Email        string
	Tools        map[string]manifest.ToolConfig
	Secrets      manifest.SecretsConfig
	Directory    string
	NoGit        bool
	Verbose      bool
	GoModuleName string
}

type ToolConfig struct {
	ToolName    string
	Description string
}
