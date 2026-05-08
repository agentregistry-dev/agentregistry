package frameworks

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

func TestDiscoverFromDir_LoadsFrameworks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "adk-python", "framework.yaml"), `
apiVersion: arctl.dev/v1
name: adk-python
type: agent
framework: adk
language: python
`)

	frameworks, err := DiscoverFromDir(root)
	require.NoError(t, err)
	require.Len(t, frameworks, 1)
	assert.Equal(t, "adk-python", frameworks[0].Name)
	assert.Equal(t, filepath.Join(root, "adk-python"), frameworks[0].SourceDir)
}

func TestDiscoverFromDir_SkipsNonDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stray.txt"), "not a framework")
	frameworks, err := DiscoverFromDir(root)
	require.NoError(t, err)
	assert.Empty(t, frameworks)
}

func TestDiscoverFromDir_MissingRoot(t *testing.T) {
	frameworks, err := DiscoverFromDir("/nonexistent")
	require.NoError(t, err)
	assert.Empty(t, frameworks)
}

func TestUserFrameworksDir_RespectsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/cfg")
	t.Setenv("HOME", "/home/user")
	assert.Equal(t, "/custom/cfg/arctl/frameworks", UserFrameworksDir())
}

func TestUserFrameworksDir_FallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/user")
	assert.Equal(t, "/home/user/.config/arctl/frameworks", UserFrameworksDir())
}

func TestProjectFrameworksDir(t *testing.T) {
	assert.Equal(t, "/proj/.arctl/frameworks", ProjectFrameworksDir("/proj"))
}
