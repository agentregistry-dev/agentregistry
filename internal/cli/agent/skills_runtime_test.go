package agent

import (
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

func TestExtractSkillImageRef(t *testing.T) {
	tests := []struct {
		name      string
		resp      *models.SkillResponse
		wantImage string
		wantErr   bool
	}{
		{
			name: "docker package",
			resp: &models.SkillResponse{
				Skill: models.SkillJSON{
					Packages: []models.SkillPackageInfo{
						{RegistryType: "docker", Identifier: "docker.io/org/skill:1.0.0"},
					},
				},
			},
			wantImage: "docker.io/org/skill:1.0.0",
		},
		{
			name: "oci package",
			resp: &models.SkillResponse{
				Skill: models.SkillJSON{
					Packages: []models.SkillPackageInfo{
						{RegistryType: "oci", Identifier: "ghcr.io/org/skill:1.2.3"},
					},
				},
			},
			wantImage: "ghcr.io/org/skill:1.2.3",
		},
		{
			name: "missing docker package",
			resp: &models.SkillResponse{
				Skill: models.SkillJSON{
					Packages: []models.SkillPackageInfo{
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

func TestNormalizeSkillRegistryURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "appends v0",
			input: "https://registry.example.com",
			want:  "https://registry.example.com/v0",
		},
		{
			name:  "keeps existing v0",
			input: "https://registry.example.com/v0",
			want:  "https://registry.example.com/v0",
		},
		{
			name:    "empty url",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeSkillRegistryURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("normalizeSkillRegistryURL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("normalizeSkillRegistryURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveSkillSourceImagePassthrough(t *testing.T) {
	ref := models.SkillRef{
		Name:  "local",
		Image: "docker.io/org/skill:latest",
	}

	got, err := resolveSkillSource(ref)
	if err != nil {
		t.Fatalf("resolveSkillSource() error = %v", err)
	}
	if got.image != ref.Image {
		t.Fatalf("resolveSkillSource().image = %q, want %q", got.image, ref.Image)
	}
	if got.repoURL != "" {
		t.Fatalf("resolveSkillSource().repoURL = %q, want empty", got.repoURL)
	}
}

func TestResolveSkillSourceValidation(t *testing.T) {
	tests := []struct {
		name       string
		ref        models.SkillRef
		errContain string
	}{
		{
			name: "missing image and registry skill name",
			ref: models.SkillRef{
				Name: "missing",
			},
			errContain: "one of image or registrySkillName is required",
		},
		{
			name: "both image and registry skill name set",
			ref: models.SkillRef{
				Name:              "invalid-both",
				Image:             "docker.io/org/skill:latest",
				RegistrySkillName: "remote-skill",
			},
			errContain: "only one of image or registrySkillName may be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveSkillSource(tt.ref)
			if err == nil {
				t.Fatalf("resolveSkillSource() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContain) {
				t.Fatalf("resolveSkillSource() error = %q, want substring %q", err.Error(), tt.errContain)
			}
		})
	}
}

func TestExtractSkillRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		resp    *models.SkillResponse
		wantURL string
		wantErr bool
	}{
		{
			name: "git repository",
			resp: &models.SkillResponse{
				Skill: models.SkillJSON{
					Repository: &models.SkillRepository{
						Source: "git",
						URL:    "https://github.com/org/skill/tree/main/skills/my-skill",
					},
				},
			},
			wantURL: "https://github.com/org/skill/tree/main/skills/my-skill",
		},
		{
			name: "no repository",
			resp: &models.SkillResponse{
				Skill: models.SkillJSON{},
			},
			wantErr: true,
		},
		{
			name: "non-git source with URL still resolves",
			resp: &models.SkillResponse{
				Skill: models.SkillJSON{
					Repository: &models.SkillRepository{
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
