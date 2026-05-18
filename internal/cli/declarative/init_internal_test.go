package declarative

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
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

// fakeMCPFetcher implements mcpresolve.Fetcher for init-level tests.
type fakeMCPFetcher struct {
	servers map[string]*v1alpha1.MCPServer // key = "name@tag"
}

func (f *fakeMCPFetcher) Fetch(_ context.Context, name, tag string) (*v1alpha1.MCPServer, error) {
	key := name + "@" + tag
	if s, ok := f.servers[key]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("not found: %s", key)
}

func TestInitAgent_MCP_RemoteRef_WritesEnv(t *testing.T) {
	dir := t.TempDir()
	prev := mcpFetcherForTest
	mcpFetcherForTest = &fakeMCPFetcher{servers: map[string]*v1alpha1.MCPServer{
		"acme/fetch@latest": {
			Metadata: v1alpha1.ObjectMeta{Name: "acme/fetch", Tag: "latest"},
			Spec: v1alpha1.MCPServerSpec{
				Remote: &v1alpha1.MCPTransport{Type: "streamable-http", URL: "https://mcp.acme.com/mcp"},
			},
		},
	}}
	t.Cleanup(func() { mcpFetcherForTest = prev })

	cmd := NewInitCmd()
	cmd.SetArgs([]string{"agent", "myagent", "--framework", "adk", "--language", "python", "--mcp", "acme/fetch", "--output-dir", dir})
	require.NoError(t, cmd.Execute())

	pd := filepath.Join(dir, "myagent")
	env, err := os.ReadFile(filepath.Join(pd, ".env"))
	require.NoError(t, err)
	assert.Contains(t, string(env), `"name":"acme/fetch"`)
	assert.Contains(t, string(env), `"url":"https://mcp.acme.com/mcp"`)

	agentYAML, err := os.ReadFile(filepath.Join(pd, "agent.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(agentYAML), "name: acme/fetch")
}

func TestInitAgent_MCP_SourceRef_NoEnvWrite(t *testing.T) {
	dir := t.TempDir()
	prev := mcpFetcherForTest
	mcpFetcherForTest = &fakeMCPFetcher{servers: map[string]*v1alpha1.MCPServer{
		"acme/source@latest": {
			Metadata: v1alpha1.ObjectMeta{Name: "acme/source", Tag: "latest"},
			Spec:     v1alpha1.MCPServerSpec{Source: &v1alpha1.MCPServerSource{}},
		},
	}}
	t.Cleanup(func() { mcpFetcherForTest = prev })

	cmd := NewInitCmd()
	cmd.SetArgs([]string{"agent", "myagent", "--framework", "adk", "--language", "python", "--mcp", "acme/source", "--output-dir", dir})
	require.NoError(t, cmd.Execute())

	pd := filepath.Join(dir, "myagent")
	env, err := os.ReadFile(filepath.Join(pd, ".env"))
	require.NoError(t, err)
	assert.NotContains(t, string(env), "MCP_SERVERS_CONFIG", "Source-mode MCP must not populate MCP_SERVERS_CONFIG")
	agentYAML, err := os.ReadFile(filepath.Join(pd, "agent.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(agentYAML), "name: acme/source")
}

func TestInitAgent_MCP_RegistryFailure_NoPartialWrites(t *testing.T) {
	dir := t.TempDir()
	prev := mcpFetcherForTest
	mcpFetcherForTest = &fakeMCPFetcher{} // empty → all lookups fail
	t.Cleanup(func() { mcpFetcherForTest = prev })

	cmd := NewInitCmd()
	cmd.SetArgs([]string{"agent", "myagent", "--framework", "adk", "--language", "python", "--mcp", "acme/missing", "--output-dir", dir})
	require.Error(t, cmd.Execute())

	pd := filepath.Join(dir, "myagent")
	_, err := os.Stat(filepath.Join(pd, "agent.yaml"))
	assert.True(t, os.IsNotExist(err), "agent.yaml must not be written on registry failure")
	_, err = os.Stat(filepath.Join(pd, ".env"))
	assert.True(t, os.IsNotExist(err), ".env must not be written on registry failure")
}
