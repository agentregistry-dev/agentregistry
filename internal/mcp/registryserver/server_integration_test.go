//go:build integration

package registryserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPListServers_HappyPath(t *testing.T) {
	ctx := context.Background()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool)

	// Seed a published MCPServer so the MCP tool has something to return.
	const (
		serverNamespace = "default"
		serverName      = "echo"
		serverVersion   = "1.0.0"
	)
	spec, err := json.Marshal(v1alpha1.MCPServerSpec{
		Description: "Echo test server",
		Remotes: []v1alpha1.MCPTransport{
			{Type: "streamable-http", URL: "https://echo.example/mcp"},
		},
	})
	require.NoError(t, err)
	_, err = stores[v1alpha1.KindMCPServer].Upsert(ctx, serverNamespace, serverName, serverVersion, spec, v1alpha1store.UpsertOpts{})
	require.NoError(t, err, "seed server")

	// Wire up MCP server + client over in-memory transports.
	server := NewServer(stores)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err, "connect MCP server")
	defer func() {
		require.NoError(t, serverSession.Wait())
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err, "connect MCP client")
	defer func() { _ = clientSession.Close() }()

	// list_servers returns v1alpha1 envelopes.
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_servers",
		Arguments: map[string]any{"limit": 10},
	})
	require.NoError(t, err, "call list_servers")
	require.NotNil(t, res.StructuredContent, "structured output present")

	var out struct {
		Items      []v1alpha1.MCPServer `json:"items"`
		NextCursor string               `json:"nextCursor,omitempty"`
		Count      int                  `json:"count"`
	}
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err, "marshal structured output")
	require.NoError(t, json.Unmarshal(raw, &out), "unmarshal list output")

	require.Len(t, out.Items, 1)
	got := out.Items[0]
	assert.Equal(t, v1alpha1.GroupVersion, got.APIVersion)
	assert.Equal(t, v1alpha1.KindMCPServer, got.Kind)
	assert.Equal(t, serverName, got.Metadata.Name)
	assert.Equal(t, serverVersion, got.Metadata.Version)
	assert.Equal(t, "Echo test server", got.Spec.Description)

	// get_server returns a single v1alpha1 envelope.
	getRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_server",
		Arguments: map[string]any{"name": serverName, "version": serverVersion},
	})
	require.NoError(t, err, "call get_server")
	require.NotNil(t, getRes.StructuredContent)

	var gotOne v1alpha1.MCPServer
	raw, err = json.Marshal(getRes.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &gotOne))
	assert.Equal(t, serverName, gotOne.Metadata.Name)
	assert.Equal(t, "Echo test server", gotOne.Spec.Description)
}
