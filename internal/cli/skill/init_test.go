package skill

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProjectPath(t *testing.T) {
	tests := []struct {
		name        string
		projectName string
		outputDir   string
		wantSuffix  string
	}{
		{
			name:        "empty output directory",
			projectName: "myskill",
			outputDir:   "",
			wantSuffix:  "myskill",
		},
		{
			name:        "with output directory",
			projectName: "myskill",
			outputDir:   "/tmp/skills",
			wantSuffix:  filepath.Join("/tmp/skills", "myskill"),
		},
		{
			name:        "with relative output directory",
			projectName: "myskill",
			outputDir:   "./out",
			wantSuffix:  filepath.Join("out", "myskill"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveProjectPath(tt.projectName, tt.outputDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !filepath.IsAbs(got) {
				t.Errorf("expected absolute path, got %q", got)
			}

			isAbsolute := tt.outputDir != "" && filepath.IsAbs(tt.outputDir)
			if isAbsolute {
				// For absolute output dirs, check exact match
				if got != tt.wantSuffix {
					t.Errorf("got %q, want %q", got, tt.wantSuffix)
				}
			} else {
				// For relative or no output dir, check suffix
				cleanGot := filepath.Clean(got)
				cleanWant := filepath.Clean(tt.wantSuffix)
				if !strings.HasSuffix(cleanGot, cleanWant) {
					t.Errorf("got %q, want path ending with %q", got, tt.wantSuffix)
				}
			}
		})
	}
}
