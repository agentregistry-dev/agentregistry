package skill

import (
	"path/filepath"
	"testing"
)

func TestResolveProjectPath(t *testing.T) {
	tests := []struct {
		name        string
		projectName string
		extraArgs   []string
		wantSuffix  string
	}{
		{
			name:        "no output directory",
			projectName: "myskill",
			extraArgs:   nil,
			wantSuffix:  "myskill",
		},
		{
			name:        "with output directory",
			projectName: "myskill",
			extraArgs:   []string{"/tmp/skills"},
			wantSuffix:  filepath.Join("/tmp/skills", "myskill"),
		},
		{
			name:        "with relative output directory",
			projectName: "myskill",
			extraArgs:   []string{"./out"},
			wantSuffix:  filepath.Join("out", "myskill"),
		},
		{
			name:        "empty extraArgs slice",
			projectName: "myskill",
			extraArgs:   []string{},
			wantSuffix:  "myskill",
		},
		{
			name:        "empty string extraArg",
			projectName: "myskill",
			extraArgs:   []string{""},
			wantSuffix:  "myskill",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveProjectPath(tt.projectName, tt.extraArgs...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !filepath.IsAbs(got) {
				t.Errorf("expected absolute path, got %q", got)
			}
			if tt.extraArgs != nil && len(tt.extraArgs) > 0 && filepath.IsAbs(tt.extraArgs[0]) {
				// For absolute output dirs, check exact match
				if got != tt.wantSuffix {
					t.Errorf("got %q, want %q", got, tt.wantSuffix)
				}
			} else {
				// For relative or no output dir, check suffix
				if !hasSuffix(got, tt.wantSuffix) {
					t.Errorf("got %q, want path ending with %q", got, tt.wantSuffix)
				}
			}
		})
	}
}

func hasSuffix(path, suffix string) bool {
	cleanPath := filepath.Clean(path)
	cleanSuffix := filepath.Clean(suffix)
	return len(cleanPath) >= len(cleanSuffix) &&
		cleanPath[len(cleanPath)-len(cleanSuffix):] == cleanSuffix
}
