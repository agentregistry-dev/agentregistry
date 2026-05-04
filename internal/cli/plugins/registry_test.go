package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_AddAndLookup(t *testing.T) {
	r := NewRegistry()
	p := &Plugin{Name: "adk-python", Type: "agent", Framework: "adk", Language: "python"}
	require.NoError(t, r.Add(p, SourceInTree))

	got, ok := r.Lookup("agent", "adk", "python")
	require.True(t, ok)
	assert.Same(t, p, got)
}

func TestRegistry_LookupMiss(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Lookup("agent", "nope", "python")
	assert.False(t, ok)
}

func TestRegistry_ConflictInTreeWins(t *testing.T) {
	r := NewRegistry()
	inTree := &Plugin{Name: "adk-python", Type: "agent", Framework: "adk", Language: "python"}
	require.NoError(t, r.Add(inTree, SourceInTree))
	outOfTree := &Plugin{Name: "adk-python-fork", Type: "agent", Framework: "adk", Language: "python"}
	// Out-of-tree fights for the same key — must lose, but no error.
	require.NoError(t, r.Add(outOfTree, SourceUserHome))

	got, _ := r.Lookup("agent", "adk", "python")
	assert.Same(t, inTree, got)
	require.Len(t, r.Conflicts(), 1)
}

func TestRegistry_ListByType(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Add(&Plugin{Name: "adk-python", Type: "agent", Framework: "adk", Language: "python"}, SourceInTree))
	require.NoError(t, r.Add(&Plugin{Name: "fastmcp-python", Type: "mcp", Framework: "fastmcp", Language: "python"}, SourceInTree))

	agents := r.ListByType("agent")
	assert.Len(t, agents, 1)
	mcps := r.ListByType("mcp")
	assert.Len(t, mcps, 1)
}
