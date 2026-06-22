package v1alpha1

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

// realPluginJSON exercises the union forms, author/dependencies/userConfig, the
// tri-state defaultEnabled:false, and Codex-only superset keys (interface/apps).
const realPluginJSON = `{
  "$schema": "https://json.schemastore.org/claude-code-plugin-manifest.json",
  "name": "company-deploy",
  "version": "1.2.0",
  "description": "Deploy workflow",
  "author": {"name": "Maya", "email": "maya@example.com"},
  "keywords": ["deploy", "ci"],
  "dependencies": ["audit-logger", {"name": "secrets-vault", "marketplace": "acme", "version": "~2.1.0"}],
  "commands": {"deploy": {"description": "Deploy it", "allowedTools": ["Bash"]}},
  "agents": "./custom/agents",
  "skills": ["./skills/a", "./skills/b"],
  "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "./scripts/x.sh"}]}]},
  "mcpServers": {"db": {"command": "npx", "args": ["@db/mcp"]}},
  "userConfig": {"token": {"type": "string", "title": "API token", "description": "Your token", "sensitive": true}},
  "defaultEnabled": false,
  "interface": {"displayName": "Company Deploy"},
  "apps": "./.app.json"
}`

func TestPluginManifestRoundTripLossless(t *testing.T) {
	var m PluginManifest
	if err := json.Unmarshal([]byte(realPluginJSON), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Typed access into the union/structured fields.
	if m.Name != "company-deploy" || m.Author == nil || m.Author.Name != "Maya" {
		t.Fatalf("scalar/author wrong: %+v", m)
	}
	if len(m.Dependencies) != 2 || m.Dependencies[0].Ref != "audit-logger" || m.Dependencies[1].Name != "secrets-vault" {
		t.Fatalf("dependencies union wrong: %+v", m.Dependencies)
	}
	if m.Agents == nil || m.Agents.WasArray || len(m.Agents.Values) != 1 {
		t.Fatalf("agents scalar form wrong: %+v", m.Agents)
	}
	if m.Skills == nil || !m.Skills.WasArray || len(m.Skills.Values) != 2 {
		t.Fatalf("skills array form wrong: %+v", m.Skills)
	}
	if m.Hooks == nil || m.Hooks.Events["PreToolUse"][0].Hooks[0].Command != "./scripts/x.sh" {
		t.Fatalf("hooks object form wrong: %+v", m.Hooks)
	}
	if m.MCPServers == nil || m.MCPServers.Servers["db"].Command != "npx" {
		t.Fatalf("mcpServers object form wrong: %+v", m.MCPServers)
	}
	if uc, ok := m.UserConfig["token"]; !ok || uc.Sensitive == nil || !*uc.Sensitive {
		t.Fatalf("userConfig wrong: %+v", m.UserConfig)
	}
	if m.DefaultEnabled == nil || *m.DefaultEnabled != false {
		t.Fatalf("defaultEnabled:false must round-trip, got %+v", m.DefaultEnabled)
	}
	if _, ok := m.Extras["interface"]; !ok {
		t.Fatalf("Codex 'interface' not captured in Extras: %+v", m.Extras)
	}
	if _, ok := m.Extras["apps"]; !ok {
		t.Fatalf("Codex 'apps' not captured in Extras")
	}

	// Re-marshal and assert semantic equality + no stray nulls.
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(out, []byte(":null")) {
		t.Fatalf("marshaled manifest contains a stray null: %s", out)
	}
	var want, got map[string]any
	_ = json.Unmarshal([]byte(realPluginJSON), &want)
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("round-trip not lossless:\n want %v\n got  %v", want, got)
	}
}

func TestPluginManifestSparseNoNulls(t *testing.T) {
	// A minimal manifest must not emit null/empty component keys.
	out, err := json.Marshal(PluginManifest{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"name":"x"}` {
		t.Fatalf("sparse manifest should be {\"name\":\"x\"}, got %s", out)
	}
}
