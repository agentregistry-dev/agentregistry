package frameworks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LoadEmbedded returns whatever frameworks are baked into the binary. With no frameworks
// shipped yet, the result is empty (but the call must succeed).
func TestLoadEmbedded_EmptyOK(t *testing.T) {
	frameworks, err := LoadEmbedded(t.TempDir())
	require.NoError(t, err)
	assert.NotNil(t, frameworks) // empty slice or nil, both fine
}

func TestLoadEmbedded_FindsAdkPython(t *testing.T) {
	frameworks, err := LoadEmbedded(t.TempDir())
	require.NoError(t, err)
	var found *Framework
	for _, p := range frameworks {
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
	frameworks, err := LoadEmbedded(t.TempDir())
	require.NoError(t, err)
	for _, p := range frameworks {
		if p.Name == "fastmcp-python" {
			assert.Equal(t, "mcp", p.Type)
			assert.Equal(t, "fastmcp", p.Framework)
			assert.Equal(t, "python", p.Language)
			return
		}
	}
	t.Fatal("fastmcp-python not found among embedded frameworks")
}

func TestLoadEmbedded_FindsMcpGo(t *testing.T) {
	frameworks, err := LoadEmbedded(t.TempDir())
	require.NoError(t, err)
	for _, p := range frameworks {
		if p.Name == "mcp-go" {
			assert.Equal(t, "mcp", p.Type)
			assert.Equal(t, "mcp-go", p.Framework)
			assert.Equal(t, "go", p.Language)
			return
		}
	}
	t.Fatal("mcp-go not found among embedded frameworks")
}
