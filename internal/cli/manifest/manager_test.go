package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type testManifest struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

type testValidator struct {
	shouldFail bool
}

func (v *testValidator) Validate(m *testManifest) error {
	if v.shouldFail {
		return fmt.Errorf("validation failed")
	}
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

func TestManager_Exists(t *testing.T) {
	tests := []struct {
		name       string
		createFile bool
		want       bool
	}{
		{"file exists", true, true},
		{"file does not exist", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.createFile {
				if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte("name: test"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			mgr := NewManager[*testManifest](dir, "test.yaml", nil)
			if got := mgr.Exists(); got != tt.want {
				t.Errorf("Exists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManager_Load(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		createFile  bool
		validator   Validator[*testManifest]
		wantErr     bool
		errContains string
	}{
		{
			name:       "valid manifest",
			content:    "name: test\nversion: 1.0.0",
			createFile: true,
			validator:  &testValidator{},
			wantErr:    false,
		},
		{
			name:        "file not found",
			createFile:  false,
			wantErr:     true,
			errContains: "not found",
		},
		{
			name:        "invalid yaml",
			content:     "{{invalid yaml",
			createFile:  true,
			wantErr:     true,
			errContains: "parsing",
		},
		{
			name:        "validation fails",
			content:     "name: test",
			createFile:  true,
			validator:   &testValidator{shouldFail: true},
			wantErr:     true,
			errContains: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.createFile {
				if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(tt.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			mgr := NewManager(dir, "test.yaml", tt.validator)
			got, err := mgr.Load()

			if tt.wantErr {
				if err == nil {
					t.Error("Load() expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("Load() error = %v, want containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("Load() unexpected error: %v", err)
				return
			}

			if got.Name != "test" {
				t.Errorf("Load() name = %q, want %q", got.Name, "test")
			}
		})
	}
}

func TestManager_Save(t *testing.T) {
	tests := []struct {
		name        string
		manifest    *testManifest
		validator   Validator[*testManifest]
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid manifest",
			manifest:  &testManifest{Name: "test", Version: "1.0.0"},
			validator: &testValidator{},
			wantErr:   false,
		},
		{
			name:        "validation fails",
			manifest:    &testManifest{Name: "test"},
			validator:   &testValidator{shouldFail: true},
			wantErr:     true,
			errContains: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mgr := NewManager(dir, "test.yaml", tt.validator)

			err := mgr.Save(tt.manifest)

			if tt.wantErr {
				if err == nil {
					t.Error("Save() expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("Save() error = %v, want containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("Save() unexpected error: %v", err)
				return
			}

			// Verify file was written
			if !mgr.Exists() {
				t.Error("Save() file not created")
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
