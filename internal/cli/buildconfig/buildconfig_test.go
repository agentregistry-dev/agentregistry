package buildconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteAndRead_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Framework: "adk", Language: "python"}
	require.NoError(t, Write(dir, cfg))

	got, err := Read(dir)
	require.NoError(t, err)
	assert.Equal(t, cfg.Framework, got.Framework)
	assert.Equal(t, cfg.Language, got.Language)
}

func TestRead_MissingFileErrs(t *testing.T) {
	_, err := Read(t.TempDir())
	require.Error(t, err)
}

func TestWrite_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Write(dir, &Config{Framework: "old", Language: "py"}))
	require.NoError(t, Write(dir, &Config{Framework: "new", Language: "py"}))
	got, err := Read(dir)
	require.NoError(t, err)
	assert.Equal(t, "new", got.Framework)
}

func TestPath(t *testing.T) {
	assert.Equal(t, filepath.Join("/proj", "arctl.yaml"), Path("/proj"))
}

func TestWriteEnvExample_RequiredAndOptional(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteEnvExample(dir, []string{"OPENAI_API_KEY"}, []string{"LOG_LEVEL"}))
	data, err := os.ReadFile(filepath.Join(dir, ".env.example"))
	require.NoError(t, err)
	got := string(data)
	assert.Contains(t, got, "OPENAI_API_KEY=")
	assert.Contains(t, got, "# Required")
	assert.Contains(t, got, "LOG_LEVEL=")
	assert.Contains(t, got, "# Optional")
}

func TestWriteEnvExample_NoneOmitsFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteEnvExample(dir, nil, nil))
	_, err := os.Stat(filepath.Join(dir, ".env.example"))
	assert.True(t, os.IsNotExist(err))
}
