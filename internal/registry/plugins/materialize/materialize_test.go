package materialize

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/registry"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/store"
	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/translate"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

type anonKeychain struct{}

func (anonKeychain) Resolve(authn.Resource) (authn.Authenticator, error) { return authn.Anonymous, nil }

func newStore(t *testing.T) store.Store {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	st, err := store.NewOCIStore(store.Config{
		Registry: strings.TrimPrefix(srv.URL, "http://"),
		Insecure: true,
		Keychain: anonKeychain{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestMaterializePluginAndWriteDir(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	bundle := &store.CanonicalBundle{Files: map[string][]byte{
		"skills/deploy/SKILL.md": []byte("---\nname: deploy\n---\n"),
		".mcp.json":              []byte(`{"mcpServers":{"db":{}}}`),
	}}
	ref, hash, err := st.Push(ctx, "author", "company-deploy", "v1", bundle)
	if err != nil {
		t.Fatal(err)
	}

	p := &v1alpha1.Plugin{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "company-deploy", Tag: "v1"},
		Spec:     v1alpha1.PluginSpec{Content: &v1alpha1.PluginContent{ContentHash: hash, OCIRef: ref}},
	}

	files, rep, err := Plugin(ctx, st, p, translate.HarnessClaudeCode)
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

func TestMaterializeUnpublishedPluginFails(t *testing.T) {
	_, _, err := Plugin(context.Background(), newStore(t),
		&v1alpha1.Plugin{Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "x"}},
		translate.HarnessClaudeCode)
	if err == nil || !strings.Contains(err.Error(), "no stored content") {
		t.Fatalf("expected no-content error, got %v", err)
	}
}

func TestWriteDirRejectsTraversal(t *testing.T) {
	err := WriteDir(map[string][]byte{"../escape": []byte("x")}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "traversal") {
		t.Fatalf("expected traversal rejection, got %v", err)
	}
}
