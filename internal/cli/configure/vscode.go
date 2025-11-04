package configure

import (
	"encoding/json"
	"fmt"
	"os"
)

// VSCodeConfigurer handles VS Code MCP configuration
type VSCodeConfigurer struct{}

// mcpServerConfig represents a VS Code MCP server configuration
type mcpServerConfig struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// mcpConfig represents the VS Code MCP configuration file structure
type mcpConfig struct {
	Servers map[string]mcpServerConfig `json:"servers"`
}

func (v *VSCodeConfigurer) GetConfigPath() (string, error) {
	return ".vscode/mcp.json", nil
}

func (v *VSCodeConfigurer) CreateConfig(url string, configPath string) (interface{}, error) {
	config := mcpConfig{
		Servers: make(map[string]mcpServerConfig),
	}

	// Read existing config if it exists
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return config, fmt.Errorf("failed to parse existing config: %w", err)
		}
	}

	// Add or update the arctl server
	config.Servers["arctl"] = mcpServerConfig{
		Type: "http",
		URL:  url,
	}

	return config, nil
}

func (v *VSCodeConfigurer) GetClientName() string {
	return "Visual Studio Code"
}
