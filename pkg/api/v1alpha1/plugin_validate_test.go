package v1alpha1

import (
	"strings"
	"testing"
)

func basePluginMeta() ObjectMeta {
	return ObjectMeta{Namespace: "default", Name: "my-plugin", Tag: "v1"}
}

func TestPluginValidate(t *testing.T) {
	fullSHA := strings.Repeat("a1b2c3d4", 5) // 40 hex chars
	gitPinned := &PluginSource{
		Type: PluginSourceTypeGit,
		Git:  &PluginSourceGit{Repository: &Repository{URL: "https://github.com/org/repo", Commit: fullSHA}},
	}

	tests := []struct {
		name    string
		spec    PluginSpec
		wantErr string // substring; empty means valid
	}{
		{
			name: "valid git source",
			spec: PluginSpec{Title: "My Plugin", Harnesses: []string{"claude-code"}, Source: gitPinned},
		},
		{
			name: "valid oci digest source",
			spec: PluginSpec{Source: &PluginSource{Type: PluginSourceTypeOCI, OCI: &PluginSourceOCI{Reference: "ghcr.io/org/plugin@sha256:" + strings.Repeat("a", 64)}}},
		},
		{
			name:    "missing source",
			spec:    PluginSpec{Title: "x"},
			wantErr: "spec.source",
		},
		{
			name: "git source with branch only (controller resolves the commit)",
			spec: PluginSpec{Source: &PluginSource{Type: PluginSourceTypeGit, Git: &PluginSourceGit{Repository: &Repository{URL: "https://github.com/org/repo", Branch: "main"}}}},
		},
		{
			name: "git source with no ref (controller resolves default branch)",
			spec: PluginSpec{Source: &PluginSource{Type: PluginSourceTypeGit, Git: &PluginSourceGit{Repository: &Repository{URL: "https://github.com/org/repo"}}}},
		},
		{
			name:    "git source missing url",
			spec:    PluginSpec{Source: &PluginSource{Type: PluginSourceTypeGit, Git: &PluginSourceGit{Repository: &Repository{Commit: fullSHA}}}},
			wantErr: "url",
		},
		{
			name:    "git commit not a full SHA (would never resolve)",
			spec:    PluginSpec{Source: &PluginSource{Type: PluginSourceTypeGit, Git: &PluginSourceGit{Repository: &Repository{URL: "https://github.com/org/repo", Commit: "abc123"}}}},
			wantErr: "full 40-character SHA",
		},
		{
			name:    "git branch and commit both set (ambiguous)",
			spec:    PluginSpec{Source: &PluginSource{Type: PluginSourceTypeGit, Git: &PluginSourceGit{Repository: &Repository{URL: "https://github.com/org/repo", Branch: "main", Commit: fullSHA}}}},
			wantErr: "at most one of branch or commit",
		},
		{
			name:    "oci source not digest-pinned",
			spec:    PluginSpec{Source: &PluginSource{Type: PluginSourceTypeOCI, OCI: &PluginSourceOCI{Reference: "ghcr.io/org/plugin:latest"}}},
			wantErr: "digest-pinned",
		},
		{
			name:    "unknown source type",
			spec:    PluginSpec{Source: &PluginSource{Type: "svn"}},
			wantErr: "unknown plugin source type",
		},
		{
			name:    "git and oci both set",
			spec:    PluginSpec{Source: &PluginSource{Type: PluginSourceTypeGit, Git: &PluginSourceGit{Repository: &Repository{URL: "https://github.com/org/repo", Commit: "abc"}}, OCI: &PluginSourceOCI{Reference: "x"}}},
			wantErr: "oci must be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plugin{
				TypeMeta: TypeMeta{APIVersion: GroupVersion, Kind: KindPlugin},
				Metadata: basePluginMeta(),
				Spec:     tt.spec,
			}
			err := p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected valid, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}
