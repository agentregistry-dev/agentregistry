package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestMCPPackageLaunch_UnmarshalYAML_FlatListShorthand confirms hand-writers
// can use the `args: [a, b]` shorthand and get a positional MCPArgument per
// string element.
func TestMCPPackageLaunch_UnmarshalYAML_FlatListShorthand(t *testing.T) {
	in := []byte(`command: python
args: [src/main.py, "--debug"]
`)
	var l MCPPackageLaunch
	require.NoError(t, yaml.Unmarshal(in, &l))
	assert.Equal(t, "python", l.Command)
	require.Len(t, l.Args, 2)
	assert.Equal(t, MCPArgument{Type: MCPArgumentTypePositional, Value: "src/main.py"}, l.Args[0])
	assert.Equal(t, MCPArgument{Type: MCPArgumentTypePositional, Value: "--debug"}, l.Args[1])
}

// TestMCPPackageLaunch_UnmarshalYAML_StructuredForm confirms the canonical
// structured form (mapping per element) still parses, including a mix of
// positional and named arguments.
func TestMCPPackageLaunch_UnmarshalYAML_StructuredForm(t *testing.T) {
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

// TestMCPPackageLaunch_UnmarshalYAML_EmptyArgs confirms `args: []` leaves
// Args nil so it round-trips through omitempty without emitting `args: null`.
func TestMCPPackageLaunch_UnmarshalYAML_EmptyArgs(t *testing.T) {
	in := []byte(`command: /app/server
args: []
`)
	var l MCPPackageLaunch
	require.NoError(t, yaml.Unmarshal(in, &l))
	assert.Equal(t, "/app/server", l.Command)
	assert.Empty(t, l.Args)
}

// TestMCPPackageLaunch_UnmarshalYAML_RejectsNonStringInFlatList confirms a
// mixed list (scalar followed by mapping) is rejected with a clear error
// rather than silently producing a malformed Args slice.
func TestMCPPackageLaunch_UnmarshalYAML_RejectsNonStringInFlatList(t *testing.T) {
	in := []byte(`command: python
args:
  - "src/main.py"
  - {type: positional, value: oops}
`)
	var l MCPPackageLaunch
	err := yaml.Unmarshal(in, &l)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "launch.args")
}

// TestMCPPackageLaunch_UnmarshalYAML_NoArgsKey confirms a launch block
// without any `args:` key leaves Args nil.
func TestMCPPackageLaunch_UnmarshalYAML_NoArgsKey(t *testing.T) {
	in := []byte(`command: python
`)
	var l MCPPackageLaunch
	require.NoError(t, yaml.Unmarshal(in, &l))
	assert.Equal(t, "python", l.Command)
	assert.Nil(t, l.Args)
}
