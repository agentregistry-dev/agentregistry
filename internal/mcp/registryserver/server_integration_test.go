//go:build integration

package registryserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestMCPListServers_HappyPath(t *testing.T) {
	ctx := context.Background()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool, v1alpha1store.TestSchemaRegistry())

	// Seed a published MCPServer so the MCP tool has something to return.
	const (
		serverNamespace = "default"
		serverName      = "echo"
	)
	serverTag := v1alpha1store.DefaultTag()
	_, err := stores[v1alpha1.KindMCPServer].Upsert(ctx, &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Namespace: serverNamespace, Name: serverName},
		Spec: v1alpha1.MCPServerSpec{
			Description: "Echo test server",
			Source: &v1alpha1.MCPServerSource{
				Package: &v1alpha1.MCPPackage{
					Origin: v1alpha1.MCPPackageOrigin{
						Type:       v1alpha1.MCPPackageOriginTypeOCI,
						Identifier: "ghcr.io/example/echo:1.0.0",
						OCI:        &v1alpha1.MCPPackageOriginOCI{ServerName: "echo"},
					},
					Transport: v1alpha1.MCPTransport{Type: "stdio"},
				},
			},
		},
	})
	require.NoError(t, err, "seed server")

	// Wire up MCP server + client over in-memory transports.
	server := NewServer(stores, nil, nil)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err, "connect MCP server")
	defer func() {
		// In-memory transport clean close surfaces as ErrClosedPipe / io.EOF.
		err := serverSession.Wait()
		if err != nil && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		}
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
	assert.Equal(t, serverTag, got.Metadata.Tag)
	assert.Equal(t, "Echo test server", got.Spec.Description)

	// get_server returns a single v1alpha1 envelope.
	getRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_server",
		Arguments: map[string]any{"name": serverName, "tag": serverTag},
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

// seedMCPServer publishes one MCPServer so the authz-seam tests have a row to
// include/exclude, and returns its namespace/name.
func seedMCPServer(ctx context.Context, t *testing.T, stores map[string]*v1alpha1store.Store) (namespace, name string) {
	t.Helper()
	namespace, name = "default", "echo"
	_, err := stores[v1alpha1.KindMCPServer].Upsert(ctx, &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name},
		Spec: v1alpha1.MCPServerSpec{
			Description: "Echo test server",
			Source: &v1alpha1.MCPServerSource{
				Package: &v1alpha1.MCPPackage{
					Origin: v1alpha1.MCPPackageOrigin{
						Type:       v1alpha1.MCPPackageOriginTypeOCI,
						Identifier: "ghcr.io/example/echo:1.0.0",
						OCI:        &v1alpha1.MCPPackageOriginOCI{ServerName: "echo"},
					},
					Transport: v1alpha1.MCPTransport{Type: "stdio"},
				},
			},
		},
	})
	require.NoError(t, err, "seed server")
	return namespace, name
}

// TestRunList_Authz exercises the list read path's RBAC seam directly with fake
// per-kind hooks: denial short-circuits, and Extra{Where+Args} reach the SQL query.
func TestRunList_Authz(t *testing.T) {
	ctx := context.Background()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool, v1alpha1store.TestSchemaRegistry())
	_, name := seedMCPServer(ctx, t, stores)
	store := stores[v1alpha1.KindMCPServer]

	t.Run("authorizer denial is returned", func(t *testing.T) {
		denyAuthz := func(context.Context, resource.AuthorizeInput) error { return errors.New("denied") }
		rows, _, err := runList(ctx, store, v1alpha1.KindMCPServer, denyAuthz, nil, listInput{})
		require.Error(t, err)
		assert.Nil(t, rows)
	})

	t.Run("list filter Extra{Where+Args} exclude non-matching rows", func(t *testing.T) {
		filter := func(context.Context, resource.AuthorizeInput) (string, []any, error) {
			return "name = $1", []any{"does-not-exist"}, nil
		}
		rows, _, err := runList(ctx, store, v1alpha1.KindMCPServer, nil, filter, listInput{})
		require.NoError(t, err)
		assert.Empty(t, rows, "predicate should exclude the seeded row")
	})

	t.Run("list filter ExtraWhere+ExtraArgs include matching rows", func(t *testing.T) {
		filter := func(context.Context, resource.AuthorizeInput) (string, []any, error) {
			return "name = $1", []any{name}, nil
		}
		rows, _, err := runList(ctx, store, v1alpha1.KindMCPServer, nil, filter, listInput{})
		require.NoError(t, err)
		assert.Len(t, rows, 1, "predicate with the seeded name should include it")
	})

	t.Run("nil hooks read the store unscoped", func(t *testing.T) {
		rows, _, err := runList(ctx, store, v1alpha1.KindMCPServer, nil, nil, listInput{})
		require.NoError(t, err)
		assert.Len(t, rows, 1, "no hooks: full catalogue (OSS default)")
	})

	t.Run("hooks receive the list AuthorizeInput", func(t *testing.T) {
		var gotAuth, gotFilter resource.AuthorizeInput
		authz := func(_ context.Context, in resource.AuthorizeInput) error { gotAuth = in; return nil }
		filter := func(_ context.Context, in resource.AuthorizeInput) (string, []any, error) {
			gotFilter = in
			return "", nil, nil
		}
		_, _, err := runList(ctx, store, v1alpha1.KindMCPServer, authz, filter, listInput{Namespace: "default"})
		require.NoError(t, err)
		want := resource.AuthorizeInput{Verb: "list", Kind: v1alpha1.KindMCPServer, Namespace: "default"}
		assert.Equal(t, want, gotAuth)
		assert.Equal(t, want, gotFilter)
	})
}

// TestGetEnvelope_Authz exercises the get read path's RBAC seam: denial
// short-circuits before the fetch, a nil authorizer returns the object, and the
// authorizer receives the get AuthorizeInput.
func TestGetEnvelope_Authz(t *testing.T) {
	ctx := context.Background()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool, v1alpha1store.TestSchemaRegistry())
	ns, name := seedMCPServer(ctx, t, stores)
	store := stores[v1alpha1.KindMCPServer]
	newObj := func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} }

	t.Run("authorizer denial is returned", func(t *testing.T) {
		denyAuthz := func(context.Context, resource.AuthorizeInput) error { return errors.New("denied") }
		_, _, err := getEnvelope(ctx, store, v1alpha1.KindMCPServer, denyAuthz, getByRefInput{Namespace: ns, Name: name}, newObj)
		require.Error(t, err)
	})

	t.Run("nil authorizer returns the object", func(t *testing.T) {
		_, obj, err := getEnvelope(ctx, store, v1alpha1.KindMCPServer, nil, getByRefInput{Namespace: ns, Name: name}, newObj)
		require.NoError(t, err)
		require.NotNil(t, obj)
		assert.Equal(t, name, obj.Metadata.Name)
	})

	t.Run("authorizer receives the get AuthorizeInput", func(t *testing.T) {
		var got resource.AuthorizeInput
		authz := func(_ context.Context, in resource.AuthorizeInput) error { got = in; return nil }
		_, _, err := getEnvelope(ctx, store, v1alpha1.KindMCPServer, authz, getByRefInput{Namespace: ns, Name: name}, newObj)
		require.NoError(t, err)
		assert.Equal(t, resource.AuthorizeInput{Verb: "get", Kind: v1alpha1.KindMCPServer, Namespace: ns, Name: name}, got)
	})
}
