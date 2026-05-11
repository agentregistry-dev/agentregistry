package frameworks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAll_GathersFromAllSources(t *testing.T) {
	tmp := t.TempDir()
	stageDir := filepath.Join(tmp, "stage")
	userDir := filepath.Join(tmp, "user-frameworks")
	projDir := filepath.Join(tmp, "proj")

	// User-level framework
	require.NoError(t, os.MkdirAll(filepath.Join(userDir, "fake-user"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "fake-user", "framework.yaml"), []byte(`
apiVersion: arctl.dev/v1
name: fake-user
type: agent
framework: fake
language: a
`), 0644))

	// Project-local framework
	require.NoError(t, os.MkdirAll(filepath.Join(projDir, ".arctl", "frameworks", "fake-proj"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(projDir, ".arctl", "frameworks", "fake-proj", "framework.yaml"), []byte(`
apiVersion: arctl.dev/v1
name: fake-proj
type: agent
framework: fake
language: b
`), 0644))

	r, err := LoadAll(LoadOpts{StageDir: stageDir, UserDir: userDir, ProjectRoot: projDir})
	require.NoError(t, err)

	user, ok := r.Lookup("agent", "fake", "a")
	require.True(t, ok)
	assert.Equal(t, "fake-user", user.Name)

	proj, ok := r.Lookup("agent", "fake", "b")
	require.True(t, ok)
	assert.Equal(t, "fake-proj", proj.Name)
}
