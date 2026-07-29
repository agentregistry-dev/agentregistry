package configure

import "testing"

func TestKiroConfigurer_GetConfigPath(t *testing.T) {
	configurer := &KiroConfigurer{}
	path, err := configurer.GetConfigPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != ".kiro/settings/mcp.json" {
		t.Errorf("got path %q, want %q", path, ".kiro/settings/mcp.json")
	}
}

func TestKiroConfigurer_GetClientName(t *testing.T) {
	configurer := &KiroConfigurer{}
	if name := configurer.GetClientName(); name != "Kiro agentic IDE" {
		t.Errorf("got name %q, want %q", name, "Kiro agentic IDE")
	}
}
