package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"gopkg.in/yaml.v3"
)

func TestAddSkillWithImage(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, baseManifest())

	// Override package-level flags for test
	skillProjectDir = dir
	skillImage = "docker.io/org/my-skill:v1"
	skillRegistrySkillName = ""
	skillRegistrySkillVersion = ""
	skillRegistryURL = ""

	if err := addSkillCmd("my-skill"); err != nil {
		t.Fatalf("addSkillCmd() error: %v", err)
	}

	manifest := readTestManifest(t, dir)
	if len(manifest.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(manifest.Skills))
	}
	if manifest.Skills[0].Name != "my-skill" {
		t.Errorf("expected skill name 'my-skill', got '%s'", manifest.Skills[0].Name)
	}
	if manifest.Skills[0].Image != "docker.io/org/my-skill:v1" {
		t.Errorf("expected image 'docker.io/org/my-skill:v1', got '%s'", manifest.Skills[0].Image)
	}
}

func TestAddSkillWithRegistry(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, baseManifest())

	skillProjectDir = dir
	skillImage = ""
	skillRegistrySkillName = "cool-skill"
	skillRegistrySkillVersion = "1.0.0"
	skillRegistryURL = "https://registry.example.com"

	if err := addSkillCmd("cool"); err != nil {
		t.Fatalf("addSkillCmd() error: %v", err)
	}

	manifest := readTestManifest(t, dir)
	if len(manifest.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(manifest.Skills))
	}
	if manifest.Skills[0].RegistrySkillName != "cool-skill" {
		t.Errorf("expected registrySkillName 'cool-skill', got '%s'", manifest.Skills[0].RegistrySkillName)
	}
	if manifest.Skills[0].RegistrySkillVersion != "1.0.0" {
		t.Errorf("expected registrySkillVersion '1.0.0', got '%s'", manifest.Skills[0].RegistrySkillVersion)
	}
	if manifest.Skills[0].RegistryURL != "https://registry.example.com" {
		t.Errorf("expected registryURL 'https://registry.example.com', got '%s'", manifest.Skills[0].RegistryURL)
	}
}

func TestAddSkillDuplicateName(t *testing.T) {
	dir := t.TempDir()
	m := baseManifest()
	m.Skills = []models.SkillRef{
		{Name: "existing-skill", Image: "docker.io/org/skill:v1"},
	}
	writeTestManifest(t, dir, m)

	skillProjectDir = dir
	skillImage = "docker.io/org/another:v2"
	skillRegistrySkillName = ""

	err := addSkillCmd("existing-skill")
	if err == nil {
		t.Fatal("expected error for duplicate skill name, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestAddSkillNoFlags(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, baseManifest())

	skillProjectDir = dir
	skillImage = ""
	skillRegistrySkillName = ""

	err := addSkillCmd("no-flags")
	if err == nil {
		t.Fatal("expected error when no flags set, got nil")
	}
	if !strings.Contains(err.Error(), "one of --image or --registry-skill-name is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAddSkillMultipleFlags(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, baseManifest())

	skillProjectDir = dir
	skillImage = "docker.io/org/skill:v1"
	skillRegistrySkillName = "some-skill"

	err := addSkillCmd("conflict")
	if err == nil {
		t.Fatal("expected error for multiple flags, got nil")
	}
	if !strings.Contains(err.Error(), "only one of") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAddSkillSortsAlphabetically(t *testing.T) {
	dir := t.TempDir()
	writeTestManifest(t, dir, baseManifest())

	skillProjectDir = dir
	skillRegistrySkillName = ""
	skillRegistrySkillVersion = ""
	skillRegistryURL = ""

	// Add skills in reverse alphabetical order
	skillImage = "z:latest"
	if err := addSkillCmd("zulu"); err != nil {
		t.Fatalf("addSkillCmd(zulu) error: %v", err)
	}

	skillImage = "a:latest"
	if err := addSkillCmd("alpha"); err != nil {
		t.Fatalf("addSkillCmd(alpha) error: %v", err)
	}

	skillImage = "m:latest"
	if err := addSkillCmd("mike"); err != nil {
		t.Fatalf("addSkillCmd(mike) error: %v", err)
	}

	manifest := readTestManifest(t, dir)
	if len(manifest.Skills) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(manifest.Skills))
	}

	expected := []string{"alpha", "mike", "zulu"}
	for i, want := range expected {
		if manifest.Skills[i].Name != want {
			t.Errorf("skills[%d]: expected %q, got %q", i, want, manifest.Skills[i].Name)
		}
	}
}

func TestSkillValidation(t *testing.T) {
	tests := []struct {
		name       string
		skills     []models.SkillRef
		wantErr    bool
		errContain string
	}{
		{
			name:    "valid image skill",
			skills:  []models.SkillRef{{Name: "s1", Image: "img:latest"}},
			wantErr: false,
		},
		{
			name:    "valid path skill",
			skills:  []models.SkillRef{{Name: "s1", Path: "skills/s1"}},
			wantErr: false,
		},
		{
			name:    "valid registry skill",
			skills:  []models.SkillRef{{Name: "s1", RegistrySkillName: "remote-skill"}},
			wantErr: false,
		},
		{
			name:       "missing name",
			skills:     []models.SkillRef{{Image: "img:latest"}},
			wantErr:    true,
			errContain: "name is required",
		},
		{
			name:       "no source specified",
			skills:     []models.SkillRef{{Name: "s1"}},
			wantErr:    true,
			errContain: "one of image, path, or registrySkillName is required",
		},
		{
			name:       "multiple sources",
			skills:     []models.SkillRef{{Name: "s1", Image: "img", Path: "path"}},
			wantErr:    true,
			errContain: "only one of",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := baseManifest()
			manifest.Skills = tt.skills
			validator := &common.AgentManifestValidator{}
			err := validator.Validate(manifest)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errContain != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.errContain)
				}
			}
		})
	}
}

func baseManifest() *models.AgentManifest {
	return &models.AgentManifest{
		Name:      "test-agent",
		Language:  "python",
		Framework: "adk",
	}
}

func writeTestManifest(t *testing.T, dir string, manifest *models.AgentManifest) {
	t.Helper()
	data, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("failed to marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), data, 0o644); err != nil {
		t.Fatalf("failed to write agent.yaml: %v", err)
	}
}

func readTestManifest(t *testing.T, dir string) *models.AgentManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "agent.yaml"))
	if err != nil {
		t.Fatalf("failed to read agent.yaml: %v", err)
	}
	var manifest models.AgentManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("failed to parse agent.yaml: %v", err)
	}
	return &manifest
}
