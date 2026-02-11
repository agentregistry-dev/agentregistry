package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			err := validateProjectDir(tt.projectDir)
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
