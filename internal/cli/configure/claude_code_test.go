package configure

import "testing"

func TestClaudeCodeConfigurer_GetConfigPath(t *testing.T) {
	configurer := &ClaudeCodeConfigurer{}
	path, err := configurer.GetConfigPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != ".mcp.json" {
		t.Errorf("got path %q, want %q", path, ".mcp.json")
	}
}

func TestClaudeCodeConfigurer_GetClientName(t *testing.T) {
	configurer := &ClaudeCodeConfigurer{}
	if name := configurer.GetClientName(); name != "Claude Code" {
		t.Errorf("got name %q, want %q", name, "Claude Code")
	}
}
