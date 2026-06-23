package v1alpha1

import (
	"reflect"
	"strings"
	"testing"
)

func TestAgentHarnessValidate(t *testing.T) {
	meta := ObjectMeta{Namespace: "default", Name: "my-agent", Tag: "v1"}

	tests := []struct {
		name    string
		source  *AgentSource
		wantErr string // substring; empty means valid
	}{
		{
			name: "valid harness with plugin ref",
			source: &AgentSource{Harness: &HarnessConfig{
				Type:    "claude-code",
				Plugins: []ResourceRef{{Kind: KindPlugin, Name: "company-deploy", Tag: "v1"}},
			}},
		},
		{
			name:    "harness type required",
			source:  &AgentSource{Harness: &HarnessConfig{}},
			wantErr: "spec.source.harness.type",
		},
		{
			name: "harness and image mutually exclusive",
			source: &AgentSource{
				Image:   "ghcr.io/org/agent:1.0.0",
				Harness: &HarnessConfig{Type: "claude-code"},
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "plugin ref wrong kind",
			source: &AgentSource{Harness: &HarnessConfig{
				Type:    "codex",
				Plugins: []ResourceRef{{Kind: KindMCPServer, Name: "x"}},
			}},
			wantErr: "must be \"Plugin\"",
		},
		{
			name: "instructions must be a Prompt",
			source: &AgentSource{Harness: &HarnessConfig{
				Type:         "claude-code",
				Instructions: &ResourceRef{Kind: KindSkill, Name: "x"},
			}},
			wantErr: "must be \"Prompt\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Agent{
				TypeMeta: TypeMeta{APIVersion: GroupVersion, Kind: KindAgent},
				Metadata: meta,
				Spec:     AgentSpec{Source: tt.source},
			}
			err := a.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected valid, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// TestHarnessRefKindDefaultingPersists guards the fix for the harness-ref
// kind-defaulting bug: an omitted Kind must default AND persist into the spec,
// because the deploy-time resolver looks up stores[ref.Kind] with no defaulting
// of its own (an empty Kind would resolve to no store). Before the fix, the
// default lived in a local variable, so an omitted Kind was rejected outright
// and never reached the persisted ref.
func TestHarnessRefKindDefaultingPersists(t *testing.T) {
	a := &Agent{
		TypeMeta: TypeMeta{APIVersion: GroupVersion, Kind: KindAgent},
		Metadata: ObjectMeta{Namespace: "default", Name: "my-agent", Tag: "v1"},
		Spec: AgentSpec{
			MCPServers: []ResourceRef{{Name: "top-mcp"}}, // empty Kind (the twin path)
			Source: &AgentSource{
				Harness: &HarnessConfig{
					Type:         "claude-code",
					Plugins:      []ResourceRef{{Name: "plugin-a"}},
					Instructions: &ResourceRef{Name: "instr-a"},
				},
			},
		},
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("expected valid (kinds default in place), got: %v", err)
	}

	h := a.Spec.Source.Harness
	for _, c := range []struct{ field, got, want string }{
		{"harness.plugins", h.Plugins[0].Kind, KindPlugin},
		{"harness.instructions", h.Instructions.Kind, KindPrompt},
		{"spec.mcpServers", a.Spec.MCPServers[0].Kind, KindMCPServer},
	} {
		if c.got != c.want {
			t.Errorf("%s: kind not defaulted in place: got %q, want %q", c.field, c.got, c.want)
		}
	}
}

func TestHarnessConfigExposesOnlyPhase1Refs(t *testing.T) {
	harnessType := reflect.TypeOf(HarnessConfig{})
	for _, removed := range []string{"Skills", "MCPServers"} {
		if _, ok := harnessType.FieldByName(removed); ok {
			t.Fatalf("HarnessConfig must not expose %s in Phase 1; use plugins/instructions plus top-level AgentSpec.MCPServers", removed)
		}
	}
}
