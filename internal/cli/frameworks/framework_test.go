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

// TestParseDescriptor_BuiltinFastMCP_HasLaunchDefaults loads the
// vendored fastmcp-python framework.yaml and asserts launch defaults are
// populated so arctl init can emit them into stdio mcp.yamls.
func TestParseDescriptor_BuiltinFastMCP_HasLaunchDefaults(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("builtin", "fastmcp-python", "framework.yaml"))
	require.NoError(t, err)
	fw, err := ParseDescriptor(data)
	require.NoError(t, err)
	require.NotNil(t, fw.Launch)
	assert.Equal(t, "python", fw.Launch.Command)
	assert.Equal(t, []string{"src/main.py"}, fw.Launch.Args)

	args := fw.Launch.ToMCPArguments()
	require.Len(t, args, 1)
	assert.Equal(t, v1alpha1.MCPArgument{
		Type:  v1alpha1.MCPArgumentTypePositional,
		Value: "src/main.py",
	}, args[0])
}

// TestParseDescriptor_BuiltinMCPGo_HasLaunchDefaults confirms the Go
// framework declares a command (with no args — the binary takes no args
// in stdio mode) and that ToMCPArguments returns an empty slice rather
// than a nil receiver panic.
func TestParseDescriptor_BuiltinMCPGo_HasLaunchDefaults(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("builtin", "mcp-go", "framework.yaml"))
	require.NoError(t, err)
	fw, err := ParseDescriptor(data)
	require.NoError(t, err)
	require.NotNil(t, fw.Launch)
	assert.Equal(t, "/app/server", fw.Launch.Command)
	assert.Empty(t, fw.Launch.Args)

	args := fw.Launch.ToMCPArguments()
	assert.Empty(t, args)
}

// TestMCPLaunch_ToMCPArguments_NilReceiver guards the nil-safe path so
// callers can pass a framework's Launch field through unchecked.
func TestMCPLaunch_ToMCPArguments_NilReceiver(t *testing.T) {
	var l *MCPLaunch
	assert.Nil(t, l.ToMCPArguments())
}
