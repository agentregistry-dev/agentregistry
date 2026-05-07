package declarative

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDotEnv_OK(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("FOO=bar\nBAZ=qux\n"), 0644))
	env, err := LoadDotEnv(dir)
	require.NoError(t, err)
	assert.Equal(t, "bar", env["FOO"])
	assert.Equal(t, "qux", env["BAZ"])
}

func TestLoadDotEnv_Missing_ReturnsEmpty(t *testing.T) {
	env, err := LoadDotEnv(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, env)
}

func TestValidateRequiredEnv_AllPresent(t *testing.T) {
	require.NoError(t, ValidateRequiredEnv(map[string]string{"FOO": "bar"}, []string{"FOO"}))
}

func TestValidateRequiredEnv_MissingErrs(t *testing.T) {
	err := ValidateRequiredEnv(map[string]string{}, []string{"FOO", "BAR"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FOO")
	assert.Contains(t, err.Error(), "BAR")
}

// An empty placeholder in .env (the shape `arctl init` writes) is treated
// as missing — the user hasn't filled it in yet.
func TestValidateRequiredEnv_EmptyValueErrs(t *testing.T) {
	t.Setenv("FOO", "") // ensure process env doesn't satisfy it
	err := ValidateRequiredEnv(map[string]string{"FOO": ""}, []string{"FOO"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FOO")
}
