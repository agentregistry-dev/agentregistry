package common

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/utils"
)

// MCPConfigTarget describes where to place the resolved MCP config for an agent.
type MCPConfigTarget struct {
	BaseDir   string
	AgentName string
	Version   string // optional; when set, path includes sanitized version
}

// ComputeMCPConfigPath returns the config directory and file path for an agent.
// If BaseDir or AgentName is empty, both returned paths are empty.
func ComputeMCPConfigPath(target *MCPConfigTarget) (configDir string, configPath string) {
	if target.BaseDir == "" || target.AgentName == "" {
		return "", ""
	}

	configDir = filepath.Join(target.BaseDir, target.AgentName)
	if target.Version != "" {
		configDir = filepath.Join(configDir, utils.SanitizeVersion(target.Version))
	}
	configPath = filepath.Join(configDir, "mcp-servers.json")
	return configDir, configPath
}

// RefreshMCPConfig cleans any existing MCP config for the target and optionally writes a new one.
// If servers is empty or nil, it performs cleanup only.
func RefreshMCPConfig(target *MCPConfigTarget, servers []PythonMCPServer, verbose bool) error {
	if target == nil {
		return fmt.Errorf("target is required")
	}

	configDir, configPath := ComputeMCPConfigPath(target)
	if configDir == "" {
		return nil
	}

	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove MCP server config file: %w", err)
	}

	// Attempt to remove the directory if it is empty; ignore errors.
	_ = os.Remove(configDir)

	if len(servers) == 0 {
		return nil
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create agent config directory: %w", err)
	}

	configData, err := json.MarshalIndent(servers, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP server config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("failed to write MCP server config file: %w", err)
	}

	if verbose {
		fmt.Printf("Wrote MCP server config for agent %s version %s to %s\n", target.AgentName, target.Version, configPath)
	}

	return nil
}

// PythonServersFromManifest converts resolved MCP servers in a manifest into Python MCP server structs.
// Registry-type servers are skipped because they are not directly runnable.
func PythonServersFromManifest(manifest *AgentManifest) []PythonMCPServer {
	if manifest == nil || len(manifest.McpServers) == 0 {
		return nil
	}

	var mcpServers []PythonMCPServer
	for _, srv := range manifest.McpServers {
		if srv.Type == "registry" {
			continue
		}

		pythonServer := PythonMCPServer{
			Name: srv.Name,
			Type: srv.Type,
		}

		if srv.Type == "remote" {
			pythonServer.URL = srv.URL
			if len(srv.Headers) > 0 {
				pythonServer.Headers = srv.Headers
			}
		}
		// For command type, the Python code constructs URL as f"http://{server_name}:3000/mcp"
		// so we do not set URL here.

		mcpServers = append(mcpServers, pythonServer)
	}

	return mcpServers
}
