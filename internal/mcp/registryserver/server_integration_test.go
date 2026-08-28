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

func TestRunList_TagFilter(t *testing.T) {
	ctx := context.Background()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool, v1alpha1store.TestSchemaRegistry())
	store := stores[v1alpha1.KindAgent]

	for _, tag := range []string{"latest", "v1", "v2"} {
		_, err := store.Upsert(ctx, &v1alpha1.Agent{
			Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "test-agent", Tag: tag},
			Spec:     v1alpha1.AgentSpec{Description: tag},
		})
		require.NoError(t, err)
	}

	rows, _, err := runList(ctx, store, v1alpha1.KindAgent, nil, nil, listInput{Tag: "v1"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "v1", rows[0].Metadata.Tag)

	rows, _, err = runList(ctx, store, v1alpha1.KindAgent, nil, nil, listInput{Tag: "does-not-exist"})
	require.NoError(t, err)
	assert.Empty(t, rows)

	rows, _, err = runList(ctx, store, v1alpha1.KindAgent, nil, nil, listInput{Tag: "LATEST"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "latest", rows[0].Metadata.Tag)
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

// envelopeMeta decodes just the identity of a v1alpha1 envelope, so one helper
// can assert every kind's list_X/get_X output without a typed decode per kind.
type envelopeMeta struct {
	APIVersion string              `json:"apiVersion"`
	Kind       string              `json:"kind"`
	Metadata   v1alpha1.ObjectMeta `json:"metadata"`
}

type listEnvelope struct {
	Items      []envelopeMeta `json:"items"`
	NextCursor string         `json:"nextCursor,omitempty"`
	Count      int            `json:"count"`
}

func decodeStructured(t *testing.T, structured any, out any) {
	t.Helper()
	raw, err := json.Marshal(structured)
	require.NoError(t, err, "marshal structured output")
	require.NoError(t, json.Unmarshal(raw, out), "unmarshal structured output")
}

// TestMCPCatalogTools checks that every registered v1alpha1 kind's list_X/get_X tools
// are wired correctly.
func TestMCPCatalogTools(t *testing.T) {
	ctx := context.Background()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool, v1alpha1store.TestSchemaRegistry())

	const namespace = "default"
	taggedTag := v1alpha1store.DefaultTag()

	cases := []struct {
		kind        string
		listTool    string
		getTool     string
		name        string
		expectedTag string // DefaultTag for tagged artifacts; "" for mutable-object kinds.
		seed        func(t *testing.T, name string)
	}{
		{
			kind: v1alpha1.KindAgent, listTool: "list_agents", getTool: "get_agent",
			name: "test-agent", expectedTag: taggedTag,
			seed: func(t *testing.T, name string) {
				_, err := stores[v1alpha1.KindAgent].Upsert(ctx, &v1alpha1.Agent{
					Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name},
					Spec:     v1alpha1.AgentSpec{Description: "Test agent"},
				})
				require.NoError(t, err, "seed agent")
			},
		},
		{
			kind: v1alpha1.KindMCPServer, listTool: "list_servers", getTool: "get_server",
			name: "test-server", expectedTag: taggedTag,
			seed: func(t *testing.T, name string) {
				_, err := stores[v1alpha1.KindMCPServer].Upsert(ctx, &v1alpha1.MCPServer{
					Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name},
					Spec: v1alpha1.MCPServerSpec{
						Description: "Test server",
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
			},
		},
		{
			kind: v1alpha1.KindSkill, listTool: "list_skills", getTool: "get_skill",
			name: "test-skill", expectedTag: taggedTag,
			seed: func(t *testing.T, name string) {
				_, err := stores[v1alpha1.KindSkill].Upsert(ctx, &v1alpha1.Skill{
					Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name},
					Spec:     v1alpha1.SkillSpec{Description: "Test skill"},
				})
				require.NoError(t, err, "seed skill")
			},
		},
		{
			kind: v1alpha1.KindPrompt, listTool: "list_prompts", getTool: "get_prompt",
			name: "test-prompt", expectedTag: taggedTag,
			seed: func(t *testing.T, name string) {
				_, err := stores[v1alpha1.KindPrompt].Upsert(ctx, &v1alpha1.Prompt{
					Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name},
					Spec:     v1alpha1.PromptSpec{Description: "Test prompt", Content: "hello"},
				})
				require.NoError(t, err, "seed prompt")
			},
		},
		{
			kind: v1alpha1.KindModel, listTool: "list_models", getTool: "get_model",
			name: "test-model", expectedTag: taggedTag,
			seed: func(t *testing.T, name string) {
				_, err := stores[v1alpha1.KindModel].Upsert(ctx, &v1alpha1.Model{
					Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name},
					Spec: v1alpha1.ModelSpec{
						Description: "Test model",
						Provider:    v1alpha1.ModelProviderBedrock,
						Model:       "us.anthropic.claude-opus-4-8",
					},
				})
				require.NoError(t, err, "seed model")
			},
		},
		{
			kind: v1alpha1.KindPlugin, listTool: "list_plugins", getTool: "get_plugin",
			name: "test-plugin", expectedTag: taggedTag,
			seed: func(t *testing.T, name string) {
				_, err := stores[v1alpha1.KindPlugin].Upsert(ctx, &v1alpha1.Plugin{
					Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name},
					Spec:     v1alpha1.PluginSpec{Description: "Test plugin"},
				})
				require.NoError(t, err, "seed plugin")
				status := v1alpha1.PluginStatus{
					Manifest: &v1alpha1.PluginManifest{
						Commands: &v1alpha1.CommandsField{Map: map[string]v1alpha1.CommandEntry{
							"docs": {Description: "Search documentation"},
							"page": {Description: "Open a page"},
						}},
					},
				}
				statusJSON, err := json.Marshal(status)
				require.NoError(t, err, "marshal plugin status")
				require.NoError(t, stores[v1alpha1.KindPlugin].PatchStatus(ctx, namespace, name, taggedTag,
					func(json.RawMessage) (json.RawMessage, error) { return statusJSON, nil },
				), "seed plugin status")
			},
		},
		{
			kind: v1alpha1.KindDeployment, listTool: "list_deployments", getTool: "get_deployment",
			name: "test-deployment", expectedTag: "",
			seed: func(t *testing.T, name string) {
				_, err := stores[v1alpha1.KindDeployment].Upsert(ctx, &v1alpha1.Deployment{
					Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name},
					Spec: v1alpha1.DeploymentSpec{
						TargetRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindAgent, Name: "test-agent"},
						RuntimeRef: v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "test-runtime"},
					},
				})
				require.NoError(t, err, "seed deployment")
				// Populated status.details (a JSON object held in a json.RawMessage)
				// The envelope must survive the tools' output-schema validation.
				var status v1alpha1.Status
				require.NoError(t, status.SetDetailsKey("runtimeMetadata", map[string]any{"state": "ready"}))
				statusJSON, err := json.Marshal(status)
				require.NoError(t, err, "marshal deployment status")
				require.NoError(t, stores[v1alpha1.KindDeployment].PatchStatus(ctx, namespace, name, "",
					func(json.RawMessage) (json.RawMessage, error) { return statusJSON, nil },
				), "seed deployment status")
			},
		},
		{
			kind: v1alpha1.KindRuntime, listTool: "list_runtimes", getTool: "get_runtime",
			name: "test-runtime", expectedTag: "",
			seed: func(t *testing.T, name string) {
				_, err := stores[v1alpha1.KindRuntime].Upsert(ctx, &v1alpha1.Runtime{
					Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name},
					Spec:     v1alpha1.RuntimeSpec{Type: "docker"},
				})
				require.NoError(t, err, "seed runtime")
			},
		},
	}

	server := NewServer(stores, nil, nil)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err, "connect MCP server")
	defer func() {
		err := serverSession.Wait()
		if err != nil && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		}
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err, "connect MCP client")
	defer func() { _ = clientSession.Close() }()

	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			tc.seed(t, tc.name)

			// list_X returns the test resource as a v1alpha1 envelope. Filter by
			// its unique name so the assertion remains isolated from other rows.
			// This also exercises the search filter.
			listRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
				Name:      tc.listTool,
				Arguments: map[string]any{"namespace": namespace, "search": tc.name, "limit": 10},
			})
			require.NoError(t, err, "call %s", tc.listTool)
			require.NotNil(t, listRes.StructuredContent, "%s structured output present", tc.listTool)

			var list listEnvelope
			decodeStructured(t, listRes.StructuredContent, &list)
			require.Len(t, list.Items, 1, "%s returns the one seeded resource", tc.listTool)
			got := list.Items[0]
			assert.Equal(t, v1alpha1.GroupVersion, got.APIVersion)
			assert.Equal(t, tc.kind, got.Kind)
			assert.Equal(t, tc.name, got.Metadata.Name)
			assert.Equal(t, tc.expectedTag, got.Metadata.Tag)

			// get_X fetches the same resource by name.
			getRes, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
				Name:      tc.getTool,
				Arguments: map[string]any{"namespace": namespace, "name": tc.name},
			})
			require.NoError(t, err, "call %s", tc.getTool)
			require.NotNil(t, getRes.StructuredContent, "%s structured output present", tc.getTool)

			var one envelopeMeta
			decodeStructured(t, getRes.StructuredContent, &one)
			assert.Equal(t, tc.kind, one.Kind)
			assert.Equal(t, tc.name, one.Metadata.Name)
		})
	}
}
