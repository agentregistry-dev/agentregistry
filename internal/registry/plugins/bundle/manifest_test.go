package bundle

import (
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func everyPartBundle() *CanonicalBundle {
	return &CanonicalBundle{Files: map[string][]byte{
		"skills/deploy/SKILL.md": []byte("---\nname: deploy\ndescription: Deploys things\n---\nbody\n"),
		"SKILL.md":               []byte("---\nname: root-skill\n---\n"),
		"agents/reviewer.md":     []byte("you are a reviewer"),
		"commands/status.md":     []byte("status"),
		"bin/mytool":             []byte("#!/bin/sh"),
		".mcp.json":              []byte(`{"mcpServers":{"db":{"command":"x"},"api":{"url":"y"}}}`),
		"hooks/hooks.json":       []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command"}]}],"PostToolUse":[{"hooks":[{"type":"command"},{"type":"http"}]}]}}`),
	}}
}

func TestBuildInventory(t *testing.T) {
	m := BuildInventory(everyPartBundle(), v1alpha1.PluginFormatClaudePlugin)

	wantSkills := []v1alpha1.PluginSkill{
		{Name: "root-skill"},                            // top-level SKILL.md (sorts before "skills/...")
		{Name: "deploy", Description: "Deploys things"}, // skills/deploy/SKILL.md
	}
	if !reflect.DeepEqual(m.Skills, wantSkills) {
		t.Fatalf("skills = %+v, want %+v", m.Skills, wantSkills)
	}
	if !reflect.DeepEqual(m.Agents, []string{"reviewer"}) {
		t.Fatalf("agents = %v", m.Agents)
	}
	if !reflect.DeepEqual(m.Commands, []string{"status"}) {
		t.Fatalf("commands = %v", m.Commands)
	}
	if !reflect.DeepEqual(m.Executables, []string{"mytool"}) {
		t.Fatalf("executables = %v", m.Executables)
	}
	if !reflect.DeepEqual(m.MCPServers, []string{"api", "db"}) {
		t.Fatalf("mcpServers = %v (want sorted [api db])", m.MCPServers)
	}
	wantHooks := []v1alpha1.PluginHook{
		{Event: "PostToolUse", Type: "command"},
		{Event: "PostToolUse", Type: "http"},
		{Event: "PreToolUse", Type: "command"},
	}
	if !reflect.DeepEqual(m.Hooks, wantHooks) {
		t.Fatalf("hooks = %+v, want %+v", m.Hooks, wantHooks)
	}
}

func TestBuildInventorySkipsClaudeOnlyPartsForAgentPlugins(t *testing.T) {
	m := BuildInventory(everyPartBundle(), v1alpha1.PluginFormatAgentPlugins)

	if m.Agents != nil || m.Commands != nil || m.Hooks != nil {
		t.Fatalf("agents = %v, commands = %v, hooks = %v, want none", m.Agents, m.Commands, m.Hooks)
	}
	if len(m.Skills) != 2 || !reflect.DeepEqual(m.Executables, []string{"mytool"}) {
		t.Fatalf("skills = %+v, executables = %v, want both kept", m.Skills, m.Executables)
	}
}

func TestBuildInventoryBestEffortOnMalformed(t *testing.T) {
	b := &CanonicalBundle{Files: map[string][]byte{
		".mcp.json":        []byte("not json"),
		"hooks/hooks.json": []byte("{bad"),
	}}
	m := BuildInventory(b, v1alpha1.PluginFormatClaudePlugin) // must not panic
	if len(m.MCPServers) != 0 || len(m.Hooks) != 0 {
		t.Fatalf("expected empty index for malformed files, got %+v", m)
	}
}

func TestBuildInventoryReadsMCPFileOfFormat(t *testing.T) {
	b := &CanonicalBundle{Files: map[string][]byte{
		".mcp.json": []byte(`{"mcpServers":{"db":{},"api":{}}}`),
		"mcp.json":  []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"search":{},"db":{}}}`),
	}}
	tests := []struct {
		name   string
		format v1alpha1.PluginFormat
		want   []string
	}{
		{"claude-plugin reads .mcp.json", v1alpha1.PluginFormatClaudePlugin, []string{"api", "db"}},
		{"agent-plugins reads mcp.json", v1alpha1.PluginFormatAgentPlugins, []string{"db", "search"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildInventory(b, tt.format).MCPServers; !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("mcpServers = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildInventoryAppliesAgentPluginsMCPFileRules(t *testing.T) {
	const schema = `"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"`
	tests := []struct {
		name    string
		mcpJSON string
		want    []string
	}{
		{"valid file", `{` + schema + `,"mcpServers":{"search":{}}}`, []string{"search"}},
		{"missing $schema", `{"mcpServers":{"search":{}}}`, nil},
		{"other $schema", `{"$schema":"https://example.com/mcp.json","mcpServers":{"search":{}}}`, nil},
		{"extra top-level key", `{` + schema + `,"mcpServers":{"search":{}},"extra":1}`, nil},
		{"missing mcpServers", `{` + schema + `}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &CanonicalBundle{Files: map[string][]byte{"mcp.json": []byte(tt.mcpJSON)}}
			got := BuildInventory(b, v1alpha1.PluginFormatAgentPlugins).MCPServers
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("mcpServers = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseManifest(t *testing.T) {
	// Absent manifest -> nil, no error.
	if m, err := ParseManifest(&CanonicalBundle{Files: map[string][]byte{"SKILL.md": []byte("x")}}, ManifestPath); err != nil || m != nil {
		t.Fatalf("absent manifest: got (%v, %v), want (nil, nil)", m, err)
	}
	// Real plugin.json -> typed manifest.
	b := &CanonicalBundle{Files: map[string][]byte{
		ManifestPath: []byte(`{"name":"company-deploy","version":"1.2.0","author":{"name":"Maya"},"keywords":["deploy"]}`),
	}}
	m, err := ParseManifest(b, ManifestPath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m == nil || m.Name != "company-deploy" || m.Version != "1.2.0" || m.Author == nil || m.Author.Name != "Maya" {
		t.Fatalf("typed manifest not parsed: %+v", m)
	}
	// Root Agent Plugins plugin.json -> same typed manifest, extensions in Extras.
	agent := &CanonicalBundle{Files: map[string][]byte{
		"plugin.json": []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"acme.test","description":"Tools","extensions":{"x":1}}`),
	}}
	m, err = ParseManifest(agent, "plugin.json")
	if err != nil {
		t.Fatalf("parse agent-plugins manifest: %v", err)
	}
	if m == nil || m.Name != "acme.test" || m.Description != "Tools" || string(m.Extras["extensions"]) != `{"x":1}` {
		t.Fatalf("agent-plugins manifest not parsed: %+v", m)
	}
	// Malformed manifest -> error (fail closed).
	bad := &CanonicalBundle{Files: map[string][]byte{ManifestPath: []byte("{not json")}}
	if _, err := ParseManifest(bad, ManifestPath); err == nil {
		t.Fatal("expected error for malformed manifest")
	}
}
