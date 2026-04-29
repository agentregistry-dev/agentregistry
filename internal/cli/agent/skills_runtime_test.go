package agent

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestExtractSkillRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		resp    *v1alpha1.Skill
		wantURL string
		wantErr bool
	}{
		{
			name: "git repository",
			resp: &v1alpha1.Skill{
				Spec: v1alpha1.SkillSpec{
					Source: &v1alpha1.SkillSource{
						Repository: &v1alpha1.Repository{
							URL: "https://github.com/org/skill/tree/main/skills/my-skill",
						},
					},
				},
			},
			wantURL: "https://github.com/org/skill/tree/main/skills/my-skill",
		},
		{
			name: "no repository",
			resp: &v1alpha1.Skill{
				Spec: v1alpha1.SkillSpec{},
			},
			wantErr: true,
		},
		{
			name: "repository with URL resolves",
			resp: &v1alpha1.Skill{
				Spec: v1alpha1.SkillSpec{
					Source: &v1alpha1.SkillSource{
						Repository: &v1alpha1.Repository{
							URL: "https://gitlab.com/org/skill",
						},
					},
				},
			},
			wantURL: "https://gitlab.com/org/skill",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractSkillRepoURL(tt.resp)
			if (err != nil) != tt.wantErr {
				t.Fatalf("extractSkillRepoURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.wantURL {
				t.Fatalf("extractSkillRepoURL() = %q, want %q", got, tt.wantURL)
			}
		})
	}
}
