package translate

import (
	"encoding/json"
	"fmt"
)

func init() { Register(claudeCodeAdapter{}) }

// claudeCodeAdapter maps the canonical bundle to/from the Claude Code plugin
// layout. The canonical form is Claude-Code-shaped, so every component is a
// default-pass identity; only .claude-plugin/plugin.json is generated
// (ToHarness) or consumed (FromHarness). Lossless both ways — supporting files,
// AGENTS.md, and Claude-only extras (themes/, output-styles/) all pass through.
type claudeCodeAdapter struct{}

func (claudeCodeAdapter) Harness() Harness     { return HarnessClaudeCode }
func (claudeCodeAdapter) ManifestPath() string { return ".claude-plugin/plugin.json" }

func (claudeCodeAdapter) MapToHarness(string) PathMapping   { return PathMapping{} } // identity / default-pass
func (claudeCodeAdapter) MapFromHarness(string) PathMapping { return PathMapping{} } // identity / default-pass

func (claudeCodeAdapter) GenerateManifest(meta PluginMeta) ([]byte, error) {
	if meta.Name == "" {
		return nil, fmt.Errorf("claude-code manifest requires a name")
	}
	m := map[string]any{}
	// Preserve harness-specific extras, then overlay canonical-owned fields.
	for k, v := range meta.Extras[HarnessClaudeCode] {
		m[k] = v
	}
	m["name"] = meta.Name
	if meta.Title != "" {
		m["displayName"] = meta.Title
	}
	if meta.Version != "" {
		m["version"] = meta.Version
	}
	if meta.Description != "" {
		m["description"] = meta.Description
	}
	// encoding/json sorts map keys, so output is deterministic.
	return json.MarshalIndent(m, "", "  ")
}

func (claudeCodeAdapter) ParseManifest(b []byte) (PluginMeta, error) {
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return PluginMeta{}, err
	}
	meta := PluginMeta{}
	if v, ok := raw["name"].(string); ok {
		meta.Name = v
	}
	if v, ok := raw["displayName"].(string); ok {
		meta.Title = v
	}
	if v, ok := raw["version"].(string); ok {
		meta.Version = v
	}
	if v, ok := raw["description"].(string); ok {
		meta.Description = v
	}
	for _, k := range []string{"name", "displayName", "version", "description"} {
		delete(raw, k)
	}
	if len(raw) > 0 {
		meta.Extras = map[Harness]map[string]any{HarnessClaudeCode: raw}
	}
	return meta, nil
}
