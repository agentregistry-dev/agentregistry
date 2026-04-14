package declarative_test

import (
	"os"
	"path/filepath"
	"testing"

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

// writeDockerfile writes a minimal Dockerfile so docker build checks pass in tests.
func writeDockerfile(t *testing.T, projectDir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644))
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

// TestBuildCmd_AgentMissingDockerfile verifies a clear error when Dockerfile is absent.
func TestBuildCmd_AgentMissingDockerfile(t *testing.T) {
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
	// No Dockerfile written — should fail with a clear message.
	cmd := declarative.NewBuildCmd()
	cmd.SetArgs([]string{tmpDir})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dockerfile not found")
}

// TestBuildCmd_YAMLPrecedence verifies agent.yaml is preferred over mcp.yaml when both exist.
func TestBuildCmd_YAMLPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	// Write both files; agent.yaml should be found first.
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
	writeBuildYAML(t, tmpDir, "mcp.yaml", `
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: my-server
  version: 0.1.0
spec:
  title: my-server
  description: test server
`)
	// No Dockerfile — the error is agent-specific (dockerfile not found), confirming
	// agent.yaml was selected over mcp.yaml.
	cmd := declarative.NewBuildCmd()
	cmd.SetArgs([]string{tmpDir})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dockerfile not found")
}

// TestBuildCmd_AgentDockerNotAvailable verifies a clear error when Docker is not found.
// This test only runs when docker is not in PATH (CI environments without Docker).
func TestBuildCmd_AgentDockerNotAvailable(t *testing.T) {
	// Only meaningful in environments without Docker — skip if Docker is present.
	if isDockerAvailable() {
		t.Skip("Docker is available; skipping no-docker error test")
	}

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
	writeDockerfile(t, tmpDir)

	cmd := declarative.NewBuildCmd()
	cmd.SetArgs([]string{tmpDir})
	err := cmd.Execute()
	require.Error(t, err)
}

// isDockerAvailable checks if the docker CLI is present and daemon is reachable.
func isDockerAvailable() bool {
	cmd := declarative.CheckDockerAvailable()
	return cmd == nil
}
