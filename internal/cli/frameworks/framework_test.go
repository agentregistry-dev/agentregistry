package frameworks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestParseDescriptor_OK(t *testing.T) {
	yaml := []byte(`
apiVersion: arctl.dev/v1
name: adk-python
type: agent
framework: adk
language: python
description: Google Agent Development Kit
templatesDir: ./templates
env:
  required:
    - OPENAI_API_KEY
  optional:
    - LOG_LEVEL
build:
  command: ["docker", "build", "-t", "{{.Image}}", "."]
run:
  command: ["docker", "compose", "up"]
`)
	got, err := ParseDescriptor(yaml)
	require.NoError(t, err)
	assert.Equal(t, "arctl.dev/v1", got.APIVersion)
	assert.Equal(t, "adk-python", got.Name)
	assert.Equal(t, "agent", got.Type)
	assert.Equal(t, "adk", got.Framework)
	assert.Equal(t, "python", got.Language)
	assert.Equal(t, []string{"OPENAI_API_KEY"}, got.Env.Required)
	assert.Equal(t, []string{"LOG_LEVEL"}, got.Env.Optional)
	assert.Equal(t, []string{"docker", "build", "-t", "{{.Image}}", "."}, got.Build.Command)
	assert.Equal(t, []string{"docker", "compose", "up"}, got.Run.Command)
}

func TestParseDescriptor_RejectsUnsupportedAPIVersion(t *testing.T) {
	yaml := []byte(`apiVersion: arctl.dev/v99
name: foo
type: agent
framework: foo
language: bar
`)
	_, err := ParseDescriptor(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported apiVersion")
}

func TestParseDescriptor_RejectsMissingRequiredFields(t *testing.T) {
	yaml := []byte(`apiVersion: arctl.dev/v1
name: foo
type: agent
`) // missing framework, language
	_, err := ParseDescriptor(yaml)
	require.Error(t, err)
}

// TestParseDescriptor_RejectsLegacyLaunchShape asserts the pre-redesign
// `launch: {command, args}` shape is rejected with a clear error so
// external framework authors get a loud signal at parse time rather
// than silently producing a manifest with no Launch block.
func TestParseDescriptor_RejectsLegacyLaunchShape(t *testing.T) {
	yaml := []byte(`apiVersion: arctl.dev/v1
name: foo
type: agent
framework: foo
language: python
launch:
  command: python
  args: [src/main.py]
`)
	_, err := ParseDescriptor(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "launch.stdio or launch.http")
}

// TestParseDescriptor_BuiltinFastMCP_HasLaunchDefaults loads the
// vendored fastmcp-python framework.yaml and asserts both stdio and http
// launch defaults are populated so arctl init can emit the right block
// per --transport.
func TestParseDescriptor_BuiltinFastMCP_HasLaunchDefaults(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("builtin", "fastmcp-python", "framework.yaml"))
	require.NoError(t, err)
	fw, err := ParseDescriptor(data)
	require.NoError(t, err)
	require.NotNil(t, fw.Launch)
	require.NotNil(t, fw.Launch.Stdio)
	assert.Equal(t, "python", fw.Launch.Stdio.Command)
	assert.Equal(t, []string{"src/main.py"}, fw.Launch.Stdio.Args)

	require.NotNil(t, fw.Launch.HTTP)
	assert.Equal(t, "python", fw.Launch.HTTP.Command)
	assert.Equal(t, []string{
		"src/main.py", "--transport", "http", "--host", "0.0.0.0", "--port", "{{.Port}}",
	}, fw.Launch.HTTP.Args)
}

// TestParseDescriptor_BuiltinMCPGo_HasLaunchDefaults confirms the Go
// framework declares both transports — stdio is bare /app/server, http
// adds -http :{{.Port}}.
func TestParseDescriptor_BuiltinMCPGo_HasLaunchDefaults(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("builtin", "mcp-go", "framework.yaml"))
	require.NoError(t, err)
	fw, err := ParseDescriptor(data)
	require.NoError(t, err)
	require.NotNil(t, fw.Launch)
	require.NotNil(t, fw.Launch.Stdio)
	assert.Equal(t, "/app/server", fw.Launch.Stdio.Command)
	assert.Empty(t, fw.Launch.Stdio.Args)

	require.NotNil(t, fw.Launch.HTTP)
	assert.Equal(t, "/app/server", fw.Launch.HTTP.Command)
	assert.Equal(t, []string{"-http", ":{{.Port}}"}, fw.Launch.HTTP.Args)
}

// TestFrameworkLaunch_ForTransport covers the dispatch logic including
// the nil-receiver path so callers can pipe an unset Launch through.
func TestFrameworkLaunch_ForTransport(t *testing.T) {
	var nilLaunch *FrameworkLaunch
	assert.Nil(t, nilLaunch.ForTransport("stdio"))

	full := &FrameworkLaunch{
		Stdio: &MCPLaunch{Command: "s"},
		HTTP:  &MCPLaunch{Command: "h"},
	}
	assert.Equal(t, "s", full.ForTransport("stdio").Command)
	assert.Equal(t, "h", full.ForTransport("http").Command)
	assert.Nil(t, full.ForTransport("sse"))
	assert.Nil(t, full.ForTransport(""))

	stdioOnly := &FrameworkLaunch{Stdio: &MCPLaunch{Command: "s"}}
	assert.Nil(t, stdioOnly.ForTransport("http"))
}

// TestMCPLaunch_Render_TemplatesPort confirms {{.Port}} substitution
// matches the existing build/run command pattern.
func TestMCPLaunch_Render_TemplatesPort(t *testing.T) {
	l := &MCPLaunch{
		Command: "/app/server",
		Args:    []string{"-http", ":{{.Port}}"},
	}
	cmd, args, err := l.Render(8080)
	require.NoError(t, err)
	assert.Equal(t, "/app/server", cmd)
	assert.Equal(t, []string{"-http", ":8080"}, args)
}

// TestMCPLaunch_Render_NoTemplateVar passes args through verbatim when
// no template var appears.
func TestMCPLaunch_Render_NoTemplateVar(t *testing.T) {
	l := &MCPLaunch{Command: "python", Args: []string{"src/main.py"}}
	cmd, args, err := l.Render(3000)
	require.NoError(t, err)
	assert.Equal(t, "python", cmd)
	assert.Equal(t, []string{"src/main.py"}, args)
}

// TestMCPLaunch_Render_NilReceiver guards the nil-safe path so callers
// can pass a missing-transport launch through unchecked.
func TestMCPLaunch_Render_NilReceiver(t *testing.T) {
	var l *MCPLaunch
	cmd, args, err := l.Render(3000)
	require.NoError(t, err)
	assert.Equal(t, "", cmd)
	assert.Nil(t, args)
}

// TestToMCPArguments_Positional confirms the flat list converts into
// all-positional MCPArgument entries.
func TestToMCPArguments_Positional(t *testing.T) {
	args := ToMCPArguments([]string{"a", "b"})
	assert.Equal(t, []v1alpha1.MCPArgument{
		{Type: v1alpha1.MCPArgumentTypePositional, Value: "a"},
		{Type: v1alpha1.MCPArgumentTypePositional, Value: "b"},
	}, args)
}
