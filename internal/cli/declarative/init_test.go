package declarative_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"github.com/agentregistry-dev/agentregistry/internal/cli/buildconfig"
	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
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

func TestInitAgent_WritesYAMLAndArctlAndDotEnv(t *testing.T) {
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

	// arctl.yaml written with framework + language + default model fields
	cfg, err := buildconfig.Read(projectDir)
	require.NoError(t, err)
	assert.Equal(t, "adk", cfg.Framework)
	assert.Equal(t, "python", cfg.Language)
	assert.Equal(t, "gemini", cfg.ModelProvider)

	// .env written directly (no cp step needed)
	_, err = os.Stat(filepath.Join(projectDir, ".env"))
	require.NoError(t, err)

	// .env should be gitignored so secrets aren't accidentally committed
	gi, err := os.ReadFile(filepath.Join(projectDir, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(gi), ".env")
}

// TestInitAgent_MCPToolsHasNamePrefix asserts the scaffolded mcp_tools.py
// constructs each MCPToolset with tool_name_prefix set. Without this, two
// MCP servers exposing the same tool name (e.g. `echo`) merge into a single
// flat list and the LLM API rejects with "tools: Tool names must be
// unique." See google-adk's MCPToolset tool_name_prefix parameter.
func TestInitAgent_MCPToolsHasNamePrefix(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"agent", "myagent", "--framework", "adk", "--language", "python"})
	require.NoError(t, cmd.Execute())

	body, err := os.ReadFile(filepath.Join(tmp, "myagent", "myagent", "mcp_tools.py"))
	require.NoError(t, err)
	src := string(body)

	assert.Contains(t, src, "tool_name_prefix",
		"mcp_tools.py must pass tool_name_prefix to MCPToolset; without it, two MCPs exposing the same tool name collide at the LLM API")
}

func TestInitAgent_OutputDirFlag(t *testing.T) {
	tmp := t.TempDir()
	out := t.TempDir() // separate from cwd
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"agent", "outdirbot",
		"--framework", "adk", "--language", "python",
		"--output-dir", out,
	})
	require.NoError(t, cmd.Execute())

	// Project lands under --output-dir, not cwd.
	_, err = os.Stat(filepath.Join(out, "outdirbot", "arctl.yaml"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(tmp, "outdirbot"))
	assert.True(t, os.IsNotExist(err), "project should NOT be in cwd")
}

func TestInitAgent_ModelProviderFlagFlowsToArctlYAML(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"agent", "openaibot",
		"--framework", "adk", "--language", "python",
		"--model-provider", "openai",
		"--model-name", "gpt4",
	})
	require.NoError(t, cmd.Execute())

	projectDir := filepath.Join(tmp, "openaibot")

	cfg, err := buildconfig.Read(projectDir)
	require.NoError(t, err)
	assert.Equal(t, "openai", cfg.ModelProvider)
	assert.Equal(t, "gpt4", cfg.ModelName)

	// agent.yaml still mirrors model fields for the registry side
	spec := readYAMLFile(t, filepath.Join(projectDir, "agent.yaml"))["spec"].(map[string]any)
	assert.Equal(t, "openai", spec["modelProvider"])
	assert.Equal(t, "gpt4", spec["modelName"])
}

// ---- init mcp ----

func TestInitMCP_RejectsNonDNSSubdomainName(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"mcp", "acme/my-mcp", "--framework", "fastmcp", "--language", "python", "--transport", "http"})
	require.Error(t, cmd.Execute())
}

func TestInitMCP_WritesYAMLAndArctl(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"mcp", "my-mcp", "--framework", "fastmcp", "--language", "python", "--transport", "http"})
	require.NoError(t, cmd.Execute())

	projectDir := filepath.Join(tmp, "my-mcp")
	_, err = os.Stat(filepath.Join(projectDir, "mcp.yaml"))
	require.NoError(t, err)

	// The generated manifest declares the http transport matching the
	// scaffolded fastmcp server (default --port 3000, path /mcp) so it's
	// deployable as-is.
	mcpSpec := readYAMLFile(t, filepath.Join(projectDir, "mcp.yaml"))["spec"].(map[string]any)
	pkg := mcpSpec["source"].(map[string]any)["package"].(map[string]any)
	transport := pkg["transport"].(map[string]any)
	assert.Equal(t, "http", transport["type"])
	assert.EqualValues(t, 3000, transport["port"])
	assert.Equal(t, "/mcp", transport["path"])

	cfg, err := buildconfig.Read(projectDir)
	require.NoError(t, err)
	assert.Equal(t, "fastmcp", cfg.Framework)
}

// TestInitMCP_StdioTransport runs the command with --transport stdio and
// asserts the manifest declares stdio (no port/path) while the OCI origin
// and arctl.yaml are intact — the runtime/framework binary handles the
// stdio loop, so no template changes are needed.
func TestInitMCP_StdioTransport(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"mcp", "my-stdio-mcp",
		"--framework", "fastmcp", "--language", "python",
		"--transport", "stdio",
	})
	require.NoError(t, cmd.Execute())

	projectDir := filepath.Join(tmp, "my-stdio-mcp")
	_, err = os.Stat(filepath.Join(projectDir, "mcp.yaml"))
	require.NoError(t, err)

	mcpSpec := readYAMLFile(t, filepath.Join(projectDir, "mcp.yaml"))["spec"].(map[string]any)
	pkg := mcpSpec["source"].(map[string]any)["package"].(map[string]any)
	transport := pkg["transport"].(map[string]any)
	assert.Equal(t, "stdio", transport["type"])
	// port/path use omitempty in the API type, so a stdio transport must
	// not emit them.
	assert.NotContains(t, transport, "port", "stdio transport should not emit port")
	assert.NotContains(t, transport, "path", "stdio transport should not emit path")

	// Origin block intact: type=oci, identifier=image, oci.serverName=name.
	origin := pkg["origin"].(map[string]any)
	assert.Equal(t, "oci", origin["type"])
	assert.NotEmpty(t, origin["identifier"])
	oci := origin["oci"].(map[string]any)
	assert.Equal(t, "my-stdio-mcp", oci["serverName"])

	// arctl.yaml unchanged by transport choice — framework/language still
	// land in the build config the same way.
	cfg, err := buildconfig.Read(projectDir)
	require.NoError(t, err)
	assert.Equal(t, "fastmcp", cfg.Framework)
	assert.Equal(t, "python", cfg.Language)
}

// TestInitMCP_StdioTransport_OmitsPortFromArctlYAML asserts the scaffolded
// arctl.yaml has no `port` field for stdio projects. The cobra default
// (3000) used to leak into arctl.yaml regardless of transport, which made
// `arctl run` and --local-mcp wiring point at a non-existent HTTP server.
func TestInitMCP_StdioTransport_OmitsPortFromArctlYAML(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"mcp", "my-stdio-mcp",
		"--framework", "fastmcp", "--language", "python",
		"--transport", "stdio",
	})
	require.NoError(t, cmd.Execute())

	cfg, err := buildconfig.Read(filepath.Join(tmp, "my-stdio-mcp"))
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.Port, "stdio scaffold must not write a port (HTTP-only semantic)")
}

// TestInitMCP_StdioTransport_WritesLaunchFromFramework asserts that for
// stdio transport the framework's launch defaults land in
// spec.source.package.launch in structured form so the runtime has a
// non-empty Cmd at deploy time.
func TestInitMCP_StdioTransport_WritesLaunchFromFramework(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"mcp", "my-stdio-mcp",
		"--framework", "fastmcp", "--language", "python",
		"--transport", "stdio",
	})
	require.NoError(t, cmd.Execute())

	projectDir := filepath.Join(tmp, "my-stdio-mcp")
	mcpSpec := readYAMLFile(t, filepath.Join(projectDir, "mcp.yaml"))["spec"].(map[string]any)
	pkg := mcpSpec["source"].(map[string]any)["package"].(map[string]any)

	launch, ok := pkg["launch"].(map[string]any)
	require.True(t, ok, "stdio transport must scaffold spec.source.package.launch")
	assert.Equal(t, "python3", launch["command"])

	args, ok := launch["args"].([]any)
	require.True(t, ok, "launch.args must be a list")
	require.Len(t, args, 3)
	values := make([]string, 0, len(args))
	for _, a := range args {
		entry := a.(map[string]any)
		assert.Equal(t, "positional", entry["type"])
		values = append(values, entry["value"].(string))
	}
	assert.Equal(t, []string{"src/main.py", "--transport", "stdio"}, values)
}

// TestInitMCP_HTTPTransport_WritesLaunchFromFramework asserts that http
// transport gets a launch block populated from the framework's http
// defaults — so the deployed container runs with the right flags
// (--transport http --host 0.0.0.0 --port N for fastmcp).
func TestInitMCP_HTTPTransport_WritesLaunchFromFramework(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"mcp", "my-http-mcp",
		"--framework", "fastmcp", "--language", "python",
		"--transport", "http",
		"--port", "4321",
	})
	require.NoError(t, cmd.Execute())

	projectDir := filepath.Join(tmp, "my-http-mcp")
	mcpSpec := readYAMLFile(t, filepath.Join(projectDir, "mcp.yaml"))["spec"].(map[string]any)
	pkg := mcpSpec["source"].(map[string]any)["package"].(map[string]any)

	launch, ok := pkg["launch"].(map[string]any)
	require.True(t, ok, "http transport must scaffold spec.source.package.launch")
	assert.Equal(t, "python3", launch["command"])

	args, ok := launch["args"].([]any)
	require.True(t, ok, "launch.args must be a list")
	values := make([]string, 0, len(args))
	for _, a := range args {
		item := a.(map[string]any)
		assert.Equal(t, "positional", item["type"])
		values = append(values, item["value"].(string))
	}
	assert.Equal(t, []string{
		"src/main.py", "--transport", "http", "--host", "0.0.0.0", "--port", "4321",
	}, values, "http launch.args must carry --transport/--host/--port with the user's --port substituted")
}

// TestInitMCP_PortIncompatibleWithStdio rejects --port + --transport stdio
// up front so the user doesn't get a manifest with a port that the runtime
// will ignore.
func TestInitMCP_PortIncompatibleWithStdio(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"mcp", "my-mcp",
		"--framework", "fastmcp", "--language", "python",
		"--transport", "stdio",
		"--port", "4000",
	})
	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--port")
}

// TestInitMCP_InvalidTransport rejects unknown --transport values rather
// than silently falling back to a default.
func TestInitMCP_InvalidTransport(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{
		"mcp", "my-mcp",
		"--framework", "fastmcp", "--language", "python",
		"--transport", "sse",
	})
	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--transport")
}

// ---- init skill ----

func TestInitSkill_StillWorks(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"skill", "my-skill"})
	require.NoError(t, cmd.Execute())

	_, err = os.Stat(filepath.Join(tmp, "my-skill", "skill.yaml"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(tmp, "my-skill", "SKILL.md"))
	require.NoError(t, err)
}

func TestInitPrompt_StillWorks(t *testing.T) {
	tmp := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := declarative.NewInitCmd()
	cmd.SetArgs([]string{"prompt", "my-prompt"})
	require.NoError(t, cmd.Execute())

	_, err = os.Stat(filepath.Join(tmp, "my-prompt.yaml"))
	require.NoError(t, err)
}

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
	// metadata.tag is intentionally omitted from scaffolded YAML; server
	// fills with literal "latest" on apply.
	assert.NotContains(t, metadata, "tag")

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
		"--description", "Text summarizer",
	})
	require.NoError(t, cmd.Execute())

	m := readYAMLFile(t, filepath.Join(tmpDir, "myskill", "skill.yaml"))
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
	assert.NotContains(t, metadata, "tag")

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
	})
	require.NoError(t, cmd.Execute())

	m := readYAMLFile(t, filepath.Join(tmpDir, "summarizer.yaml"))
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
