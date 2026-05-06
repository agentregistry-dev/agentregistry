package declarative_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/buildconfig"
	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// readYAMLFile parses a YAML file at the given absolute path and returns it as a map.
func readYAMLFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "YAML file should exist at %s", path)
	var m map[string]any
	require.NoError(t, yaml.Unmarshal(data, &m), "file should be valid YAML")
	return m
}

// ---- init agent ----

func TestInitAgent_WritesYAMLAndArctlAndEnvExample(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"agent", "myagent", "--framework", "adk", "--language", "python"})
	require.NoError(t, cmd.Execute())

	projectDir := filepath.Join(tmp, "myagent")

	// agent.yaml written
	_, err = os.Stat(filepath.Join(projectDir, "agent.yaml"))
	require.NoError(t, err)

	// arctl.yaml written with framework + language
	cfg, err := buildconfig.Read(projectDir)
	require.NoError(t, err)
	assert.Equal(t, "adk", cfg.Framework)
	assert.Equal(t, "python", cfg.Language)

	// .env.example written
	_, err = os.Stat(filepath.Join(projectDir, ".env.example"))
	assert.NoError(t, err)
}

// ---- init skill ----

func TestInitSkillCmd_BasicScaffold(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"skill", "myskill"})
	require.NoError(t, cmd.Execute())

	m := readYAMLFile(t, filepath.Join(tmpDir, "myskill", "skill.yaml"))
	assert.Equal(t, "ar.dev/v1alpha1", m["apiVersion"])
	assert.Equal(t, "Skill", m["kind"])

	metadata := m["metadata"].(map[string]any)
	assert.Equal(t, "myskill", metadata["name"])
	assert.Equal(t, "0.1.0", metadata["version"])

	spec := m["spec"].(map[string]any)
	assert.Equal(t, "myskill", spec["title"])
	assert.NotEmpty(t, spec["description"])
}

func TestInitSkillCmd_CustomFlags(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"skill", "myskill",
		"--version", "1.2.0",
		"--description", "Text summarizer",
	})
	require.NoError(t, cmd.Execute())

	m := readYAMLFile(t, filepath.Join(tmpDir, "myskill", "skill.yaml"))
	metadata := m["metadata"].(map[string]any)
	assert.Equal(t, "1.2.0", metadata["version"])

	spec := m["spec"].(map[string]any)
	assert.Equal(t, "Text summarizer", spec["description"])
}

func TestInitSkillCmd_ProjectFilesCreated(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"skill", "myskill"})
	require.NoError(t, cmd.Execute())

	_, err = os.Stat(filepath.Join(tmpDir, "myskill"))
	require.NoError(t, err, "project directory should be created")
	_, err = os.Stat(filepath.Join(tmpDir, "myskill", "skill.yaml"))
	require.NoError(t, err, "skill.yaml should exist")
}

// ---- init prompt ----

func TestInitPromptCmd_BasicScaffold(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"prompt", "myprompt"})
	require.NoError(t, cmd.Execute())

	// Prompt writes NAME.yaml in cwd, not a subdir
	m := readYAMLFile(t, filepath.Join(tmpDir, "myprompt.yaml"))
	assert.Equal(t, "ar.dev/v1alpha1", m["apiVersion"])
	assert.Equal(t, "Prompt", m["kind"])

	metadata := m["metadata"].(map[string]any)
	assert.Equal(t, "myprompt", metadata["name"])
	assert.Equal(t, "0.1.0", metadata["version"])

	spec := m["spec"].(map[string]any)
	assert.NotEmpty(t, spec["content"])
	assert.NotEmpty(t, spec["description"])
}

func TestInitPromptCmd_CustomContent(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"prompt", "summarizer",
		"--description", "Summarize text",
		"--content", "You are a text summarizer. Be concise.",
		"--version", "2.0.0",
	})
	require.NoError(t, cmd.Execute())

	m := readYAMLFile(t, filepath.Join(tmpDir, "summarizer.yaml"))
	metadata := m["metadata"].(map[string]any)
	assert.Equal(t, "2.0.0", metadata["version"])

	spec := m["spec"].(map[string]any)
	assert.Equal(t, "Summarize text", spec["description"])
	assert.Equal(t, "You are a text summarizer. Be concise.", spec["content"])
}

func TestInitPromptCmd_WritesFileNotDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"prompt", "myprompt"})
	require.NoError(t, cmd.Execute())

	// Must write myprompt.yaml in cwd, NOT create a directory
	info, err := os.Stat(filepath.Join(tmpDir, "myprompt.yaml"))
	require.NoError(t, err, "myprompt.yaml should exist")
	assert.False(t, info.IsDir(), "myprompt.yaml should be a file, not a directory")

	_, err = os.Stat(filepath.Join(tmpDir, "myprompt"))
	assert.True(t, os.IsNotExist(err), "no directory named myprompt should be created")
}
