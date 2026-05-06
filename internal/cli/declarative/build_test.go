package declarative_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/buildconfig"
	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeBuildYAML writes a declarative YAML fixture to a project directory.
func writeBuildYAML(t *testing.T, projectDir, filename, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, filename), []byte(content), 0o644))
}

// TestBuildCmd_NoDirectory verifies the command fails when the directory doesn't exist.
func TestBuildCmd_NoDirectory(t *testing.T) {
	cmd := declarative.NewBuildCmd()
	cmd.SetArgs([]string{"/tmp/nonexistent-declarative-build-dir-xyz"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestBuildCmd_FileInsteadOfDirectory verifies the command fails with a helpful error
// when a YAML file is passed instead of a project directory.
func TestBuildCmd_FileInsteadOfDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	yamlFile := filepath.Join(tmpDir, "my-prompt.yaml")
	require.NoError(t, os.WriteFile(yamlFile, []byte("apiVersion: ar.dev/v1alpha1\nkind: Prompt\n"), 0o644))

	cmd := declarative.NewBuildCmd()
	cmd.SetArgs([]string{yamlFile})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected a project directory, not a file")
}

// TestBuildCmd_SkillFileInsteadOfDirectory verifies the same helpful error for skill YAML files.
func TestBuildCmd_SkillFileInsteadOfDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	yamlFile := filepath.Join(tmpDir, "my-skill.yaml")
	require.NoError(t, os.WriteFile(yamlFile, []byte("apiVersion: ar.dev/v1alpha1\nkind: Skill\n"), 0o644))

	cmd := declarative.NewBuildCmd()
	cmd.SetArgs([]string{yamlFile})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected a project directory, not a file")
}

// TestBuildCmd_NoYAML verifies the command fails when no declarative YAML is present.
func TestBuildCmd_NoYAML(t *testing.T) {
	tmpDir := t.TempDir()
	cmd := declarative.NewBuildCmd()
	cmd.SetArgs([]string{tmpDir})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no declarative YAML found")
}

// TestBuildCmd_PromptKindError verifies that building a Prompt returns a helpful error.
func TestBuildCmd_PromptKindError(t *testing.T) {
	tmpDir := t.TempDir()
	writeBuildYAML(t, tmpDir, "prompt.yaml", `
apiVersion: ar.dev/v1alpha1
kind: Prompt
metadata:
  name: my-prompt
  version: 0.1.0
spec:
  description: A test prompt
  content: You are a helpful assistant.
`)

	cmd := declarative.NewBuildCmd()
	cmd.SetArgs([]string{tmpDir})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prompts have no build step")
}

// TestBuildCmd_UnknownKindError verifies that an unknown kind returns an error.
func TestBuildCmd_UnknownKindError(t *testing.T) {
	tmpDir := t.TempDir()
	writeBuildYAML(t, tmpDir, "agent.yaml", `
apiVersion: ar.dev/v1alpha1
kind: BogusKind
metadata:
  name: my-thing
  version: 0.1.0
spec:
  description: Unknown
`)

	cmd := declarative.NewBuildCmd()
	cmd.SetArgs([]string{tmpDir})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown kind")
}

// TestBuildCmd_AgentMissingArctlYAML verifies a clear error when arctl.yaml is missing.
// Build dispatches via the plugin registry, which requires arctl.yaml to identify the
// (framework, language) plugin to invoke.
func TestBuildCmd_AgentMissingArctlYAML(t *testing.T) {
	tmpDir := t.TempDir()
	writeBuildYAML(t, tmpDir, "agent.yaml", `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: my-agent
  version: 0.1.0
spec:
  image: localhost:5001/my-agent:latest
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
  description: test agent
`)
	cmd := declarative.NewBuildCmd()
	cmd.SetArgs([]string{tmpDir})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "arctl.yaml")
}

// TestBuildCmd_SkillKindError verifies that building a Skill returns a helpful error.
// Skills are metadata-only and have no build step.
func TestBuildCmd_SkillKindError(t *testing.T) {
	tmpDir := t.TempDir()
	writeBuildYAML(t, tmpDir, "skill.yaml", `
apiVersion: ar.dev/v1alpha1
kind: Skill
metadata:
  name: my-skill
  version: 0.1.0
spec:
  title: my-skill
  description: a test skill
`)

	cmd := declarative.NewBuildCmd()
	cmd.SetArgs([]string{tmpDir})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "skills have no build step")
}

// TestBuild_DispatchesViaPlugin verifies that arctl init writes a valid arctl.yaml
// that build's plugin-dispatch path can read. End-to-end docker invocation is not
// exercised here (no docker daemon assumption); the contract under test is that
// init's output is what build expects to consume.
func TestBuild_DispatchesViaPlugin(t *testing.T) {
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	require.NoError(t, os.Chdir(tmp))

	initCmd := declarative.NewInitCmd()
	initCmd.SetArgs([]string{"agent", "myagent", "--framework", "adk", "--language", "python"})
	require.NoError(t, initCmd.Execute())

	projectDir := filepath.Join(tmp, "myagent")
	cfg, err := buildconfig.Read(projectDir)
	require.NoError(t, err)
	assert.Equal(t, "adk", cfg.Framework)
	assert.Equal(t, "python", cfg.Language)
}
