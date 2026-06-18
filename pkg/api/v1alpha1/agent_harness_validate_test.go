package v1alpha1

import (
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
