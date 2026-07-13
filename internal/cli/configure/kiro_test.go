package configure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestKiroConfigurer_GetConfigPath(t *testing.T) {
	configurer := &KiroConfigurer{}
	path, err := configurer.GetConfigPath()

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	expected := ".kiro/settings/mcp.json"
	if path != expected {
		t.Errorf("Expected path %s, got %s", expected, path)
	}
}

func TestKiroConfigurer_GetClientName(t *testing.T) {
	configurer := &KiroConfigurer{}
	name := configurer.GetClientName()

	expected := "Kiro agentic IDE"
	if name != expected {
		t.Errorf("Expected name %s, got %s", expected, name)
	}
}

func TestKiroConfigurer_CreateConfig(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, ".kiro", "settings", "mcp.json")

	configurer := &KiroConfigurer{}
	url := "http://localhost:8080/mcp"

	// Test creating a new config
	config, err := configurer.CreateConfig(url, configPath)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify the config structure
	kiroConfig, ok := config.(kiroConfig)
	if !ok {
		t.Fatal("Expected config to be of type kiroConfig")
	}

	if len(kiroConfig.MCPServers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(kiroConfig.MCPServers))
	}

	arctlServer, exists := kiroConfig.MCPServers["ARCTL"]
	if !exists {
		t.Fatal("Expected ARCTL server to exist")
	}

	if arctlServer.URL != url {
		t.Errorf("Expected URL %s, got %s", url, arctlServer.URL)
	}
}

func TestKiroConfigurer_CreateConfig_MergesExisting(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.json")

	// Create an existing config with another server
	existingConfig := kiroConfig{
		MCPServers: map[string]kiroServerConfig{
			"existing-server": {
				URL: "http://existing.com",
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
	configurer := &KiroConfigurer{}
	url := "http://localhost:8080/mcp"

	config, err := configurer.CreateConfig(url, configPath)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify both servers exist
	kiroConfig, ok := config.(kiroConfig)
	if !ok {
		t.Fatal("Expected config to be of type kiroConfig")
	}

	if len(kiroConfig.MCPServers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(kiroConfig.MCPServers))
	}

	// Check existing server is preserved
	existingServer, exists := kiroConfig.MCPServers["existing-server"]
	if !exists {
		t.Fatal("Expected existing-server to be preserved")
	}

	if existingServer.URL != "http://existing.com" {
		t.Errorf("Existing server URL changed unexpectedly")
	}

	// Check arctl server was added
	arctlServer, exists := kiroConfig.MCPServers["ARCTL"]
	if !exists {
		t.Fatal("Expected ARCTL server to exist")
	}

	if arctlServer.URL != url {
		t.Errorf("Expected arctl URL %s, got %s", url, arctlServer.URL)
	}
}
