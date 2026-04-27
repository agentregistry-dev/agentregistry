package agent

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestExtractSkillImageRef(t *testing.T) {
	tests := []struct {
		name      string
		resp      *v1alpha1.Skill
		wantImage string
		wantErr   bool
	}{
		{
			name: "docker package",
			resp: &v1alpha1.Skill{
				Spec: v1alpha1.SkillSpec{
					Packages: []v1alpha1.SkillPackage{
						{RegistryType: "docker", Identifier: "docker.io/org/skill:1.0.0"},
					},
				},
			},
			wantImage: "docker.io/org/skill:1.0.0",
		},
		{
			name: "oci package",
			resp: &v1alpha1.Skill{
				Spec: v1alpha1.SkillSpec{
					Packages: []v1alpha1.SkillPackage{
						{RegistryType: "oci", Identifier: "ghcr.io/org/skill:1.2.3"},
					},
				},
			},
			wantImage: "ghcr.io/org/skill:1.2.3",
		},
		{
			name: "missing docker package",
			resp: &v1alpha1.Skill{
				Spec: v1alpha1.SkillSpec{
					Packages: []v1alpha1.SkillPackage{
						{RegistryType: "npm", Identifier: "@org/skill"},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractSkillImageRef(tt.resp)
			if (err != nil) != tt.wantErr {
				t.Fatalf("extractSkillImageRef() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.wantImage {
				t.Fatalf("extractSkillImageRef() = %q, want %q", got, tt.wantImage)
			}
		})
	}
}

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
					Repository: &v1alpha1.Repository{
						Source: "git",
						URL:    "https://github.com/org/skill/tree/main/skills/my-skill",
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
			name: "non-git source with URL still resolves",
			resp: &v1alpha1.Skill{
				Spec: v1alpha1.SkillSpec{
					Repository: &v1alpha1.Repository{
						Source: "svn",
						URL:    "https://gitlab.com/org/skill",
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
