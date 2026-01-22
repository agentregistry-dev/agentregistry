package registryserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestMCPListServers_HappyPath(t *testing.T) {
	ctx := context.Background()
	db := database.NewTestDB(t)
	svc := service.NewRegistryService(db, &config.Config{EnableRegistryValidation: false}, nil)

	// Seed a published server so the MCP tool can return it.
	const (
		serverName    = "com.example/echo"
		serverVersion = "1.0.0"
	)
	_, err := svc.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Echo test server",
		Version:     serverVersion,
	})
	if err != nil && strings.Contains(err.Error(), "vector") {
		t.Skip("pgvector extension not available in local Postgres; skipping MCP integration test")
	}
	require.NoError(t, err, "seed server")
	require.NoError(t, svc.PublishServer(ctx, serverName, serverVersion), "publish server")

	// Wire up MCP server and client over in-memory transports.
	server := NewServer(config.NewConfig(), svc)
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

	// Call list_servers and decode structured output.
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_servers",
		Arguments: map[string]any{"limit": 10},
	})
	require.NoError(t, err, "call list_servers")
	require.NotNil(t, res.StructuredContent, "structured output present")

	var out apiv0.ServerListResponse
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err, "marshal structured output")
	require.NoError(t, json.Unmarshal(raw, &out), "unmarshal to ServerListResponse")

	require.Len(t, out.Servers, 1)
	assert.Equal(t, serverName, out.Servers[0].Server.Name)
	assert.Equal(t, serverVersion, out.Servers[0].Server.Version)
	assert.Equal(t, "Echo test server", out.Servers[0].Server.Description)
}
