package configure

import "fmt"

// ClaudeCodeConfigurer handles Claude Code MCP configuration
type ClaudeCodeConfigurer struct{}

// claudeServerConfig is the arctl server entry written into .mcp.json. Other
// servers' entries are preserved verbatim by mergeServerEntry to avoid breaking
// a user's file.
type claudeServerConfig struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (c *ClaudeCodeConfigurer) GetConfigPath() (string, error) {
	return ".mcp.json", nil
}

func (c *ClaudeCodeConfigurer) CreateConfig(opts CreateOptions, configPath string) (any, error) {
	entry := claudeServerConfig{
		Type: "http",
		URL:  opts.URL,
	}
	if opts.TokenEnv != "" {
		// Claude Code expands ${VAR} in header values at connect time, keeping the token out of the checked-in file.
		entry.Headers = map[string]string{
			"Authorization": fmt.Sprintf("Bearer ${%s}", opts.TokenEnv),
		}
	}
	return mergeServerEntry(configPath, "mcpServers", entry)
}

func (c *ClaudeCodeConfigurer) GetClientName() string {
	return "Claude Code"
}
