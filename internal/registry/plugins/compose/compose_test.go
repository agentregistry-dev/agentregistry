package compose

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func base(files map[string][]byte) *bundle.CanonicalBundle {
	return &bundle.CanonicalBundle{Files: files}
}

func TestCompose_PureComposition_GeneratesManifest(t *testing.T) {
	out, report, err := Compose(Inputs{
		PluginName:  "incident-response",
		Description: "IR toolkit",
		Skills:      []Skill{{Name: "log-triage", Files: map[string][]byte{"SKILL.md": []byte("# triage")}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(out.Files["skills/log-triage/SKILL.md"]); got != "# triage" {
		t.Errorf("skill not placed, got %q", got)
	}
	var m struct{ Name, Description string }
	if err := json.Unmarshal(out.Files[".claude-plugin/plugin.json"], &m); err != nil {
		t.Fatalf("generated manifest not valid JSON: %v", err)
	}
	if m.Name != "incident-response" || m.Description != "IR toolkit" {
		t.Errorf("generated manifest = %+v", m)
	}
	if len(report.Replaced) != 0 {
		t.Errorf("nothing should be replaced, got %+v", report.Replaced)
	}
	if len(report.Placed) != 1 || report.Placed[0].Dest != "skills/log-triage/" {
		t.Errorf("placement = %+v", report.Placed)
	}
}

func TestCompose_BaseManifestPassesThrough(t *testing.T) {
	manifest := []byte(`{"name":"base-plugin","hooks":"./hooks/hooks.json"}`)
	out, _, err := Compose(Inputs{
		Base:       base(map[string][]byte{".claude-plugin/plugin.json": manifest}),
		PluginName: "ignored-for-manifest",
		Skills:     []Skill{{Name: "s", Files: map[string][]byte{"SKILL.md": []byte("x")}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(out.Files[".claude-plugin/plugin.json"], manifest) {
		t.Errorf("base manifest was modified: %s", out.Files[".claude-plugin/plugin.json"])
	}
}

func TestCompose_SkillOverlayWins_WholeDirectory(t *testing.T) {
	b := base(map[string][]byte{
		"skills/log-triage/SKILL.md":  []byte("old"),
		"skills/log-triage/helper.py": []byte("old helper"),
		"skills/other/SKILL.md":       []byte("untouched"),
	})
	out, report, err := Compose(Inputs{
		Base:   b,
		Skills: []Skill{{Name: "log-triage", Files: map[string][]byte{"SKILL.md": []byte("new")}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(out.Files["skills/log-triage/SKILL.md"]); got != "new" {
		t.Errorf("overlay did not win, got %q", got)
	}
	// Whole-directory replace: the base's helper.py must be gone, not interleaved.
	if _, ok := out.Files["skills/log-triage/helper.py"]; ok {
		t.Error("base helper.py survived — trees were interleaved, not replaced")
	}
	if got := string(out.Files["skills/other/SKILL.md"]); got != "untouched" {
		t.Errorf("unrelated skill touched: %q", got)
	}
	if len(report.Replaced) != 1 || report.Replaced[0].Files != 2 || report.Replaced[0].Kind != v1alpha1.KindSkill {
		t.Errorf("replacement not recorded correctly: %+v", report.Replaced)
	}
	// Base must not be mutated.
	if _, ok := b.Files["skills/log-triage/helper.py"]; !ok {
		t.Error("Compose mutated the base bundle")
	}
}

func TestCompose_CommandOverlayWins(t *testing.T) {
	out, report, err := Compose(Inputs{
		Base:     base(map[string][]byte{"commands/deploy.md": []byte("old")}),
		Commands: []Command{{Name: "deploy", Body: "new body"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(out.Files["commands/deploy.md"]); got != "new body" {
		t.Errorf("command overlay did not win: %q", got)
	}
	if len(report.Replaced) != 1 || report.Replaced[0].Dest != "commands/deploy.md" {
		t.Errorf("replacement not recorded: %+v", report.Replaced)
	}
}

func TestCompose_MCPMerge_PreservesAndReplaces(t *testing.T) {
	baseDoc := `{"mcpServers":{"existing":{"type":"http","url":"https://a"},"shadowed":{"type":"http","url":"https://old"}},"unrelatedTop":42}`
	out, report, err := Compose(Inputs{
		Base: base(map[string][]byte{".mcp.json": []byte(baseDoc)}),
		MCPServers: []MCPServer{
			{Name: "shadowed", Entry: json.RawMessage(`{"type":"http","url":"https://new"}`)},
			{Name: "added", Entry: json.RawMessage(`{"type":"sse","url":"https://b"}`)},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc struct {
		MCPServers map[string]struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"mcpServers"`
		UnrelatedTop int `json:"unrelatedTop"`
	}
	if err := json.Unmarshal(out.Files[".mcp.json"], &doc); err != nil {
		t.Fatalf("merged .mcp.json invalid: %v", err)
	}
	if doc.UnrelatedTop != 42 {
		t.Error("unrelated top-level field dropped")
	}
	if doc.MCPServers["existing"].URL != "https://a" {
		t.Error("unrelated server entry dropped")
	}
	if doc.MCPServers["shadowed"].URL != "https://new" {
		t.Error("overlay entry did not replace same-named base entry")
	}
	if doc.MCPServers["added"].Type != "sse" {
		t.Error("new entry missing")
	}
	if len(report.Replaced) != 1 || report.Replaced[0].Name != "shadowed" {
		t.Errorf("replacement not recorded: %+v", report.Replaced)
	}
}

func TestCompose_MCPMerge_MalformedBaseIsInvalidBundle(t *testing.T) {
	_, _, err := Compose(Inputs{
		Base:       base(map[string][]byte{".mcp.json": []byte("not json")}),
		MCPServers: []MCPServer{{Name: "x", Entry: json.RawMessage(`{}`)}},
	})
	if err == nil || !strings.Contains(err.Error(), ".mcp.json") {
		t.Fatalf("expected malformed .mcp.json error, got %v", err)
	}
}

func TestCompose_InstructionsAppendAndCreate(t *testing.T) {
	// Append to existing.
	out, _, err := Compose(Inputs{
		Base:         base(map[string][]byte{"AGENTS.md": []byte("base rules\n")}),
		Instructions: &Instructions{Name: "extra", Body: "overlay rules"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "base rules\n\n---\n\noverlay rules\n"
	if got := string(out.Files["AGENTS.md"]); got != want {
		t.Errorf("append: got %q want %q", got, want)
	}
	// Create when absent (pure composition also generates a manifest).
	out, _, err = Compose(Inputs{
		PluginName:   "p",
		Instructions: &Instructions{Name: "only", Body: "solo rules"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(out.Files["AGENTS.md"]); got != "solo rules\n" {
		t.Errorf("create: got %q", got)
	}
}

func TestCompose_SkillPathTraversalRejected(t *testing.T) {
	_, _, err := Compose(Inputs{
		PluginName: "p",
		Skills:     []Skill{{Name: "evil", Files: map[string][]byte{"../escape": []byte("x")}}},
	})
	if err == nil || !strings.Contains(err.Error(), "traversal") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
}

func TestCompose_CeilingEnforced(t *testing.T) {
	big := make(map[string][]byte, bundle.MaxBundleFiles+1)
	for i := 0; i <= bundle.MaxBundleFiles; i++ {
		big[fmt.Sprintf("f/%d", i)] = []byte("b")
	}
	_, _, err := Compose(Inputs{PluginName: "p", Skills: []Skill{{Name: "huge", Files: big}}})
	if err == nil || !strings.Contains(err.Error(), "too many files") {
		t.Fatalf("expected file-count ceiling, got %v", err)
	}
}

func TestCompose_Deterministic(t *testing.T) {
	in := Inputs{
		Base: base(map[string][]byte{
			".mcp.json":     []byte(`{"mcpServers":{"z":{"url":"https://z"},"a":{"url":"https://a"}}}`),
			"AGENTS.md":     []byte("rules"),
			"skills/s/x.md": []byte("keep"),
		}),
		Skills:       []Skill{{Name: "n", Files: map[string][]byte{"SKILL.md": []byte("s"), "a/b.txt": []byte("t")}}},
		MCPServers:   []MCPServer{{Name: "m", Entry: json.RawMessage(`{"url":"https://m"}`)}},
		Commands:     []Command{{Name: "c", Body: "cmd"}},
		Instructions: &Instructions{Name: "i", Body: "ins"},
	}
	first, _, err := Compose(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for range 10 {
		again, _, err := Compose(in)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(again.Files) != len(first.Files) {
			t.Fatalf("file count drifted: %d vs %d", len(again.Files), len(first.Files))
		}
		for p, b := range first.Files {
			if !bytes.Equal(again.Files[p], b) {
				t.Fatalf("non-deterministic output at %q", p)
			}
		}
	}
}
