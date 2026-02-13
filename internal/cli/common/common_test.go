package common

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
)

func TestValidateProjectDir(t *testing.T) {
	tempDir := t.TempDir()

	tempFile := filepath.Join(tempDir, "testfile.txt")
	if err := os.WriteFile(tempFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	tests := []struct {
		name       string
		projectDir string
		wantErr    bool
		errContain string
	}{
		{
			name:       "valid directory",
			projectDir: tempDir,
			wantErr:    false,
		},
		{
			name:       "non-existent directory",
			projectDir: filepath.Join(tempDir, "nonexistent"),
			wantErr:    true,
			errContain: "does not exist",
		},
		{
			name:       "path is a file not directory",
			projectDir: tempFile,
			wantErr:    true,
			errContain: "is not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProjectDir(tt.projectDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateProjectDir(%q) error = %v, wantErr %v",
					tt.projectDir, err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContain != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("validateProjectDir(%q) error = %v, want error containing %q",
						tt.projectDir, err, tt.errContain)
				}
			}
		})
	}
}

func TestBuildLocalImageName(t *testing.T) {
	tests := []struct {
		name     string
		project  string
		version  string
		expected string
	}{
		{
			name:     "simple name and version",
			project:  "MyProject",
			version:  "1.0.0",
			expected: "my-project:1.0.0",
		},
		{
			name:     "name with spaces and empty version",
			project:  "Another Project",
			version:  "",
			expected: "another-project:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageName := BuildLocalImageName(tt.project, tt.version)
			if imageName != tt.expected {
				t.Errorf("BuildLocalImageName(%q, %q) = %q, want %q",
					tt.project, tt.version, imageName, tt.expected)
			}
		})
	}
}

func TestBuildRegistryImageName(t *testing.T) {
	tests := []struct {
		name        string
		registryURL string
		project     string
		version     string
		expected    string
	}{
		{
			name:        "simple registry URL",
			registryURL: "docker.io",
			project:     "MyProject",
			version:     "1.0.0",
			expected:    "docker.io/my-project:1.0.0",
		},
		{
			name:        "registry URL with trailing slash",
			registryURL: "gcr.io/",
			project:     "Another Project",
			version:     "",
			expected:    "gcr.io/another-project:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageRef := BuildRegistryImageName(tt.registryURL, tt.project, tt.version)
			if imageRef != tt.expected {
				t.Errorf("BuildRegistryImageName(%q, %q, %q) = %q, want %q",
					tt.registryURL, tt.project, tt.version, imageRef, tt.expected)
			}
		})
	}
}

type mockManifestLoader struct {
	exists   bool
	manifest *manifest.ProjectManifest
	loadErr  error
}

func (m *mockManifestLoader) Exists() bool {
	return m.exists
}

func (m *mockManifestLoader) Load() (*manifest.ProjectManifest, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	return m.manifest, nil
}

func TestGetImageNameFromManifest(t *testing.T) {
	tests := []struct {
		name          string
		loader        manifest.ManifestLoader
		expectedImage string
		expectErr     bool
	}{
		{
			name: "simple name and version",
			loader: &mockManifestLoader{
				exists: true,
				manifest: &manifest.ProjectManifest{
					Name:    "My MCP Server",
					Version: "1.0.0",
				},
			},
			expectedImage: "my-mcp-server:1.0.0",
			expectErr:     false,
		},
		{
			name: "empty version defaults to latest",
			loader: &mockManifestLoader{
				exists: true,
				manifest: &manifest.ProjectManifest{
					Name:    "My Server",
					Version: "",
				},
			},
			expectedImage: "my-server:latest",
			expectErr:     false,
		},
		{
			name: "manifest does not exist",
			loader: &mockManifestLoader{
				exists: false,
			},
			expectedImage: "",
			expectErr:     true,
		},
		{
			name: "load error",
			loader: &mockManifestLoader{
				exists:  true,
				loadErr: fmt.Errorf("failed to read file"),
			},
			expectedImage: "",
			expectErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imageName, err := GetImageNameFromManifest(tt.loader)

			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if imageName != tt.expectedImage {
				t.Errorf("expected image name %q, got %q", tt.expectedImage, imageName)
			}
		})
	}
}

func TestBuildMCPServerRegistryName(t *testing.T) {
	tests := []struct {
		name     string
		author   string
		project  string
		expected string
	}{
		{
			name:     "simple author and project",
			author:   "bob",
			project:  "MyProject",
			expected: "bob/myproject",
		},
		{
			name:     "empty author defaults to 'user'",
			author:   "",
			project:  "AnotherProject",
			expected: "user/anotherproject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registryName := BuildMCPServerRegistryName(tt.author, tt.project)
			if registryName != tt.expected {
				t.Errorf("BuildMCPServerRegistryName(%q, %q) = %q, want %q",
					tt.author, tt.project, registryName, tt.expected)
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		flagVersion     string
		manifestVersion string
		actual          string
		expected        string
	}{
		{
			flagVersion:     "1.0.0",
			manifestVersion: "2.0.0",
			expected:        "1.0.0",
		},
		{
			flagVersion:     "",
			manifestVersion: "2.0.0",
			expected:        "2.0.0",
		},
		{
			flagVersion:     "",
			manifestVersion: "",
			expected:        "latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.flagVersion, func(t *testing.T) {
			actual := ResolveVersion(tt.flagVersion, tt.manifestVersion)
			if actual != tt.expected {
				t.Errorf("expected %s but got %s", tt.expected, actual)
			}
		})
	}
}
