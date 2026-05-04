package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func TestDiscoverFromDir_LoadsPlugins(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "adk-python", "plugin.yaml"), `
apiVersion: arctl.dev/v1
name: adk-python
type: agent
framework: adk
language: python
`)

	plugins, err := DiscoverFromDir(root)
	require.NoError(t, err)
	require.Len(t, plugins, 1)
	assert.Equal(t, "adk-python", plugins[0].Name)
	assert.Equal(t, filepath.Join(root, "adk-python"), plugins[0].SourceDir)
}

func TestDiscoverFromDir_SkipsNonDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stray.txt"), "not a plugin")
	plugins, err := DiscoverFromDir(root)
	require.NoError(t, err)
	assert.Len(t, plugins, 0)
}

func TestDiscoverFromDir_MissingRoot(t *testing.T) {
	plugins, err := DiscoverFromDir("/nonexistent")
	require.NoError(t, err)
	assert.Len(t, plugins, 0)
}
