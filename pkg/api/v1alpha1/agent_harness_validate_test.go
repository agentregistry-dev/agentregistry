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
		spec    AgentSpec
		wantErr string // substring; empty means valid
	}{
		{
			name: "valid compatible harness with top-level plugin ref",
			spec: AgentSpec{
				Plugins:             []ResourceRef{{Kind: KindPlugin, Name: "company-deploy", Tag: "v1"}},
				CompatibleHarnesses: []HarnessCompatibility{{Type: "claude-code"}},
			},
		},
		{
			name:    "compatible harness type required",
			spec:    AgentSpec{CompatibleHarnesses: []HarnessCompatibility{{}}},
			wantErr: "spec.compatibleHarnesses[0].type",
		},
		{
			name: "image can coexist with harness compatibility",
			spec: AgentSpec{
				Source:              &AgentSource{Image: "ghcr.io/org/agent:1.0.0"},
				CompatibleHarnesses: []HarnessCompatibility{{Type: "claude-code"}},
			},
		},
		{
			name: "duplicate compatible harness rejected",
			spec: AgentSpec{
				CompatibleHarnesses: []HarnessCompatibility{{Type: "claude-code"}, {Type: "claude-code"}},
			},
			wantErr: "duplicate",
		},
		{
			name: "plugin ref wrong kind",
			spec: AgentSpec{
				Plugins:             []ResourceRef{{Kind: KindMCPServer, Name: "x"}},
				CompatibleHarnesses: []HarnessCompatibility{{Type: "codex"}},
			},
			wantErr: "must be \"Plugin\"",
		},
		{
			name: "skill ref wrong kind",
			spec: AgentSpec{
				Skills:              []ResourceRef{{Kind: KindPlugin, Name: "x"}},
				CompatibleHarnesses: []HarnessCompatibility{{Type: "claude-code"}},
			},
			wantErr: "must be \"Skill\"",
		},
		{
			name: "instructions must be a Prompt",
			spec: AgentSpec{
				Instructions:        &ResourceRef{Kind: KindSkill, Name: "x"},
				CompatibleHarnesses: []HarnessCompatibility{{Type: "claude-code"}},
			},
			wantErr: "must be \"Prompt\"",
		},
		{
			name: "composition requires harness compatibility",
			spec: AgentSpec{
				Plugins: []ResourceRef{{Kind: KindPlugin, Name: "x"}},
				Source:  &AgentSource{Image: "ghcr.io/org/agent:1.0.0"},
			},
			wantErr: "require compatibleHarnesses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Agent{
				TypeMeta: TypeMeta{APIVersion: GroupVersion, Kind: KindAgent},
				Metadata: meta,
				Spec:     tt.spec,
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

// TestCompositionRefKindDefaultingPersists guards the kind-defaulting fix: an
// omitted Kind on a composition ref must default AND persist into the spec,
// because the deploy-time resolver looks up stores[ref.Kind] with no defaulting
// of its own (an empty Kind would resolve to no store).
func TestCompositionRefKindDefaultingPersists(t *testing.T) {
	a := &Agent{
		TypeMeta: TypeMeta{APIVersion: GroupVersion, Kind: KindAgent},
		Metadata: ObjectMeta{Namespace: "default", Name: "my-agent", Tag: "v1"},
		Spec: AgentSpec{
			Plugins:      []ResourceRef{{Name: "plugin-a"}}, // empty Kind
			Skills:       []ResourceRef{{Name: "skill-a"}},  // empty Kind
			Instructions: &ResourceRef{Name: "instr-a"},     // empty Kind
			MCPServers:   []ResourceRef{{Name: "top-mcp"}},  // empty Kind
			CompatibleHarnesses: []HarnessCompatibility{
				{Type: "claude-code"},
			},
		},
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("expected valid (kinds default in place), got: %v", err)
	}

	for _, c := range []struct{ field, got, want string }{
		{"spec.plugins", a.Spec.Plugins[0].Kind, KindPlugin},
		{"spec.skills", a.Spec.Skills[0].Kind, KindSkill},
		{"spec.instructions", a.Spec.Instructions.Kind, KindPrompt},
		{"spec.mcpServers", a.Spec.MCPServers[0].Kind, KindMCPServer},
	} {
		if c.got != c.want {
			t.Errorf("%s: kind not defaulted in place: got %q, want %q", c.field, c.got, c.want)
		}
	}
}

// TestHarnessCompatibilityIsMatrixOnly asserts Agent compatibility is a
// harness-type matrix; the deployment owns concrete version/policy selection.
func TestHarnessCompatibilityIsMatrixOnly(t *testing.T) {
	harnessType := reflect.TypeFor[HarnessCompatibility]()
	for _, removed := range []string{"Version", "Plugins", "Skills", "Instructions", "MCPServers"} {
		if _, ok := harnessType.FieldByName(removed); ok {
			t.Fatalf("HarnessCompatibility must not expose %s; selection and composition live elsewhere", removed)
		}
	}
}

// TestDeploymentHarnessDoesNotExposeVersion asserts deployment harness
// selection stays limited to fields the deploy path actually consumes.
func TestDeploymentHarnessDoesNotExposeVersion(t *testing.T) {
	harnessType := reflect.TypeFor[DeploymentHarness]()
	if _, ok := harnessType.FieldByName("Version"); ok {
		t.Fatalf("DeploymentHarness must not expose Version until runtime image resolution uses it")
	}
}
