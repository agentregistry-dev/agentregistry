package configure

import "testing"

func TestCursorConfigurer_GetConfigPath(t *testing.T) {
	configurer := &CursorConfigurer{}
	path, err := configurer.GetConfigPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != ".cursor/mcp.json" {
		t.Errorf("got path %q, want %q", path, ".cursor/mcp.json")
	}
}

func TestCursorConfigurer_GetClientName(t *testing.T) {
	configurer := &CursorConfigurer{}
	if name := configurer.GetClientName(); name != "Cursor AI Editor" {
		t.Errorf("got name %q, want %q", name, "Cursor AI Editor")
	}
}
