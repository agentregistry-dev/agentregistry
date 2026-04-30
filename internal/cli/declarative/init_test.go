package declarative_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// readAgentYAML parses the generated agent.yaml inside dir/name/ and returns it as a map.
func readAgentYAML(t *testing.T, dir, name string) map[string]any {
	t.Helper()
	return readYAMLFile(t, filepath.Join(dir, name, "agent.yaml"))
}

func TestInitAgentCmd_BasicScaffold(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"agent", "adk", "python", "myagent"})
	require.NoError(t, cmd.Execute())

	// agent.yaml must exist and have declarative format
	m := readAgentYAML(t, tmpDir, "myagent")
	assert.Equal(t, "ar.dev/v1alpha1", m["apiVersion"])
	assert.Equal(t, "Agent", m["kind"])

	metadata, ok := m["metadata"].(map[string]any)
	require.True(t, ok, "metadata should be a map")
	assert.Equal(t, "myagent", metadata["name"])
	assert.Equal(t, "0.1.0", metadata["version"])

	spec, ok := m["spec"].(map[string]any)
	require.True(t, ok, "spec should be a map")
	assert.Equal(t, "adk", spec["framework"])
	assert.Equal(t, "python", spec["language"])
	assert.Equal(t, "gemini", spec["modelProvider"])
	assert.Equal(t, "gemini-2.0-flash", spec["modelName"])
	assert.NotEmpty(t, spec["image"])
	assert.NotEmpty(t, spec["description"])
}

func TestInitAgentCmd_CustomFlags(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"agent", "adk", "python", "mybot",
		"--version", "2.0.0",
		"--description", "My custom bot",
		"--model-provider", "openai",
		"--model-name", "gpt-4o",
		"--image", "ghcr.io/acme/mybot:v2",
	})
	require.NoError(t, cmd.Execute())

	m := readAgentYAML(t, tmpDir, "mybot")
	metadata := m["metadata"].(map[string]any)
	assert.Equal(t, "2.0.0", metadata["version"])

	spec := m["spec"].(map[string]any)
	assert.Equal(t, "openai", spec["modelProvider"])
	assert.Equal(t, "gpt-4o", spec["modelName"])
	assert.Equal(t, "ghcr.io/acme/mybot:v2", spec["image"])
	assert.Equal(t, "My custom bot", spec["description"])
}

func TestInitAgentCmd_MCPSkillPromptRefs(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"agent", "adk", "python", "mybot",
		"--mcp", "acme/fetch@1.0.0",
		"--mcp", "myorg/weather",
		"--skill", "summarize@2.0.0",
		"--prompt", "system-prompt",
	})
	require.NoError(t, cmd.Execute())

	m := readAgentYAML(t, tmpDir, "mybot")
	spec := m["spec"].(map[string]any)

	mcps := spec["mcpServers"].([]any)
	require.Len(t, mcps, 2)
	mcp0 := mcps[0].(map[string]any)
	assert.Equal(t, "MCPServer", mcp0["kind"])
	assert.Equal(t, "acme/fetch", mcp0["name"])
	assert.Equal(t, "1.0.0", mcp0["version"])
	mcp1 := mcps[1].(map[string]any)
	assert.Equal(t, "MCPServer", mcp1["kind"])
	assert.Equal(t, "myorg/weather", mcp1["name"])
	assert.Equal(t, "latest", mcp1["version"])

	skills := spec["skills"].([]any)
	require.Len(t, skills, 1)
	skill0 := skills[0].(map[string]any)
	assert.Equal(t, "Skill", skill0["kind"])
	assert.Equal(t, "summarize", skill0["name"])
	assert.Equal(t, "2.0.0", skill0["version"])

	prompts := spec["prompts"].([]any)
	require.Len(t, prompts, 1)
	prompt0 := prompts[0].(map[string]any)
	assert.Equal(t, "Prompt", prompt0["kind"])
	assert.Equal(t, "system-prompt", prompt0["name"])
	assert.Equal(t, "latest", prompt0["version"])
}

func TestInitAgentCmd_GitRepository(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"agent", "adk", "python", "mybot",
		"--git", "https://github.com/acme/mybot",
	})
	require.NoError(t, cmd.Execute())

	m := readAgentYAML(t, tmpDir, "mybot")
	spec := m["spec"].(map[string]any)
	repo, ok := spec["repository"].(map[string]any)
	require.True(t, ok, "repository should be present in spec")
	assert.Equal(t, "https://github.com/acme/mybot", repo["url"])
}

func TestInitAgentCmd_NoGitRepository(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"agent", "adk", "python", "mybot"})
	require.NoError(t, cmd.Execute())

	m := readAgentYAML(t, tmpDir, "mybot")
	spec := m["spec"].(map[string]any)
	assert.NotContains(t, spec, "repository")
}

func TestInitAgentCmd_ModelProviderDefaultsModelName(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"agent", "adk", "python", "anthrobot", "--model-provider", "anthropic"})
	require.NoError(t, cmd.Execute())

	m := readAgentYAML(t, tmpDir, "anthrobot")
	spec := m["spec"].(map[string]any)
	assert.Equal(t, "anthropic", spec["modelProvider"])
	// When only --model-provider is set, model name should default to provider's default
	assert.Equal(t, "claude-3-5-sonnet", spec["modelName"])
}

func TestInitAgentCmd_SuccessMessage(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	var buf bytes.Buffer
	cmd := declarative.NewInitCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"agent", "adk", "python", "myagent"})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "✓ Successfully created agent: myagent")
	assert.Contains(t, out, "arctl apply -f agent.yaml")
}

func TestInitAgentCmd_InvalidName_Hyphen(t *testing.T) {
	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"agent", "adk", "python", "my-agent"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid agent name")
}

func TestInitAgentCmd_UnsupportedFramework(t *testing.T) {
	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"agent", "langchain", "python", "myagent"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported framework")
}

func TestInitAgentCmd_UnsupportedLanguage(t *testing.T) {
	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"agent", "adk", "javascript", "myagent"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported language")
}

func TestInitAgentCmd_InvalidModelProvider(t *testing.T) {
	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"agent", "adk", "python", "myagent", "--model-provider", "badprovider"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported model provider")
}

func TestInitAgentCmd_DefaultImageUsesRegistryName(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"agent", "adk", "python", "coolbot"})
	require.NoError(t, cmd.Execute())

	m := readAgentYAML(t, tmpDir, "coolbot")
	spec := m["spec"].(map[string]any)
	image, _ := spec["image"].(string)
	assert.True(t, strings.HasSuffix(image, "/coolbot:latest"),
		"default image should end with /<name>:latest, got: %s", image)
}

func TestInitAgentCmd_DeclarativeYAMLHasCorrectStructure(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"agent", "adk", "python", "structbot"})
	require.NoError(t, cmd.Execute())

	data, err := os.ReadFile(filepath.Join(tmpDir, "structbot", "agent.yaml"))
	require.NoError(t, err)
	content := string(data)

	// Must have declarative format fields
	assert.Contains(t, content, "apiVersion:")
	assert.Contains(t, content, "ar.dev/v1alpha1")
	assert.Contains(t, content, "kind: Agent")
	assert.Contains(t, content, "metadata:")
	assert.Contains(t, content, "spec:")
}

// ---- init mcp ----

func TestInitMCPCmd_BasicScaffold(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"mcp", "fastmcp-python", "myorg/myserver"})
	require.NoError(t, cmd.Execute())

	// Directory uses just the name part after "/"
	m := readYAMLFile(t, filepath.Join(tmpDir, "myserver", "mcp.yaml"))
	assert.Equal(t, "ar.dev/v1alpha1", m["apiVersion"])
	assert.Equal(t, "MCPServer", m["kind"])

	metadata := m["metadata"].(map[string]any)
	assert.Equal(t, "myorg/myserver", metadata["name"])
	assert.Equal(t, "0.1.0", metadata["version"])

	spec := m["spec"].(map[string]any)
	assert.Equal(t, "myserver", spec["title"])
	assert.NotEmpty(t, spec["description"])
	pkgs, ok := spec["packages"].([]any)
	require.True(t, ok, "spec.packages should be a list")
	require.Len(t, pkgs, 1)
	pkg := pkgs[0].(map[string]any)
	assert.Equal(t, "oci", pkg["registryType"])
	assert.NotEmpty(t, pkg["identifier"])
}

func TestInitMCPCmd_CustomFlags(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"mcp", "fastmcp-python", "myorg/myserver",
		"--version", "2.0.0",
		"--description", "My weather server",
		"--image", "ghcr.io/acme/myserver:v2",
	})
	require.NoError(t, cmd.Execute())

	m := readYAMLFile(t, filepath.Join(tmpDir, "myserver", "mcp.yaml"))
	metadata := m["metadata"].(map[string]any)
	assert.Equal(t, "myorg/myserver", metadata["name"])
	assert.Equal(t, "2.0.0", metadata["version"])

	spec := m["spec"].(map[string]any)
	assert.Equal(t, "My weather server", spec["description"])
	pkgs := spec["packages"].([]any)
	pkg := pkgs[0].(map[string]any)
	assert.Equal(t, "ghcr.io/acme/myserver:v2", pkg["identifier"])
}

func TestInitMCPCmd_DefaultImageUsesName(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"mcp", "fastmcp-python", "myorg/coolserver"})
	require.NoError(t, cmd.Execute())

	// Directory uses just the name part after "/"
	m := readYAMLFile(t, filepath.Join(tmpDir, "coolserver", "mcp.yaml"))
	spec := m["spec"].(map[string]any)
	pkgs := spec["packages"].([]any)
	pkg := pkgs[0].(map[string]any)
	identifier, _ := pkg["identifier"].(string)
	assert.True(t, strings.HasSuffix(identifier, "/coolserver:latest"),
		"default image should end with /<name>:latest, got: %s", identifier)
}

func TestInitMCPCmd_UnsupportedFramework(t *testing.T) {
	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"mcp", "typescript", "myorg/myserver"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported framework")
}

func TestInitMCPCmd_InvalidName_NoNamespace(t *testing.T) {
	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"mcp", "fastmcp-python", "myserver"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid MCP server name")
}

func TestInitMCPCmd_ProjectFilesCreated(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"mcp", "fastmcp-python", "myorg/myserver"})
	require.NoError(t, cmd.Execute())

	// Directory uses just the name part after "/"; YAML metadata.name uses full namespace/name
	_, err = os.Stat(filepath.Join(tmpDir, "myserver"))
	require.NoError(t, err, "project directory should be created (using name part only)")
	_, err = os.Stat(filepath.Join(tmpDir, "myserver", "mcp.yaml"))
	require.NoError(t, err, "mcp.yaml should exist")
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
