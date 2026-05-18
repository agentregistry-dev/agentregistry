package declarative

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteMCPServersConfig_MergesEntries(t *testing.T) {
	dir := t.TempDir()
	// Seed an existing .env (simulating buildconfig.WriteDotEnv having run first).
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("FOO=bar\n"), 0o644))

	entries := []mcpEnvEntry{
		{Name: "acme/local", Type: "remote", URL: "http://host.docker.internal:3000/mcp"},
		{Name: "acme/fetch", Type: "remote", URL: "https://mcp.acme.com/mcp"},
	}
	require.NoError(t, writeMCPServersConfig(dir, entries))

	got, err := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, err)
	s := string(got)
	assert.Contains(t, s, "FOO=bar")
	assert.Equal(t, 1, strings.Count(s, "MCP_SERVERS_CONFIG="), "expected exactly one MCP_SERVERS_CONFIG line")
	assert.Contains(t, s, `"name":"acme/local"`)
	assert.Contains(t, s, `"name":"acme/fetch"`)
	assert.Contains(t, s, `"url":"https://mcp.acme.com/mcp"`)
}
