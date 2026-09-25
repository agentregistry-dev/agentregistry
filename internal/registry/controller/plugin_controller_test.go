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

// TestPluginReconciled checks the gate on ObservedGeneration and ScanVersion.
// Ready true/false is irrelevant: a terminal failure must NOT re-resolve every tick.
func TestPluginReconciled(t *testing.T) {
	const current = v1alpha1.PluginScanVersion
	tests := []struct {
		name                              string
		observed, generation, scanVersion int64
		ready                             v1alpha1.ConditionStatus
		want                              bool
	}{
		{"success at current generation", 3, 3, current, v1alpha1.ConditionTrue, true},
		{"terminal failure at current generation", 3, 3, current, v1alpha1.ConditionFalse, true},
		{"retryable or pending", 2, 3, current, v1alpha1.ConditionFalse, false},
		{"fresh plugin with generation 0", 0, 0, 0, "", false},
		{"older scan version rescans", 3, 3, current - 1, v1alpha1.ConditionTrue, false},
		{"newer scan version after a rollback rescans", 3, 3, current + 1, v1alpha1.ConditionTrue, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &v1alpha1.Plugin{}
			p.Metadata.Generation = tt.generation
			p.Status.ObservedGeneration = tt.observed
			p.Status.ScanVersion = tt.scanVersion
			p.Status.SetCondition(v1alpha1.Condition{Type: pluginReadyCondition, Status: tt.ready, Reason: "x"})
			if got := pluginReconciled(p); got != tt.want {
				t.Fatalf("pluginReconciled = %v, want %v", got, tt.want)
			}
		})
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

	if _, err := scanStatus(resolved, &bundle.CanonicalBundle{Files: map[string][]byte{"SKILL.md": []byte("x")}}); !errors.Is(err, bundle.ErrInvalidBundle) {
		t.Errorf("rules reject: err = %v, want ErrInvalidBundle", err)
	}
	// kagent ignores a key its rules do not check, so the scan must too.
	ignoredKey := &bundle.CanonicalBundle{Files: map[string][]byte{bundle.ManifestPath: []byte(`{"name":"a","hooks":5}`)}}
	if _, err := scanStatus(resolved, ignoredKey); err != nil {
		t.Errorf("unparsable ignored key: err = %v, want nil", err)
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
