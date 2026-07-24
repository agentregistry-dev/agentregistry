package configure

import "fmt"

// KiroConfigurer handles Kiro MCP configuration
type KiroConfigurer struct{}

// kiroServerConfig is the arctl server entry written into .kiro/settings/mcp.json.
// Other servers' entries are preserved verbatim by mergeServerEntry.
type kiroServerConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (c *KiroConfigurer) GetConfigPath() (string, error) {
	return ".kiro/settings/mcp.json", nil
}

func (c *KiroConfigurer) CreateConfig(opts CreateOptions, configPath string) (any, error) {
	entry := kiroServerConfig{
		URL: opts.URL,
	}
	if opts.TokenEnv != "" {
		// Kiro documents plain ${VAR} expansion in header values, but based on this issue
		// it is sent literally instead of expanded: https://github.com/kirodotdev/Kiro/issues/5060.
		// The undocumented ${env:VAR} form is reported working in that issue's comments, so it is emitted here.
		// Revisit if Kiro fixes #5060 in favor of the documented syntax.
		entry.Headers = map[string]string{
			"Authorization": fmt.Sprintf("Bearer ${env:%s}", opts.TokenEnv),
		}
	}
	return mergeServerEntry(configPath, "mcpServers", entry)
}

func (c *KiroConfigurer) GetClientName() string {
	return "Kiro agentic IDE"
}
