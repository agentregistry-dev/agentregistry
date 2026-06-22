package translate

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/store"
)

func richCanonical() *store.CanonicalBundle {
	return &store.CanonicalBundle{Files: map[string][]byte{
		"SKILL.md":                   []byte("---\nname: root\n---\n"),
		"skills/deploy/SKILL.md":     []byte("---\nname: deploy\n---\n"),
		"skills/deploy/reference.md": []byte("supporting file"), // must survive (default-pass)
		"AGENTS.md":                  []byte("# instructions"),  // must survive (KindAgentsMd)
		"agents/reviewer.md":         []byte("a subagent"),
		".mcp.json":                  []byte(`{"mcpServers":{"db":{}}}`),
		"hooks/hooks.json":           []byte(`{"hooks":{}}`),
		"themes/dracula.json":        []byte(`{"name":"Dracula"}`), // Claude-only extra; superset-canonical keeps it
		"bin/tool":                   []byte("#!/bin/sh"),
	}}
}

func TestClaudeCodeRoundTripLossless(t *testing.T) {
	orig := richCanonical()
	meta := PluginMeta{Name: "my-plugin", Version: "v1", Title: "My Plugin", Description: "does things"}

	harnessFiles, toRep, err := ToHarness(HarnessClaudeCode, orig, meta)
	if err != nil {
		t.Fatalf("ToHarness: %v", err)
	}
	if !toRep.IsClean() {
		t.Fatalf("expected clean to-harness translation, got %+v", toRep)
	}
	if _, ok := harnessFiles[".claude-plugin/plugin.json"]; !ok {
		t.Fatal("manifest not generated")
	}
	// Every canonical file must appear unchanged in the harness layout.
	for p, want := range orig.Files {
		if got, ok := harnessFiles[p]; !ok || !bytes.Equal(got, want) {
			t.Fatalf("file %q not identity-preserved on ToHarness", p)
		}
	}

	canon, gotMeta, fromRep, err := FromHarness(HarnessClaudeCode, harnessFiles)
	if err != nil {
		t.Fatalf("FromHarness: %v", err)
	}
	if fromRep.HasLoss() {
		t.Fatalf("from-harness should not lose anything, got %+v", fromRep.Dropped)
	}
	if !reflect.DeepEqual(canon.Files, orig.Files) {
		t.Fatalf("round-trip not lossless:\n want %v\n got  %v", keys(orig.Files), keys(canon.Files))
	}
	if gotMeta.Name != "my-plugin" || gotMeta.Title != "My Plugin" || gotMeta.Version != "v1" {
		t.Fatalf("metadata not recovered: %+v", gotMeta)
	}
	// Content hash stable across the round-trip.
	h1, _ := orig.ContentHash()
	h2, _ := canon.ContentHash()
	if h1 != h2 {
		t.Fatalf("content hash changed across round-trip: %q vs %q", h1, h2)
	}
}

func TestToHarnessDeterministic(t *testing.T) {
	b := richCanonical()
	meta := PluginMeta{Name: "p", Version: "v1"}
	a, _, _ := ToHarness(HarnessClaudeCode, b, meta)
	c, _, _ := ToHarness(HarnessClaudeCode, b, meta)
	if !reflect.DeepEqual(a, c) {
		t.Fatal("ToHarness not deterministic")
	}
}

func TestCodexUnsupported(t *testing.T) {
	_, _, err := ToHarness(HarnessCodex, richCanonical(), PluginMeta{Name: "p"})
	if !errors.Is(err, ErrUnsupportedHarness) {
		t.Fatalf("expected ErrUnsupportedHarness for codex, got %v", err)
	}
}

func TestUnknownHarness(t *testing.T) {
	_, _, err := ToHarness(Harness("zed"), richCanonical(), PluginMeta{Name: "p"})
	if !errors.Is(err, ErrUnknownHarness) {
		t.Fatalf("expected ErrUnknownHarness, got %v", err)
	}
}

func TestClassifyMatchesEdgeCases(t *testing.T) {
	cases := map[string]ComponentKind{
		"SKILL.md":              KindSkill,
		"skills/x/SKILL.md":     KindSkill,
		"AGENTS.md":             KindAgentsMd,
		"agents/r.md":           KindAgent,
		"commands/s.md":         KindCommand,
		"bin/tool":              KindBin,
		"bin/":                  KindOther, // bare dir excluded, mirrors store.ParseManifest
		".mcp.json":             KindMCP,
		"hooks/hooks.json":      KindHooks,
		"skills/x/reference.md": KindOther,
	}
	for p, want := range cases {
		if got := Classify(p); got != want {
			t.Errorf("Classify(%q) = %q, want %q", p, got, want)
		}
	}
}

func TestHarnessesRegistered(t *testing.T) {
	hs := Harnesses()
	if len(hs) != 1 || hs[0] != HarnessClaudeCode {
		t.Fatalf("expected only claude-code registered, got %v", hs)
	}
}

func keys(m map[string][]byte) []string { return sortedKeys(m) }
