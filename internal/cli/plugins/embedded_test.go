package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LoadEmbedded returns whatever plugins are baked into the binary. With no plugins
// shipped yet, the result is empty (but the call must succeed).
func TestLoadEmbedded_EmptyOK(t *testing.T) {
	plugins, err := LoadEmbedded(t.TempDir())
	require.NoError(t, err)
	assert.NotNil(t, plugins) // empty slice or nil, both fine
}

func TestLoadEmbedded_FindsAdkPython(t *testing.T) {
	plugins, err := LoadEmbedded(t.TempDir())
	require.NoError(t, err)
	var found *Plugin
	for _, p := range plugins {
		if p.Name == "adk-python" {
			found = p
			break
		}
	}
	require.NotNil(t, found, "adk-python should be embedded")
	assert.Equal(t, "agent", found.Type)
	assert.Equal(t, "adk", found.Framework)
	assert.Equal(t, "python", found.Language)
}

func TestLoadEmbedded_FindsFastmcpPython(t *testing.T) {
	plugins, err := LoadEmbedded(t.TempDir())
	require.NoError(t, err)
	for _, p := range plugins {
		if p.Name == "fastmcp-python" {
			assert.Equal(t, "mcp", p.Type)
			assert.Equal(t, "fastmcp", p.Framework)
			assert.Equal(t, "python", p.Language)
			return
		}
	}
	t.Fatal("fastmcp-python not found among embedded plugins")
}
