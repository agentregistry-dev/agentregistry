package configure

import (
	"encoding/json"
	"fmt"
	"os"
)

// ClaudeCodeConfigurer handles Claude Code MCP configuration
type ClaudeCodeConfigurer struct{}

// claudeServerConfig represents a Claude MCP server configuration (supports both stdio and HTTP)
type claudeServerConfig struct {
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// claudeConfig represents the Claude MCP configuration file structure
type claudeConfig struct {
	MCPServers map[string]claudeServerConfig `json:"mcpServers"`
}

func (c *ClaudeCodeConfigurer) GetConfigPath() (string, error) {
	return ".mcp.json", nil
}

func (c *ClaudeCodeConfigurer) CreateConfig(url string, configPath string) (any, error) {
	config := claudeConfig{
		MCPServers: make(map[string]claudeServerConfig),
	}

	// Read existing config if it exists
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return config, fmt.Errorf("failed to parse existing config: %w", err)
		}
	}

	// Add or update the arctl HTTP server
	config.MCPServers["arctl"] = claudeServerConfig{
		Type: "http",
		URL:  url,
	}

	return config, nil
}

func (c *ClaudeCodeConfigurer) GetClientName() string {
	return "Claude Code Editor"
}
