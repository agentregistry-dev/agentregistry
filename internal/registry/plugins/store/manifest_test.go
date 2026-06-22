package store

import (
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestBuildInventory(t *testing.T) {
	b := &CanonicalBundle{Files: map[string][]byte{
		"skills/deploy/SKILL.md": []byte("---\nname: deploy\ndescription: Deploys things\n---\nbody\n"),
		"SKILL.md":               []byte("---\nname: root-skill\n---\n"),
		"agents/reviewer.md":     []byte("you are a reviewer"),
		"commands/status.md":     []byte("status"),
		"bin/mytool":             []byte("#!/bin/sh"),
		".mcp.json":              []byte(`{"mcpServers":{"db":{"command":"x"},"api":{"url":"y"}}}`),
		"hooks/hooks.json":       []byte(`{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command"}]}],"PostToolUse":[{"hooks":[{"type":"command"},{"type":"http"}]}]}}`),
	}}

	m := BuildInventory(b)

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

func TestBuildInventoryBestEffortOnMalformed(t *testing.T) {
	b := &CanonicalBundle{Files: map[string][]byte{
		".mcp.json":        []byte("not json"),
		"hooks/hooks.json": []byte("{bad"),
	}}
	m := BuildInventory(b) // must not panic
	if len(m.MCPServers) != 0 || len(m.Hooks) != 0 {
		t.Fatalf("expected empty index for malformed files, got %+v", m)
	}
}

func TestParseManifest(t *testing.T) {
	// Absent manifest -> nil, no error.
	if m, err := ParseManifest(&CanonicalBundle{Files: map[string][]byte{"SKILL.md": []byte("x")}}); err != nil || m != nil {
		t.Fatalf("absent manifest: got (%v, %v), want (nil, nil)", m, err)
	}
	// Real plugin.json -> typed manifest.
	b := &CanonicalBundle{Files: map[string][]byte{
		ManifestPath: []byte(`{"name":"company-deploy","version":"1.2.0","author":{"name":"Maya"},"keywords":["deploy"]}`),
	}}
	m, err := ParseManifest(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m == nil || m.Name != "company-deploy" || m.Version != "1.2.0" || m.Author == nil || m.Author.Name != "Maya" {
		t.Fatalf("typed manifest not parsed: %+v", m)
	}
	// Malformed manifest -> error (fail closed).
	bad := &CanonicalBundle{Files: map[string][]byte{ManifestPath: []byte("{not json")}}
	if _, err := ParseManifest(bad); err == nil {
		t.Fatal("expected error for malformed manifest")
	}
}
