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
	cfg := &Config{
		Framework:     "adk",
		Language:      "python",
		ModelProvider: "openai",
		ModelName:     "gpt-4",
	}
	require.NoError(t, Write(dir, cfg))

	got, err := Read(dir)
	require.NoError(t, err)
	assert.Equal(t, cfg.Framework, got.Framework)
	assert.Equal(t, cfg.Language, got.Language)
	assert.Equal(t, cfg.ModelProvider, got.ModelProvider)
	assert.Equal(t, cfg.ModelName, got.ModelName)
}

func TestWriteAndRead_OmitsEmptyModelFields(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Write(dir, &Config{Framework: "fastmcp", Language: "python"}))

	data, err := os.ReadFile(filepath.Join(dir, "arctl.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(data), "modelProvider")
	assert.NotContains(t, string(data), "modelName")

	got, err := Read(dir)
	require.NoError(t, err)
	assert.Empty(t, got.ModelProvider)
	assert.Empty(t, got.ModelName)
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

func TestWriteDotEnv_RequiredAndOptional(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteDotEnv(dir, []string{"OPENAI_API_KEY"}, []string{"LOG_LEVEL"}))
	data, err := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, err)
	got := string(data)
	assert.Contains(t, got, "OPENAI_API_KEY=")
	assert.Contains(t, got, "# Required")
	assert.Contains(t, got, "LOG_LEVEL=")
	assert.Contains(t, got, "# Optional")
}

func TestWriteDotEnv_NoneOmitsFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, WriteDotEnv(dir, nil, nil))
	_, err := os.Stat(filepath.Join(dir, ".env"))
	assert.True(t, os.IsNotExist(err))
}

func TestEnsureGitignored_CreatesFileWhenMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, EnsureGitignored(dir, ".env"))
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, ".env\n", string(data))
}

func TestEnsureGitignored_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("dist/\nnode_modules/\n"), 0644))
	require.NoError(t, EnsureGitignored(dir, ".env"))
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "dist/\nnode_modules/\n.env\n", string(data))
}

func TestEnsureGitignored_AppendsTrailingNewlineWhenMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("dist/"), 0644))
	require.NoError(t, EnsureGitignored(dir, ".env"))
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, "dist/\n.env\n", string(data))
}

func TestEnsureGitignored_NoOpIfAlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	original := "dist/\n.env\nnode_modules/\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(original), 0644))
	require.NoError(t, EnsureGitignored(dir, ".env"))
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, original, string(data))
}

func TestEnsureGitignored_IgnoresWhitespaceWhenMatching(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("  .env  \n"), 0644))
	require.NoError(t, EnsureGitignored(dir, ".env"))
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	// Should not append a duplicate.
	assert.Equal(t, "  .env  \n", string(data))
}
