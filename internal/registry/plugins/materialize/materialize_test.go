package materialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/translate"
)

func TestMaterializePluginAndWriteDir(t *testing.T) {
	b := &bundle.CanonicalBundle{Files: map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"company-deploy"}`), // real manifest, passes through
		"skills/deploy/SKILL.md":     []byte("---\nname: deploy\n---\n"),
		".mcp.json":                  []byte(`{"mcpServers":{"db":{}}}`),
	}}

	files, rep, err := Plugin(b, translate.HarnessClaudeCode)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if rep.HasLoss() {
		t.Fatalf("claude-code materialize should be lossless, dropped %+v", rep.Dropped)
	}
	if _, ok := files[".claude-plugin/plugin.json"]; !ok {
		t.Fatal("manifest not materialized")
	}
	if _, ok := files["skills/deploy/SKILL.md"]; !ok {
		t.Fatal("skill not materialized")
	}

	dir := t.TempDir()
	if err := WriteDir(files, dir); err != nil {
		t.Fatalf("WriteDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "deploy", "SKILL.md")); err != nil {
		t.Fatalf("expected SKILL.md on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("expected manifest on disk: %v", err)
	}
}

func TestMaterializeNilBundleFails(t *testing.T) {
	if _, _, err := Plugin(nil, translate.HarnessClaudeCode); err == nil {
		t.Fatal("expected error for nil bundle")
	}
}

func TestWriteDirRejectsTraversal(t *testing.T) {
	err := WriteDir(map[string][]byte{"../escape": []byte("x")}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "traversal") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
}
