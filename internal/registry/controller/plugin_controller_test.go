package controller

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/source"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestClassifyResolveErr(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantReason   string
		wantTerminal bool
	}{
		{"unsupported source", fmt.Errorf("wrap: %w", source.ErrUnsupportedSource), "SourceUnsupported", true},
		{"invalid bundle", fmt.Errorf("wrap: %w", bundle.ErrInvalidBundle), "SourceInvalid", true},
		{"transient", errors.New("dial tcp: timeout"), "SourceUnresolvable", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, terminal := classifyResolveErr(tt.err)
			if reason != tt.wantReason || terminal != tt.wantTerminal {
				t.Fatalf("classifyResolveErr = (%q, %v), want (%q, %v)", reason, terminal, tt.wantReason, tt.wantTerminal)
			}
		})
	}
}

func TestPluginReconciled(t *testing.T) {
	plugin := func(observed, gen int64, ready v1alpha1.ConditionStatus) *v1alpha1.Plugin {
		p := &v1alpha1.Plugin{}
		p.Metadata.Generation = gen
		p.Status.ObservedGeneration = observed
		p.Status.ScanVersion = v1alpha1.PluginScanVersion
		p.Status.SetCondition(v1alpha1.Condition{Type: pluginReadyCondition, Status: ready, Reason: "x"})
		return p
	}

	// Gates on ObservedGeneration and ScanVersion; Ready true/false is irrelevant.
	if !pluginReconciled(plugin(3, 3, v1alpha1.ConditionTrue)) {
		t.Fatal("observed==gen (success) should be reconciled")
	}
	if !pluginReconciled(plugin(3, 3, v1alpha1.ConditionFalse)) {
		t.Fatal("observed==gen (terminal failure) should be reconciled — must NOT re-resolve every tick")
	}
	if pluginReconciled(plugin(2, 3, v1alpha1.ConditionFalse)) {
		t.Fatal("observed<gen (retryable / pending) should NOT be reconciled")
	}
	if pluginReconciled(&v1alpha1.Plugin{}) {
		t.Fatal("a fresh plugin (generation 0 in this zero value) should NOT be reconciled")
	}
	stale := plugin(3, 3, v1alpha1.ConditionTrue)
	stale.Status.ScanVersion = v1alpha1.PluginScanVersion - 1
	if pluginReconciled(stale) {
		t.Fatal("observed==gen with a stale scan version should NOT be reconciled — it must rescan")
	}
}

func TestScanStatus(t *testing.T) {
	resolved := &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginSourceTypeGit, Commit: "abc123"}
	agentPlugins := &bundle.CanonicalBundle{Files: map[string][]byte{
		"plugin.json":            []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"acme.test","version":"1.0.0"}`),
		"mcp.json":               []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"search":{"type":"streamable-http","url":"https://example.com/mcp"}}}`),
		".mcp.json":              []byte(`{"mcpServers":{"claude-only":{"url":"https://example.com/mcp"}}}`),
		"skills/review/SKILL.md": []byte("---\nname: review\n---\n"),
	}}
	mutate, err := scanStatus(resolved, agentPlugins)
	if err != nil {
		t.Fatalf("scanStatus: %v", err)
	}
	got := v1alpha1.PluginStatus{Formats: []v1alpha1.PluginFormat{v1alpha1.PluginFormatClaudePlugin}}
	mutate(&got)
	want := v1alpha1.PluginStatus{
		ResolvedSource: resolved,
		Manifest:       &v1alpha1.PluginManifest{Schema: "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json", Name: "acme.test", Version: "1.0.0"},
		Inventory:      &v1alpha1.PluginInventory{Skills: []v1alpha1.PluginSkill{{Name: "review"}}, MCPServers: []string{"search"}},
		Formats:        []v1alpha1.PluginFormat{v1alpha1.PluginFormatAgentPlugins},
		ScanVersion:    v1alpha1.PluginScanVersion,
	}
	want.SetCondition(v1alpha1.Condition{Type: pluginReadyCondition, Status: v1alpha1.ConditionTrue, Reason: "Resolved"})
	assertStatusEqual(t, want, got)

	for name, files := range map[string]map[string][]byte{
		"rules reject":           {"SKILL.md": []byte("x")},
		"manifest parse rejects": {bundle.ManifestPath: []byte(`{"name":"a","hooks":5}`)},
	} {
		if _, err := scanStatus(resolved, &bundle.CanonicalBundle{Files: files}); !errors.Is(err, bundle.ErrInvalidBundle) {
			t.Errorf("%s: err = %v, want ErrInvalidBundle", name, err)
		}
	}
}

// TestTerminalStatus guards the rescan gate: a terminal failure must clear the
// formats and record the current scan version, or the Plugin rescans forever.
func TestTerminalStatus(t *testing.T) {
	got := v1alpha1.PluginStatus{Formats: []v1alpha1.PluginFormat{v1alpha1.PluginFormatClaudePlugin}}
	got.ObservedGeneration = 3
	got.SetCondition(v1alpha1.Condition{Type: pluginReadyCondition, Status: v1alpha1.ConditionTrue, Reason: "Resolved"})
	terminalStatus("SourceInvalid", errors.New("no manifest"))(&got)

	want := v1alpha1.PluginStatus{ScanVersion: v1alpha1.PluginScanVersion}
	want.ObservedGeneration = 3
	want.SetCondition(v1alpha1.Condition{Type: pluginReadyCondition, Status: v1alpha1.ConditionFalse, Reason: "SourceInvalid", Message: "no manifest"})
	assertStatusEqual(t, want, got)
}

// assertStatusEqual compares whole statuses, ignoring condition timestamps.
func assertStatusEqual(t *testing.T, want, got v1alpha1.PluginStatus) {
	t.Helper()
	for _, st := range []*v1alpha1.PluginStatus{&want, &got} {
		for i := range st.Conditions {
			st.Conditions[i].LastTransitionTime = time.Time{}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("status = %+v, want %+v", got, want)
	}
}
