package translate

import (
	"errors"
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/store"
)

func richCanonical() *store.CanonicalBundle {
	return &store.CanonicalBundle{Files: map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"my-plugin","version":"1.0.0"}`), // real manifest: passes through
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

	harnessFiles, toRep, err := ToHarness(HarnessClaudeCode, orig)
	if err != nil {
		t.Fatalf("ToHarness: %v", err)
	}
	if !toRep.IsClean() {
		t.Fatalf("expected clean (identity) to-harness translation, got %+v", toRep)
	}
	// Identity: every canonical file (incl. the real manifest) is preserved verbatim.
	if !reflect.DeepEqual(orig.Files, harnessFiles) {
		t.Fatalf("ToHarness should be identity for claude-code")
	}
	if _, ok := harnessFiles[".claude-plugin/plugin.json"]; !ok {
		t.Fatal("real manifest did not pass through")
	}

	canon, fromRep, err := FromHarness(HarnessClaudeCode, harnessFiles)
	if err != nil {
		t.Fatalf("FromHarness: %v", err)
	}
	if fromRep.HasLoss() {
		t.Fatalf("from-harness should not lose anything, got %+v", fromRep.Dropped)
	}
	if !reflect.DeepEqual(canon.Files, orig.Files) {
		t.Fatalf("round-trip not lossless")
	}
	h1, _ := orig.ContentHash()
	h2, _ := canon.ContentHash()
	if h1 != h2 {
		t.Fatalf("content hash changed across round-trip: %q vs %q", h1, h2)
	}
}

func TestToHarnessDeterministic(t *testing.T) {
	b := richCanonical()
	a, _, _ := ToHarness(HarnessClaudeCode, b)
	c, _, _ := ToHarness(HarnessClaudeCode, b)
	if !reflect.DeepEqual(a, c) {
		t.Fatal("ToHarness not deterministic")
	}
}

func TestCodexUnsupported(t *testing.T) {
	if _, _, err := ToHarness(HarnessCodex, richCanonical()); !errors.Is(err, ErrUnsupportedHarness) {
		t.Fatalf("expected ErrUnsupportedHarness for codex, got %v", err)
	}
}

func TestUnknownHarness(t *testing.T) {
	if _, _, err := ToHarness(Harness("zed"), richCanonical()); !errors.Is(err, ErrUnknownHarness) {
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
		"bin/":                  KindOther, // bare dir excluded, mirrors store inventory scan
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
