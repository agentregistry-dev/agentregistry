package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsLocalPath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "path_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		// Current and parent directory
		{"current directory", ".", true},
		{"parent directory", "..", true},

		// Absolute paths
		{"absolute path unix", "/usr/local/bin", true},
		{"absolute path root", "/", true},

		// Relative paths starting with dot
		{"relative path dot slash", "./mydir", true},
		{"relative path dot dot slash", "../mydir", true},
		{"relative hidden dir", ".hidden", true},

		// Paths starting with slash
		{"path starting with slash", "/some/path", true},

		// Existing directory
		{"existing temp directory", tempDir, true},

		// Non-path strings
		{"simple name", "myproject", false},
		{"registry reference", "registry.io/image:tag", false},
		{"empty string", "", false},
		{"name with hyphen", "my-project", false},
		{"name with underscore", "my_project", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsLocalPath(tt.path)
			if result != tt.expected {
				t.Errorf("IsLocalPath(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestIsLocalPath_ExistingSubdir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "path_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	subDir := filepath.Join(tempDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Test that existing subdirectory is recognized
	if !IsLocalPath(subDir) {
		t.Errorf("IsLocalPath(%q) = false, want true for existing directory", subDir)
	}
}

func TestIsLocalPath_NonExistentPath(t *testing.T) {
	// A path that doesn't exist and doesn't match other patterns
	nonExistent := "nonexistent-directory-abc123"
	if IsLocalPath(nonExistent) {
		t.Errorf("IsLocalPath(%q) = true, want false for non-existent path", nonExistent)
	}
}
