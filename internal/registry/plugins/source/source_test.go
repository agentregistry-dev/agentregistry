package source

import (
	"context"
	"errors"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// TestGitResolverUnsupportedOrigins covers the terminal dispatch paths that do
// not touch the network: nil origin, OCI (not yet implemented), an unknown
// type, and a git origin missing its repository URL. Each must wrap
// ErrUnsupportedOrigin so the controller marks the plugin terminally failed
// rather than retrying forever.
func TestGitResolverUnsupportedOrigins(t *testing.T) {
	r := NewGitResolver()
	ctx := context.Background()

	tests := []struct {
		name   string
		plugin *v1alpha1.Plugin
	}{
		{"nil origin", &v1alpha1.Plugin{}},
		{
			name:   "oci origin not yet supported",
			plugin: &v1alpha1.Plugin{Spec: v1alpha1.PluginSpec{Origin: &v1alpha1.PluginOrigin{Type: v1alpha1.PluginOriginTypeOCI, OCI: &v1alpha1.PluginOriginOCI{Reference: "ghcr.io/o/p@sha256:abc"}}}},
		},
		{
			name:   "unknown origin type",
			plugin: &v1alpha1.Plugin{Spec: v1alpha1.PluginSpec{Origin: &v1alpha1.PluginOrigin{Type: "svn"}}},
		},
		{
			name:   "git origin missing url",
			plugin: &v1alpha1.Plugin{Spec: v1alpha1.PluginSpec{Origin: &v1alpha1.PluginOrigin{Type: v1alpha1.PluginOriginTypeGit, Git: &v1alpha1.PluginOriginGit{Repository: &v1alpha1.Repository{}}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := r.Resolve(ctx, tt.plugin)
			if !errors.Is(err, ErrUnsupportedOrigin) {
				t.Fatalf("expected ErrUnsupportedOrigin, got %v", err)
			}
		})
	}
}
