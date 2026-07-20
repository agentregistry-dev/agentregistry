package configure

import (
	"encoding/json"
	"fmt"
	"os"
)

// KiroConfigurer handles Kiro MCP configuration
type KiroConfigurer struct{}

// kiroServerConfig represents a Kiro MCP server configuration
type kiroServerConfig struct {
	URL string `json:"url"`
}

// kiroConfig represents the Kiro MCP configuration file structure
type kiroConfig struct {
	MCPServers map[string]kiroServerConfig `json:"mcpServers"`
}

func (c *KiroConfigurer) GetConfigPath() (string, error) {
	return ".kiro/settings/mcp.json", nil
}

func (c *KiroConfigurer) CreateConfig(url string, configPath string) (any, error) {
	config := kiroConfig{
		MCPServers: make(map[string]kiroServerConfig),
	}

	// Read existing config if it exists
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return config, fmt.Errorf("failed to parse existing config: %w", err)
		}
	}

	// Add or update the ARCTL server
	config.MCPServers["ARCTL"] = kiroServerConfig{
		URL: url,
	}

	return config, nil
}

func (c *KiroConfigurer) GetClientName() string {
	return "Kiro agentic IDE"
}
