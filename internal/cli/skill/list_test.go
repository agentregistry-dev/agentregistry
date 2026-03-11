package skill

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

func TestSkillSource(t *testing.T) {
	tests := []struct {
		name     string
		skill    *models.SkillResponse
		wantType string
		wantSrc  string
	}{
		{
			name: "package source",
			skill: &models.SkillResponse{
				Skill: models.SkillJSON{
					Packages: []models.SkillPackageInfo{
						{RegistryType: "docker", Identifier: "ghcr.io/org/skill:latest"},
					},
				},
			},
			wantType: "docker",
			wantSrc:  "ghcr.io/org/skill:latest",
		},
		{
			name: "repository source",
			skill: &models.SkillResponse{
				Skill: models.SkillJSON{
					Repository: &models.SkillRepository{
						Source: "git",
						URL:    "https://github.com/org/repo",
					},
				},
			},
			wantType: "git",
			wantSrc:  "https://github.com/org/repo",
		},
		{
			name: "package takes precedence over repository",
			skill: &models.SkillResponse{
				Skill: models.SkillJSON{
					Packages: []models.SkillPackageInfo{
						{RegistryType: "npm", Identifier: "@org/skill"},
					},
					Repository: &models.SkillRepository{
						Source: "git",
						URL:    "https://github.com/org/repo",
					},
				},
			},
			wantType: "npm",
			wantSrc:  "@org/skill",
		},
		{
			name: "no source",
			skill: &models.SkillResponse{
				Skill: models.SkillJSON{},
			},
			wantType: "<none>",
			wantSrc:  "<none>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotSrc := skillSource(tt.skill)
			if gotType != tt.wantType {
				t.Errorf("skillSource() type = %q, want %q", gotType, tt.wantType)
			}
			if gotSrc != tt.wantSrc {
				t.Errorf("skillSource() src = %q, want %q", gotSrc, tt.wantSrc)
			}
		})
	}
}
