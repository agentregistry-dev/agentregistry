package pluginmarketplace_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/pluginmarketplace"
)

func readyCondition() v1alpha1.Status {
	var s v1alpha1.Status
	s.SetCondition(v1alpha1.Condition{Type: "Ready", Status: v1alpha1.ConditionTrue, Reason: "Resolved"})
	return s
}

func notReadyCondition() v1alpha1.Status {
	var s v1alpha1.Status
	s.SetCondition(v1alpha1.Condition{Type: "Ready", Status: v1alpha1.ConditionFalse, Reason: "Progressing"})
	return s
}

func TestFromPlugin_ReadyPlainGit(t *testing.T) {
	p := &v1alpha1.Plugin{
		Metadata: v1alpha1.ObjectMeta{Name: "code-formatter"},
		Spec: v1alpha1.PluginSpec{
			Description: "Formats code on save",
			Source: &v1alpha1.PluginSource{
				Type: v1alpha1.PluginSourceTypeGit,
				Git: &v1alpha1.PluginSourceGit{
					Repository: &v1alpha1.Repository{URL: "https://github.com/acme/code-formatter", Branch: "main"},
				},
			},
		},
		Status: v1alpha1.PluginStatus{
			Status:         readyCondition(),
			ResolvedSource: &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginSourceTypeGit, Commit: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"},
			Manifest:       &v1alpha1.PluginManifest{Name: "code-formatter", Version: "1.2.0", Description: "Formats code on save"},
		},
	}

	got, err := pluginmarketplace.FromPlugin(p)
	require.NoError(t, err)

	want := pluginmarketplace.PluginEntry{
		Name: "code-formatter",
		Source: pluginmarketplace.URLSource{
			Source: "url",
			URL:    "https://github.com/acme/code-formatter",
			SHA:    "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		},
		Description: "Formats code on save",
		Version:     "1.2.0",
	}
	assert.Equal(t, want, got)
}

func TestFromPlugin_MonorepoSubfolder(t *testing.T) {
	p := &v1alpha1.Plugin{
		Metadata: v1alpha1.ObjectMeta{Name: "deployment-tools"},
		Spec: v1alpha1.PluginSpec{
			Description: "Deployment helper commands",
			Source: &v1alpha1.PluginSource{
				Type: v1alpha1.PluginSourceTypeGit,
				Git: &v1alpha1.PluginSourceGit{
					Repository: &v1alpha1.Repository{URL: "https://github.com/acme/monorepo", Subfolder: "plugins/deployment-tools"},
				},
			},
		},
		Status: v1alpha1.PluginStatus{
			Status:         readyCondition(),
			ResolvedSource: &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginSourceTypeGit, Commit: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"},
		},
	}

	got, err := pluginmarketplace.FromPlugin(p)
	require.NoError(t, err)

	want := pluginmarketplace.PluginEntry{
		Name: "deployment-tools",
		Source: pluginmarketplace.GitSubdirSource{
			Source: "git-subdir",
			URL:    "https://github.com/acme/monorepo",
			Path:   "plugins/deployment-tools",
			SHA:    "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		},
		Description: "Deployment helper commands",
		Version:     "",
	}
	assert.Equal(t, want, got)
}

func TestFromPlugin_NotReady_Skipped(t *testing.T) {
	p := &v1alpha1.Plugin{
		Metadata: v1alpha1.ObjectMeta{Name: "not-yet-resolved"},
		Spec: v1alpha1.PluginSpec{
			Source: &v1alpha1.PluginSource{
				Type: v1alpha1.PluginSourceTypeGit,
				Git: &v1alpha1.PluginSourceGit{
					Repository: &v1alpha1.Repository{URL: "https://github.com/acme/still-resolving"},
				},
			},
		},
		Status: v1alpha1.PluginStatus{Status: notReadyCondition()},
	}

	_, err := pluginmarketplace.FromPlugin(p)
	assert.ErrorIs(t, err, pluginmarketplace.ErrNotResolved)
}

func TestFromPlugin_ReadyButNoResolvedSource_Skipped(t *testing.T) {
	p := &v1alpha1.Plugin{
		Metadata: v1alpha1.ObjectMeta{Name: "weird-state"},
		Spec: v1alpha1.PluginSpec{
			Source: &v1alpha1.PluginSource{
				Type: v1alpha1.PluginSourceTypeGit,
				Git: &v1alpha1.PluginSourceGit{
					Repository: &v1alpha1.Repository{URL: "https://github.com/acme/weird"},
				},
			},
		},
		Status: v1alpha1.PluginStatus{Status: readyCondition()},
	}

	_, err := pluginmarketplace.FromPlugin(p)
	assert.ErrorIs(t, err, pluginmarketplace.ErrNotResolved)
}

func TestFromPlugin_StaleObservedGeneration_Skipped(t *testing.T) {
	// Simulates a Spec edit (e.g. new source URL) landing on an
	// already-Ready Plugin: Status.Ready/ResolvedSource still reflect the
	// prior generation until the next reconcile finishes.
	p := &v1alpha1.Plugin{
		Metadata: v1alpha1.ObjectMeta{Name: "mid-reconcile", Generation: 2},
		Spec: v1alpha1.PluginSpec{
			Source: &v1alpha1.PluginSource{
				Type: v1alpha1.PluginSourceTypeGit,
				Git: &v1alpha1.PluginSourceGit{
					Repository: &v1alpha1.Repository{URL: "https://github.com/acme/new-url"},
				},
			},
		},
		Status: v1alpha1.PluginStatus{
			Status:         readyCondition(),
			ResolvedSource: &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginSourceTypeGit, Commit: "oldcommitoldcommitoldcommitoldcommitoldc"},
		},
	}
	p.Status.ObservedGeneration = 1

	_, err := pluginmarketplace.FromPlugin(p)
	assert.ErrorIs(t, err, pluginmarketplace.ErrNotResolved)
}

func TestFromPlugin_OCI_Skipped(t *testing.T) {
	p := &v1alpha1.Plugin{
		Metadata: v1alpha1.ObjectMeta{Name: "oci-plugin"},
		Spec: v1alpha1.PluginSpec{
			Source: &v1alpha1.PluginSource{
				Type: v1alpha1.PluginSourceTypeOCI,
				OCI:  &v1alpha1.PluginSourceOCI{Reference: "ghcr.io/acme/oci-plugin@sha256:deadbeef"},
			},
		},
		Status: v1alpha1.PluginStatus{
			Status:         readyCondition(),
			ResolvedSource: &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginSourceTypeOCI, Digest: "sha256:deadbeef"},
		},
	}

	_, err := pluginmarketplace.FromPlugin(p)
	assert.ErrorIs(t, err, pluginmarketplace.ErrUnsupportedSource)
}

func TestFromPlugin_DescriptionFallsBackToSpec(t *testing.T) {
	p := &v1alpha1.Plugin{
		Metadata: v1alpha1.ObjectMeta{Name: "no-manifest-yet"},
		Spec: v1alpha1.PluginSpec{
			Description: "spec-level description",
			Source: &v1alpha1.PluginSource{
				Type: v1alpha1.PluginSourceTypeGit,
				Git: &v1alpha1.PluginSourceGit{
					Repository: &v1alpha1.Repository{URL: "https://github.com/acme/no-manifest"},
				},
			},
		},
		Status: v1alpha1.PluginStatus{
			Status:         readyCondition(),
			ResolvedSource: &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginSourceTypeGit, Commit: "cafebabecafebabecafebabecafebabecafebabe"},
		},
	}

	got, err := pluginmarketplace.FromPlugin(p)
	require.NoError(t, err)
	assert.Equal(t, "spec-level description", got.Description)
	assert.Empty(t, got.Version)
}

func TestFromPlugin_ManifestDescriptionOverridesSpec(t *testing.T) {
	p := &v1alpha1.Plugin{
		Metadata: v1alpha1.ObjectMeta{Name: "has-manifest"},
		Spec: v1alpha1.PluginSpec{
			Description: "spec-level description",
			Source: &v1alpha1.PluginSource{
				Type: v1alpha1.PluginSourceTypeGit,
				Git: &v1alpha1.PluginSourceGit{
					Repository: &v1alpha1.Repository{URL: "https://github.com/acme/has-manifest"},
				},
			},
		},
		Status: v1alpha1.PluginStatus{
			Status:         readyCondition(),
			ResolvedSource: &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginSourceTypeGit, Commit: "0123456789abcdef0123456789abcdef01234567"},
			Manifest:       &v1alpha1.PluginManifest{Name: "has-manifest", Version: "2.0.0", Description: "manifest-level description"},
		},
	}

	got, err := pluginmarketplace.FromPlugin(p)
	require.NoError(t, err)
	assert.Equal(t, "manifest-level description", got.Description)
	assert.Equal(t, "2.0.0", got.Version)
}
