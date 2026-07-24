package configure

import "fmt"

// CursorConfigurer handles Cursor MCP configuration
type CursorConfigurer struct{}

// cursorServerConfig is the arctl server entry written into .cursor/mcp.json.
// Other servers' entries are preserved verbatim by mergeServerEntry.
type cursorServerConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (c *CursorConfigurer) GetConfigPath() (string, error) {
	return ".cursor/mcp.json", nil
}

func (c *CursorConfigurer) CreateConfig(opts CreateOptions, configPath string) (any, error) {
	entry := cursorServerConfig{
		URL: opts.URL,
	}
	if opts.TokenEnv != "" {
		// Cursor interpolates ${env:NAME} in header values at connect time,
		// keeping the token out of the checked-in file.
		entry.Headers = map[string]string{
			"Authorization": fmt.Sprintf("Bearer ${env:%s}", opts.TokenEnv),
		}
	}
	return mergeServerEntry(configPath, "mcpServers", entry)
}

func (c *CursorConfigurer) GetClientName() string {
	return "Cursor AI Editor"
}
