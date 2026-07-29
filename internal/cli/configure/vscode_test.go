package configure

import "testing"

func TestVSCodeConfigurer_GetConfigPath(t *testing.T) {
	configurer := &VSCodeConfigurer{}
	path, err := configurer.GetConfigPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != ".vscode/mcp.json" {
		t.Errorf("got path %q, want %q", path, ".vscode/mcp.json")
	}
}

func TestVSCodeConfigurer_GetClientName(t *testing.T) {
	configurer := &VSCodeConfigurer{}
	if name := configurer.GetClientName(); name != "Visual Studio Code" {
		t.Errorf("got name %q, want %q", name, "Visual Studio Code")
	}
}
