package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Regression coverage for default yaml decoding of MCPPackageLaunch/MCPArgument.
// The shorthand decoder was dropped; these tests lock the structured wire
// shape against accidental tag drift on the struct fields.

func TestMCPPackageLaunch_DefaultDecode_StructuredForm(t *testing.T) {
	in := []byte(`command: python
args:
  - type: positional
    value: src/main.py
  - type: named
    name: --port
    value: "3000"
`)
	var l MCPPackageLaunch
	require.NoError(t, yaml.Unmarshal(in, &l))
	assert.Equal(t, "python", l.Command)
	require.Len(t, l.Args, 2)
	assert.Equal(t, MCPArgument{Type: MCPArgumentTypePositional, Value: "src/main.py"}, l.Args[0])
	assert.Equal(t, MCPArgument{Type: MCPArgumentTypeNamed, Name: "--port", Value: "3000"}, l.Args[1])
}

func TestMCPPackageLaunch_DefaultDecode_EmptyArgs(t *testing.T) {
	in := []byte(`command: /app/server
args: []
`)
	var l MCPPackageLaunch
	require.NoError(t, yaml.Unmarshal(in, &l))
	assert.Equal(t, "/app/server", l.Command)
	assert.Empty(t, l.Args)
}

func TestMCPPackageLaunch_DefaultDecode_NoArgsKey(t *testing.T) {
	in := []byte(`command: python
`)
	var l MCPPackageLaunch
	require.NoError(t, yaml.Unmarshal(in, &l))
	assert.Equal(t, "python", l.Command)
	assert.Nil(t, l.Args)
}
