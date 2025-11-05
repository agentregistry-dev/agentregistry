package configure

import (
	"encoding/json"
	"fmt"
	"os"
)

// CursorConfigurer handles Cursor MCP configuration
type CursorConfigurer struct{}

// cursorServerConfig represents a Cursor MCP server configuration
type cursorServerConfig struct {
	URL string `json:"url"`
}

// cursorConfig represents the Cursor MCP configuration file structure
type cursorConfig struct {
	MCPServers map[string]cursorServerConfig `json:"mcpServers"`
}

func (c *CursorConfigurer) GetConfigPath() (string, error) {
	return ".cursor/mcp.json", nil
}

func (c *CursorConfigurer) CreateConfig(url string, configPath string) (interface{}, error) {
	config := cursorConfig{
		MCPServers: make(map[string]cursorServerConfig),
	}

	// Read existing config if it exists
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return config, fmt.Errorf("failed to parse existing config: %w", err)
		}
	}

	// Add or update the ARCTL server
	config.MCPServers["ARCTL"] = cursorServerConfig{
		URL: url,
	}

	return config, nil
}

func (c *CursorConfigurer) GetClientName() string {
	return "Cursor AI Editor"
}
