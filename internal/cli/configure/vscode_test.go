package configure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVSCodeConfigurer_GetConfigPath(t *testing.T) {
	configurer := &VSCodeConfigurer{}
	path, err := configurer.GetConfigPath()

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	expected := ".vscode/mcp.json"
	if path != expected {
		t.Errorf("Expected path %s, got %s", expected, path)
	}
}

func TestVSCodeConfigurer_GetClientName(t *testing.T) {
	configurer := &VSCodeConfigurer{}
	name := configurer.GetClientName()

	expected := "Visual Studio Code"
	if name != expected {
		t.Errorf("Expected name %s, got %s", expected, name)
	}
}

func TestVSCodeConfigurer_CreateConfig(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, ".vscode", "mcp.json")

	configurer := &VSCodeConfigurer{}
	url := "http://localhost:8080/mcp"

	// Test creating a new config
	config, err := configurer.CreateConfig(url, configPath)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify the config structure
	mcpConfig, ok := config.(mcpConfig)
	if !ok {
		t.Fatal("Expected config to be of type mcpConfig")
	}

	if len(mcpConfig.Servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(mcpConfig.Servers))
	}

	arctlServer, exists := mcpConfig.Servers["arctl"]
	if !exists {
		t.Fatal("Expected arctl server to exist")
	}

	if arctlServer.Type != "http" {
		t.Errorf("Expected type 'http', got %s", arctlServer.Type)
	}

	if arctlServer.URL != url {
		t.Errorf("Expected URL %s, got %s", url, arctlServer.URL)
	}
}

func TestVSCodeConfigurer_CreateConfig_MergesExisting(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.json")

	// Create an existing config with another server
	existingConfig := mcpConfig{
		Servers: map[string]mcpServerConfig{
			"existing-server": {
				Type: "http",
				URL:  "http://existing.com",
			},
		},
	}

	data, err := json.MarshalIndent(existingConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal existing config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("Failed to write existing config: %v", err)
	}

	// Now create config with arctl
	configurer := &VSCodeConfigurer{}
	url := "http://localhost:8080/mcp"

	config, err := configurer.CreateConfig(url, configPath)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify both servers exist
	mcpConfig, ok := config.(mcpConfig)
	if !ok {
		t.Fatal("Expected config to be of type mcpConfig")
	}

	if len(mcpConfig.Servers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(mcpConfig.Servers))
	}

	// Check existing server is preserved
	existingServer, exists := mcpConfig.Servers["existing-server"]
	if !exists {
		t.Fatal("Expected existing-server to be preserved")
	}

	if existingServer.URL != "http://existing.com" {
		t.Errorf("Existing server URL changed unexpectedly")
	}

	// Check arctl server was added
	arctlServer, exists := mcpConfig.Servers["arctl"]
	if !exists {
		t.Fatal("Expected arctl server to exist")
	}

	if arctlServer.URL != url {
		t.Errorf("Expected arctl URL %s, got %s", url, arctlServer.URL)
	}
}
