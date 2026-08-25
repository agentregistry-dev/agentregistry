package commands

import (
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestAgentDisplayMode(t *testing.T) {
	tests := []struct {
		name string
		spec v1alpha1.AgentSpec
		want string
	}{
		{
			name: "no runnable mode",
			want: "<none>",
		},
		{
			name: "empty source is not a runnable mode",
			spec: v1alpha1.AgentSpec{Source: &v1alpha1.AgentSource{}},
			want: "<none>",
		},
		{
			name: "image source",
			spec: v1alpha1.AgentSpec{
				Source: &v1alpha1.AgentSource{Image: "ghcr.io/example/agent:v1"},
			},
			want: "source",
		},
		{
			name: "repository source",
			spec: v1alpha1.AgentSpec{
				Source: &v1alpha1.AgentSource{
					Repository: &v1alpha1.Repository{URL: "https://github.com/example/agent"},
				},
			},
			want: "source",
		},
		{
			name: "harness compatibility",
			spec: v1alpha1.AgentSpec{
				CompatibleHarnesses: []v1alpha1.HarnessCompatibility{{Type: "claude-code"}},
			},
			want: "harness",
		},
		{
			name: "source and harness compatibility",
			spec: v1alpha1.AgentSpec{
				Source:              &v1alpha1.AgentSource{Image: "ghcr.io/example/agent:v1"},
				CompatibleHarnesses: []v1alpha1.HarnessCompatibility{{Type: "claude-code"}},
			},
			want: "source+harness",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentDisplayMode(tt.spec); got != tt.want {
				t.Fatalf("agentDisplayMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentRowIncludesModeAndDescription(t *testing.T) {
	agent := &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Name: "reviewer", Tag: "stable"},
		Spec: v1alpha1.AgentSpec{
			Description:         "Reviews pull requests",
			Source:              &v1alpha1.AgentSource{Image: "ghcr.io/example/reviewer:v1"},
			CompatibleHarnesses: []v1alpha1.HarnessCompatibility{{Type: "codex"}},
		},
	}

	want := []string{"reviewer", "stable", "source+harness", "Reviews pull requests"}
	if got := agentRow(agent); !reflect.DeepEqual(got, want) {
		t.Fatalf("agentRow() = %#v, want %#v", got, want)
	}
}
