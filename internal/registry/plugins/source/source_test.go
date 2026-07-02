package source

import (
	"context"
	"errors"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// TestGitResolverUnsupportedSources covers the terminal dispatch paths that do
// not touch the network: nil source, OCI (not yet implemented), an unknown
// type, and a git source missing its repository URL. Each must wrap
// ErrUnsupportedSource so the controller marks the plugin terminally failed
// rather than retrying forever.
func TestGitResolverUnsupportedSources(t *testing.T) {
	r := NewGitResolver()
	ctx := context.Background()

	tests := []struct {
		name   string
		plugin *v1alpha1.Plugin
	}{
		{"nil source", &v1alpha1.Plugin{}},
		{
			name:   "oci source not yet supported",
			plugin: &v1alpha1.Plugin{Spec: v1alpha1.PluginSpec{Source: &v1alpha1.PluginSource{Type: v1alpha1.PluginSourceTypeOCI, OCI: &v1alpha1.PluginSourceOCI{Reference: "ghcr.io/o/p@sha256:abc"}}}},
		},
		{
			name:   "unknown source type",
			plugin: &v1alpha1.Plugin{Spec: v1alpha1.PluginSpec{Source: &v1alpha1.PluginSource{Type: "svn"}}},
		},
		{
			name:   "git source missing url",
			plugin: &v1alpha1.Plugin{Spec: v1alpha1.PluginSpec{Source: &v1alpha1.PluginSource{Type: v1alpha1.PluginSourceTypeGit, Git: &v1alpha1.PluginSourceGit{Repository: &v1alpha1.Repository{}}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := r.Resolve(ctx, tt.plugin)
			if !errors.Is(err, ErrUnsupportedSource) {
				t.Fatalf("expected ErrUnsupportedSource, got %v", err)
			}
		})
	}
}
